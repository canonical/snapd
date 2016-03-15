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

package state

import (
	"sync"

	"gopkg.in/tomb.v2"
)

// HandlerFunc is the type of function for the hanlders
type HandlerFunc func(task *Task) error

// TaskRunner controls the running of goroutines to execute known task kinds.
type TaskRunner struct {
	state *State

	// locking
	mu       sync.Mutex
	handlers map[string]HandlerFunc

	// go-routines lifecycle
	tombs map[string]*tomb.Tomb
}

// NewTaskRunner creates a new TaskRunner
func NewTaskRunner(s *State) *TaskRunner {
	return &TaskRunner{
		state:    s,
		handlers: make(map[string]HandlerFunc),
		tombs:    make(map[string]*tomb.Tomb),
	}
}

// AddHandler registers the function to concurrently call for handling
// tasks of the given kind.
func (r *TaskRunner) AddHandler(kind string, fn HandlerFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.handlers[kind] = fn
}

// Handlers returns the map of name/handler functions
func (r *TaskRunner) Handlers() map[string]HandlerFunc {
	return r.handlers
}

// run must be called with the state lock in place
func (r *TaskRunner) run(fn HandlerFunc, taskID string) {
	t := r.state.tasks[taskID]
	r.tombs[taskID] = &tomb.Tomb{}
	r.tombs[taskID].Go(func() error {
		err := fn(t)

		r.state.Lock()
		defer r.state.Unlock()
		if err == nil {
			t.SetStatus(DoneStatus)
		} else {
			t.SetStatus(ErrorStatus)
		}
		delete(r.tombs, taskID)

		return err
	})
}

// mustWait must be called with the state lock in place
func (r *TaskRunner) mustWait(t *Task) bool {
	for _, wt := range t.WaitTasks() {
		if wt.Status() != DoneStatus {
			return true
		}
	}

	return false
}

// Ensure starts new goroutines for all known tasks with no pending
// dependencies.
func (r *TaskRunner) Ensure() {
	r.state.Lock()
	defer r.state.Unlock()

	r.mu.Lock()
	defer r.mu.Unlock()

	for _, chg := range r.state.Changes() {
		if chg.Status() == DoneStatus {
			continue
		}

		tasks := chg.Tasks()
		for _, t := range tasks {
			// done, nothing to do
			if t.Status() == DoneStatus {
				continue
			}
			// we look at the Tomb instead of Status because
			// a task is always in RunningStatus even if
			// not running (FIXME?)
			if _, ok := r.tombs[t.ID()]; ok {
				continue
			}

			// check if there is anything we need to wait for
			if r.mustWait(t) {
				continue
			}

			// the task is ready to run (all prerequists done)
			// so full steam ahead!
			if fn, ok := r.handlers[t.Kind()]; ok {
				r.run(fn, t.ID())
			}
		}
	}
}

// Stop stops all concurrent activities and returns after that's done.
func (r *TaskRunner) Stop() {
	r.state.Lock()
	defer r.state.Unlock()

	for _, tb := range r.tombs {
		tb.Kill(nil)
		tb.Wait()
	}
}
