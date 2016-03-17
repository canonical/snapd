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

// HandlerFunc is the type of function for the handlers
type HandlerFunc func(task *Task, tomb *tomb.Tomb) error

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

// taskFail marks task t and all tasks waiting (directly and indirectly on it) as in ErrorStatus.
func taskFail(t *Task) {
	mark := make([]*Task, 1, 10)
	mark[0] = t
	i := 0
	for i < len(mark) {
		mark[i].SetStatus(ErrorStatus)
		mark = append(mark, mark[i].HaltTasks()...)
		i++
	}
}

// run must be called with the state lock in place
func (r *TaskRunner) run(fn HandlerFunc, task *Task) {
	task.SetStatus(RunningStatus) // could have been set to waiting
	tb := &tomb.Tomb{}
	r.tombs[task.ID()] = tb
	tb.Go(func() error {
		err := fn(task, tb)

		r.state.Lock()
		defer r.state.Unlock()
		halted := task.HaltTasks()
		if err == nil && tb.Err() == tomb.ErrStillAlive {
			task.SetStatus(DoneStatus)
		} else if err == nil && tb.Err() == nil {
			// just stopped, preserve status
		} else {
			taskFail(task)
		}
		if len(halted) > 0 {
			r.state.EnsureBefore(0)
		}
		return err
	})
}

// mustWait must be called with the state lock in place
func mustWait(t *Task) bool {
	for _, wt := range t.WaitTasks() {
		if wt.Status() != DoneStatus {
			return true
		}
	}

	return false
}

// isPointless returns true if all the tasks waiting on t are already in error
// and there are waiting tasks at all
func isPointless(t *Task) bool {
	halted := t.HaltTasks()
	if len(halted) == 0 {
		return false
	}
	for _, ht := range t.HaltTasks() {
		if ht.Status() != ErrorStatus {
			return false
		}
	}
	return true
}

// Ensure starts new goroutines for all known tasks with no pending
// dependencies.
// Note that Ensure will lock the state.
func (r *TaskRunner) Ensure() {
	r.state.Lock()
	defer r.state.Unlock()

	r.mu.Lock()
	defer r.mu.Unlock()

	for id, tb := range r.tombs {
		if !tb.Alive() {
			delete(r.tombs, id)
		}
	}

	for _, chg := range r.state.Changes() {
		if chg.Status() == DoneStatus {
			continue
		}

		tasks := chg.Tasks()
		for _, t := range tasks {
			// done or error are final, nothing to do
			status := t.Status()
			if status == DoneStatus || status == ErrorStatus {
				continue
			}

			// No handler for the given kind of task,
			// this means another taskrunner is going
			// to handle this task.
			if _, ok := r.handlers[t.Kind()]; !ok {
				continue
			}

			// we look at the Tomb instead of Status because
			// a task can be in RunningStatus even when it
			// is not started yet (like when the daemon
			// process restarts)
			if tomb, ok := r.tombs[t.ID()]; ok {
				if isPointless(t) {
					tomb.Killf("waiting tasks are already in error")
				}
				continue
			}

			// check if there is anything we need to wait for
			if mustWait(t) {
				continue
			}

			// the task is ready to run (all prerequists done)
			// so full steam ahead!
			r.run(r.handlers[t.Kind()], t)
		}
	}
}

// Stop kills all concurrent activities and returns after that's done.
func (r *TaskRunner) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, tb := range r.tombs {
		tb.Kill(nil)
	}

	for id, tb := range r.tombs {
		tb.Wait()
		delete(r.tombs, id)
	}
}

// Wait waits for all concurrent activities and returns after that's done.
func (r *TaskRunner) Wait() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for len(r.tombs) > 0 {
		for id, t := range r.tombs {
			r.mu.Unlock()
			t.Wait()
			r.mu.Lock()
			delete(r.tombs, id)
			break
		}
	}
}
