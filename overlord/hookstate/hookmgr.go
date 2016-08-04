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
	"os/exec"
	"regexp"
	"strconv"
	"sync"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

// HookManager is responsible for the maintenance of hooks in the system state.
// It runs hooks when they're requested, assuming they're present in the given
// snap. Otherwise they're skipped with no error.
type HookManager struct {
	state      *state.State
	runner     *state.TaskRunner
	repository *repository

	lastHookID      int
	lastHookIDMutex sync.Mutex

	activeHandlers      map[int]Handler
	activeHandlersMutex sync.RWMutex
}

// Handler is the interface a client must satify to handle hooks.
type Handler interface {
	// Before is called right before the hook is to be run.
	Before() error

	// Done is called right after the hook has finished successfully.
	Done() error

	// Error is called if the hook encounters an error while running.
	Error(err error) error

	// Get this hook's data that is associated with the given key.
	Get(key string) (map[string]interface{}, error)

	// Set this hook's data that is associated with the given key.
	Set(key string, data map[string]interface{}) error
}

// HandlerGenerator is the function signature required to register for hooks.
type HandlerGenerator func(*Context) Handler

// hookSetup is a reference to a hook within a specific snap.
type hookSetup struct {
	Snap     string        `json:"snap"`
	Revision snap.Revision `json:"revision"`
	Hook     string        `json:"hook"`
}

// Manager returns a new HookManager.
func Manager(s *state.State) (*HookManager, error) {
	runner := state.NewTaskRunner(s)
	manager := &HookManager{
		state:          s,
		runner:         runner,
		repository:     newRepository(),
		activeHandlers: make(map[int]Handler),
	}

	runner.AddHandler("run-hook", manager.doRunHook, nil)

	return manager, nil
}

// HookTask returns a task that will run the specified hook.
func HookTask(s *state.State, taskSummary, snapName string, revision snap.Revision, hookName string) *state.Task {
	task := s.NewTask("run-hook", taskSummary)
	task.Set("hook-setup", hookSetup{Snap: snapName, Revision: revision, Hook: hookName})
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

// GetHookData obtains the data that a specific hook instance has associated with the given key.
func (m *HookManager) GetHookData(hookID int, key string) (map[string]interface{}, error) {
	m.activeHandlersMutex.RLock()
	defer m.activeHandlersMutex.RUnlock()

	if handler, ok := m.activeHandlers[hookID]; ok {
		return handler.Get(key)
	}

	return nil, fmt.Errorf("no hook with ID %d", hookID)
}

// SetHookData sets the data that a specific hook instance has associated with the given key.
func (m *HookManager) SetHookData(hookID int, key string, data map[string]interface{}) error {
	m.activeHandlersMutex.RLock()
	defer m.activeHandlersMutex.RUnlock()

	if handler, ok := m.activeHandlers[hookID]; ok {
		return handler.Set(key, data)
	}

	return fmt.Errorf("no hook with ID %d", hookID)
}

func (m *HookManager) getNextHookTaskID() int {
	m.lastHookIDMutex.Lock()
	defer m.lastHookIDMutex.Unlock()

	m.lastHookID++
	return m.lastHookID
}

func (m *HookManager) addActiveHandler(id int, handler Handler) {
	m.activeHandlersMutex.Lock()
	defer m.activeHandlersMutex.Unlock()

	m.activeHandlers[id] = handler
}

func (m *HookManager) removeActiveHandler(id int) {
	m.activeHandlersMutex.Lock()
	defer m.activeHandlersMutex.Unlock()

	delete(m.activeHandlers, id)
}

// doRunHook actually runs the hook that was requested.
//
// Note that this method is synchronous, as the task is already running in a
// goroutine.
func (m *HookManager) doRunHook(task *state.Task, tomb *tomb.Tomb) error {
	task.State().Lock()
	var setup hookSetup
	err := task.Get("hook-setup", &setup)
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

	// Each hook instance needs to be able to communicate with their handler via
	// the REST API. The only way this works is if the hook instance can specify
	// the handler with which it needs to communicate. We generate a unique ID
	// per hook instance here and associate it with the handler in order to
	// solve that problem.
	hookID := m.getNextHookTaskID()
	m.addActiveHandler(hookID, handler)
	defer m.removeActiveHandler(hookID)

	// About to run the hook-- notify the handler
	if err = handler.Before(); err != nil {
		return err
	}

	// Actually run the hook
	output, err := runHookAndWait(setup.Snap, setup.Revision, setup.Hook, hookID, tomb)
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

func runHookAndWait(snapName string, revision snap.Revision, hookName string, hookID int, tomb *tomb.Tomb) ([]byte, error) {
	command := exec.Command(
		"snap", "run", "--hook", hookName, "-r", revision.String(),
		"-i", strconv.Itoa(hookID), snapName)

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
