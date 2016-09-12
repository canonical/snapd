// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

// Package hookstate implements the manager and state aspects responsible for
// the running of hooks.
package hookstate

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sync"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

// HookRunner is the hook runner. Exported here for use in tests.
var HookRunner = runHookAndWait

// HookManager is responsible for the maintenance of hooks in the system state.
// It runs hooks when they're requested, assuming they're present in the given
// snap. Otherwise they're skipped with no error.
type HookManager struct {
	state      *state.State
	runner     *state.TaskRunner
	repository *repository

	contextsMutex sync.RWMutex
	contexts      map[string]*Context
}

// Handler is the interface a client must satify to handle hooks.
type Handler interface {
	// Before is called right before the hook is to be run.
	Before() error

	// Done is called right after the hook has finished successfully.
	Done() error

	// Error is called if the hook encounters an error while running.
	Error(err error) error
}

// HandlerGenerator is the function signature required to register for hooks.
type HandlerGenerator func(*Context) Handler

// HookSetup is a reference to a hook within a specific snap.
type HookSetup struct {
	Snap     string        `json:"snap"`
	Revision snap.Revision `json:"revision"`
	Hook     string        `json:"hook"`
}

// Manager returns a new HookManager.
func Manager(s *state.State) (*HookManager, error) {
	runner := state.NewTaskRunner(s)
	manager := &HookManager{
		state:      s,
		runner:     runner,
		repository: newRepository(),
		contexts:   make(map[string]*Context),
	}

	runner.AddHandler("run-hook", manager.doRunHook, nil)

	return manager, nil
}

// HookTask returns a task that will run the specified hook.
func HookTask(s *state.State, taskSummary, snapName string, revision snap.Revision, hookName string) *state.Task {
	task := s.NewTask("run-hook", taskSummary)
	task.Set("hook-setup", HookSetup{Snap: snapName, Revision: revision, Hook: hookName})
	return task
}

// Register requests that a given handler generator be called when a matching
// hook is run, and the handler be used for the hook.
//
// Specifically, if a matching hook is about to be run, the handler's Before()
// method will be called. After the hook has completed running, either the
// handler's Done() method or its Error() method will be called, depending on
// the outcome.
func (m *HookManager) Register(pattern *regexp.Regexp, generator HandlerGenerator) {
	m.repository.addHandlerGenerator(pattern, generator)
}

// Ensure implements StateManager.Ensure.
func (m *HookManager) Ensure() error {
	m.runner.Ensure()
	return nil
}

// Wait implements StateManager.Wait.
func (m *HookManager) Wait() {
	m.runner.Wait()
}

// Stop implements StateManager.Stop.
func (m *HookManager) Stop() {
	m.runner.Stop()
}

// Context obtains the context for the given context ID.
func (m *HookManager) Context(contextID string) (*Context, error) {
	m.contextsMutex.RLock()
	defer m.contextsMutex.RUnlock()

	context, ok := m.contexts[contextID]
	if !ok {
		return nil, fmt.Errorf("no context for ID: %q", contextID)
	}

	return context, nil
}

// doRunHook actually runs the hook that was requested.
//
// Note that this method is synchronous, as the task is already running in a
// goroutine.
func (m *HookManager) doRunHook(task *state.Task, tomb *tomb.Tomb) error {
	task.State().Lock()
	setup := &HookSetup{}
	err := task.Get("hook-setup", setup)
	task.State().Unlock()

	if err != nil {
		return fmt.Errorf("cannot extract hook setup from task: %s", err)
	}

	// Obtain a handler for this hook. The repository returns a list since it's
	// possible for regular expressions to overlap, but multiple handlers is an
	// error (as is no handler).
	handlers := m.repository.generateHandlers(&Context{task: task, setup: setup})
	handlersCount := len(handlers)
	if handlersCount == 0 {
		return fmt.Errorf("no registered handlers for hook %q", setup.Hook)
	}
	if handlersCount > 1 {
		return fmt.Errorf("%d handlers registered for hook %q, expected 1", handlersCount, setup.Hook)
	}

	handler := handlers[0]
	context, err := NewContext(task, setup, handler)
	if err != nil {
		return err
	}

	contextID := context.ID()

	m.contextsMutex.Lock()
	m.contexts[contextID] = context
	m.contextsMutex.Unlock()

	defer func() {
		m.contextsMutex.Lock()
		delete(m.contexts, contextID)
		m.contextsMutex.Unlock()
	}()

	// About to run the hook-- notify the handler
	if err = handler.Before(); err != nil {
		return err
	}

	// Actually run the hook
	output, err := HookRunner(setup.Snap, setup.Revision, setup.Hook, contextID, tomb)
	if err != nil {
		err = osutil.OutputErr(output, err)
		if handlerErr := handler.Error(err); handlerErr != nil {
			return handlerErr
		}

		return err
	}

	// Assuming no error occurred, notify the handler that the hook has
	// finished.
	if err = handler.Done(); err != nil {
		return err
	}

	return nil
}

func runHookAndWait(snapName string, revision snap.Revision, hookName, hookContext string, tomb *tomb.Tomb) ([]byte, error) {
	command := exec.Command("snap", "run", "--hook", hookName, "-r", revision.String(), snapName)

	// Make sure the hook has its context defined so it can communicate via the
	// REST API.
	command.Env = append(os.Environ(), fmt.Sprintf("SNAP_CONTEXT=%s", hookContext))

	// Make sure we can obtain stdout and stderror. Same buffer so they're
	// combined.
	buffer := bytes.NewBuffer(nil)
	command.Stdout = buffer
	command.Stderr = buffer

	// Actually run the hook.
	if err := command.Start(); err != nil {
		return nil, err
	}

	hookCompleted := make(chan struct{})
	var hookError error
	go func() {
		// Wait for hook to complete
		hookError = command.Wait()
		close(hookCompleted)
	}()

	select {
	// Hook completed; it may or may not have been successful.
	case <-hookCompleted:
		return buffer.Bytes(), hookError

	// Hook was aborted.
	case <-tomb.Dying():
		if err := command.Process.Kill(); err != nil {
			return nil, fmt.Errorf("cannot abort hook %q: %s", hookName, err)
		}
		return nil, fmt.Errorf("hook %q aborted", hookName)
	}
}
