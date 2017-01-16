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
package hook

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sync"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

// HookManager is responsible for the maintenance of hooks in the system state.
// It runs hooks when they're requested, assuming they're present in the given
// snap. Otherwise they're skipped with no error.
type HookManager struct {
	state      *state.State
	runner     *state.TaskRunner
	repository *hookstate.Repository

	contextsMutex sync.RWMutex
	contexts      map[string]*hookstate.Context
}

// Manager returns a new HookManager.
func Manager(s *state.State) (*HookManager, error) {
	runner := state.NewTaskRunner(s)
	manager := &HookManager{
		state:      s,
		runner:     runner,
		repository: hookstate.NewRepository(),
		contexts:   make(map[string]*hookstate.Context),
	}

	runner.AddHandler("run-hook", manager.doRunHook, nil)

	return manager, nil
}

// Register registers a function to create Handler values whenever hooks
// matching the provided pattern are run.
func (m *HookManager) Register(pattern *regexp.Regexp, generator hookstate.HandlerGenerator) {
	m.repository.AddHandlerGenerator(pattern, generator)
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
func (m *HookManager) Context(contextID string) (*hookstate.Context, error) {
	m.contextsMutex.RLock()
	defer m.contextsMutex.RUnlock()

	context, ok := m.contexts[contextID]
	if !ok {
		return nil, fmt.Errorf("no context for ID: %q", contextID)
	}

	return context, nil
}

func hookSetup(task *state.Task) (*hookstate.HookSetup, *snapstate.SnapState, error) {
	var hooksup hookstate.HookSetup
	err := task.Get("hook-setup", &hooksup)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot extract hook setup from task: %s", err)
	}

	var snapst snapstate.SnapState
	err = snapstate.Get(task.State(), hooksup.Snap, &snapst)
	if err == state.ErrNoState {
		return nil, nil, fmt.Errorf("cannot find %q snap", hooksup.Snap)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("cannot handle %q snap: %v", hooksup.Snap, err)
	}

	return &hooksup, &snapst, nil
}

// doRunHook actually runs the hook that was requested.
//
// Note that this method is synchronous, as the task is already running in a
// goroutine.
func (m *HookManager) doRunHook(task *state.Task, tomb *tomb.Tomb) error {
	task.State().Lock()
	hooksup, snapst, err := hookSetup(task)
	task.State().Unlock()
	if err != nil {
		return err
	}

	info, err := snapst.CurrentInfo()
	if err != nil {
		return fmt.Errorf("cannot read %q snap details: %v", hooksup.Snap, err)
	}

	hookExists := info.Hooks[hooksup.Hook] != nil
	if !hookExists && !hooksup.Optional {
		return fmt.Errorf("snap %q has no %q hook", hooksup.Snap, hooksup.Hook)
	}

	context, err := hookstate.NewContext(task, hooksup, nil)
	if err != nil {
		return err
	}

	// Obtain a handler for this hook. The repository returns a list since it's
	// possible for regular expressions to overlap, but multiple handlers is an
	// error (as is no handler).
	handlers := m.repository.GenerateHandlers(context)
	handlersCount := len(handlers)
	if handlersCount == 0 {
		return fmt.Errorf("internal error: no registered handlers for hook %q", hooksup.Hook)
	}
	if handlersCount > 1 {
		return fmt.Errorf("internal error: %d handlers registered for hook %q, expected 1", handlersCount, hooksup.Hook)
	}

	context.SetHandler(handlers[0])

	contextID := context.ID()
	m.contextsMutex.Lock()
	m.contexts[contextID] = context
	m.contextsMutex.Unlock()

	defer func() {
		m.contextsMutex.Lock()
		delete(m.contexts, contextID)
		m.contextsMutex.Unlock()
	}()

	if err = context.Handler().Before(); err != nil {
		return err
	}

	if hookExists {
		output, err := runHook(context, tomb)
		if err != nil {
			err = osutil.OutputErr(output, err)
			if handlerErr := context.Handler().Error(err); handlerErr != nil {
				return handlerErr
			}

			return err
		}
	}

	if err = context.Handler().Done(); err != nil {
		return err
	}

	context.Lock()
	defer context.Unlock()
	if err = context.Done(); err != nil {
		return err
	}

	return nil
}

func runHookImpl(c *hookstate.Context, tomb *tomb.Tomb) ([]byte, error) {
	return runHookAndWait(c.SnapName(), c.SnapRevision(), c.HookName(), c.ID(), tomb)
}

var runHook = runHookImpl

// MockRunHook mocks the actual invocation of hooks for tests.
func MockRunHook(hookInvoke func(c *hookstate.Context, tomb *tomb.Tomb) ([]byte, error)) (restore func()) {
	oldRunHook := runHook
	runHook = hookInvoke
	return func() {
		runHook = oldRunHook
	}
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
