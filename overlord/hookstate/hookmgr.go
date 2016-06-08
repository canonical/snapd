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
	"fmt"
	"regexp"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

// HookManager is responsible for the maintenance of hooks in the system state.
// It runs hooks when they're requested, assuming they're present in the given
// snap. Otherwise they're skipped with no error.
type HookManager struct {
	state  *state.State
	runner *state.TaskRunner
	repository *Repository
}

type Handler interface {
	Before() error
	Done() error
	Error(err error) error
}

type HandlerGenerator func(*Context) Handler

// HookRef is a reference to a hook within a specific snap.
type HookRef struct {
	Snap     string        `json:"snap"`
	Revision snap.Revision `json:"revision"`
	Hook     string        `json:"hook"`
}

// Manager returns a new HookManager.
func Manager(s *state.State) (*HookManager, error) {
	runner := state.NewTaskRunner(s)
	manager := &HookManager{
		state:  s,
		runner: runner,
		repository: NewRepository(),
	}

	runner.AddHandler("run-hook", manager.doRunHook, nil)

	return manager, nil
}

// Register requests that a given handler generator be called when a matching
// hook is run, and the handler be used for the hook.
//
// Specifically, if a matching hook is about to be run, the handler's Before()
// method will be called. After the hook has completed running, either the
// handler's Done() method or its Error() method will be called, depending on
// the outcome.
func (m *HookManager) Register(pattern *regexp.Regexp, generator HandlerGenerator) {
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

// doRunHook actually runs the hook that was requested.
//
// Note that this method is synchronous, as the task is already running in a
// goroutine.
func (m *HookManager) doRunHook(task *state.Task, tomb *tomb.Tomb) error {
	task.State().Lock()
	var hookRef HookRef
	err := task.Get("hook", &hookRef)
	task.State().Unlock()

	if err != nil {
		return fmt.Errorf("failed to extract hook from task: %s", err)
	}

	context := &Context {
		task: task,
		hookRef: hookRef,
	}

	// Obtain a list of handlers for this hook (if any)
	handlers := m.repository.GenerateHandlers(context)

	// About to run the hook-- notify the handlers
	for _, handler := range handlers {
		handler.Before()
	}

	// TODO: Actually dispatch the hook.

	// Done with the hook. TODO: Check the result, if success call Done(), if
	// error, call Error(). Since we have no hooks, for now we just call Done().
	for _, handler := range handlers {
		handler.Done()
	}

	return nil
}
