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

type HandlerFunc func(task *Task) error

// TaskRunner controls the running of goroutines to execute known task kinds.
type TaskRunner struct {
	state *State

	// locking
	mu       sync.Mutex
	handlers map[string]HandlerFunc

	// go-routines
	tomb tomb.Tomb
}

// NewTaskRunner creates a new TaskRunner
func NewTaskRunner(s *State) *TaskRunner {
	return &TaskRunner{
		state:    s,
		handlers: make(map[string]HandlerFunc),
	}
}

// AddHandler registers the function to concurrently call for handling
// tasks of the given kind.
func (r *TaskRunner) AddHandler(kind string, fn HandlerFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.handlers[kind] = fn
}

// Ensure starts new goroutines for all known tasks with no pending
// dependencies.
func (r *TaskRunner) Ensure() {
	r.state.Lock()
	defer r.state.Unlock()

	// see what changes and tasks are pending
	for _, chg := range r.state.Changes() {
		for _, t := range chg.Tasks() {
			// FIMXE: add task loop detection
			if len(t.WaitTasks()) > 0 {
				continue
			}

			// run stuff
			if fn, ok := r.handlers[t.Kind()]; ok {
				// FIXME: do something sensible with an error
				r.tomb.Go(func() error { return fn(t) })
			}
		}
	}
}

// Stop stops all concurrent activities and returns after that's done.
func (r *TaskRunner) Stop() {
	r.tomb.Kill(nil)
	r.tomb.Wait()

}
