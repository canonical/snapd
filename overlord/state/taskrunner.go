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
	"errors"
	"sync"

	"gopkg.in/tomb.v2"

	"github.com/ubuntu-core/snappy/logger"
)

// HandlerFunc is the type of function for the handlers
type HandlerFunc func(task *Task, tomb *tomb.Tomb) error

// Retry is returned from a handler to signal that is ok to rerun the
// task at a later point. It's to be used also when a task goroutine
// is asked to stop through its tomb.
var Retry = errors.New("task should be retried")

// TaskRunner controls the running of goroutines to execute known task kinds.
type TaskRunner struct {
	state *State

	// locking
	mu       sync.Mutex
	handlers map[string]handlerPair
	stopped  bool

	// go-routines lifecycle
	tombs map[string]*tomb.Tomb
}

type handlerPair struct {
	do, undo HandlerFunc
}

// NewTaskRunner creates a new TaskRunner
func NewTaskRunner(s *State) *TaskRunner {
	return &TaskRunner{
		state:    s,
		handlers: make(map[string]handlerPair),
		tombs:    make(map[string]*tomb.Tomb),
	}
}

// AddHandler registers the functions to concurrently call for doing and
// undoing tasks of the given kind. The undo handler may be nil.
func (r *TaskRunner) AddHandler(kind string, do, undo HandlerFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.handlers[kind] = handlerPair{do, undo}
}

// run must be called with the state lock in place
func (r *TaskRunner) run(t *Task) {
	var handler HandlerFunc
	switch t.Status() {
	case DoStatus:
		t.SetStatus(DoingStatus)
		fallthrough
	case DoingStatus:
		handler = r.handlers[t.Kind()].do

	case UndoStatus:
		t.SetStatus(UndoingStatus)
		fallthrough
	case UndoingStatus:
		handler = r.handlers[t.Kind()].undo

	default:
		panic("internal error: attempted to run task in status " + t.Status().String())
	}
	if handler == nil {
		panic("internal error: attempted to run task with nil handler for status " + t.Status().String())
	}

	tomb := &tomb.Tomb{}
	r.tombs[t.ID()] = tomb
	tomb.Go(func() error {
		// Capture the error result with tomb.Kill so we can
		// use tomb.Err uniformily to consider both it or a
		// overriding previous Kill reason.
		tomb.Kill(handler(t, tomb))

		// Locks must be acquired in the same order everywhere.
		r.mu.Lock()
		defer r.mu.Unlock()
		r.state.Lock()
		defer r.state.Unlock()

		delete(r.tombs, t.ID())

		switch err := tomb.Err(); err {
		case Retry:
			// Handler asked to be called again later.
			// TODO Allow postponing retries past the next Ensure.
			if t.Status() == AbortStatus {
				// Would work without it but might take two ensures.
				r.tryUndo(t)
			}
		case nil:
			var next []*Task
			switch t.Status() {
			case DoingStatus:
				t.SetStatus(DoneStatus)
				next = t.HaltTasks()
			case AbortStatus:
				t.SetStatus(DoneStatus) // Not actually aborted.
				r.tryUndo(t)
			case UndoingStatus:
				t.SetStatus(UndoneStatus)
				next = t.WaitTasks()
			}
			if len(next) > 0 {
				r.state.EnsureBefore(0)
			}
		default:
			t.SetStatus(ErrorStatus)
			t.Errorf("%s", err)
			r.abortChange(t.Change())
		}

		return nil
	})
}

func (r *TaskRunner) abortChange(chg *Change) {
	chg.Abort()
	ensureScheduled := false
	for _, t := range chg.Tasks() {
		status := t.Status()
		if status == AbortStatus {
			if tb, ok := r.tombs[t.ID()]; ok {
				tb.Kill(nil)
			}
		}
		if !ensureScheduled && !status.Ready() {
			ensureScheduled = true
			r.state.EnsureBefore(0)
		}
	}
}

// tryUndo replaces the status of a knowingly aborted task.
func (r *TaskRunner) tryUndo(t *Task) {
	if t.Status() == AbortStatus && r.handlers[t.Kind()].undo == nil {
		// Cannot undo but it was stopped in flight.
		// Hold so it doesn't look like it finished.
		t.SetStatus(HoldStatus)
		if len(t.WaitTasks()) > 0 {
			r.state.EnsureBefore(0)
		}
	} else {
		t.SetStatus(UndoStatus)
		r.state.EnsureBefore(0)
	}
}

// Ensure starts new goroutines for all known tasks with no pending
// dependencies.
// Note that Ensure will lock the state.
func (r *TaskRunner) Ensure() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.stopped {
		// we are stopping, don't run another ensure
		return
	}

	// Locks must be acquired in the same order everywhere.
	r.state.Lock()
	defer r.state.Unlock()

	for _, t := range r.state.Tasks() {
		handlers, ok := r.handlers[t.Kind()]
		if !ok {
			// Handled by a different runner instance.
			continue
		}

		tb := r.tombs[t.ID()]

		if t.Status() == AbortStatus {
			if tb != nil {
				tb.Kill(nil)
				continue
			}
			r.tryUndo(t)
		}

		if tb != nil {
			// Already being handled.
			continue
		}

		status := t.Status()
		if status.Ready() {
			continue
		}
		if status == UndoStatus && handlers.undo == nil {
			// Cannot undo. Revert to done status.
			t.SetStatus(DoneStatus)
			if len(t.WaitTasks()) > 0 {
				r.state.EnsureBefore(0)
			}
			continue
		}

		if mustWait(t) {
			// Dependencies still unhandled.
			continue
		}
		logger.Debugf("Running task %s: %s", t.ID(), t.Summary())
		r.run(t)
	}
}

// mustWait returns whether task t must wait for other tasks to be done.
func mustWait(t *Task) bool {
	switch t.Status() {
	case DoStatus:
		for _, wt := range t.WaitTasks() {
			if wt.Status() != DoneStatus {
				return true
			}
		}
	case UndoStatus:
		for _, ht := range t.HaltTasks() {
			if !ht.Status().Ready() {
				return true
			}
		}
	}
	return false
}

// wait expectes to be called with th r.mu lock held
func (r *TaskRunner) wait() {
	for len(r.tombs) > 0 {
		for _, t := range r.tombs {
			r.mu.Unlock()
			t.Wait()
			r.mu.Lock()
			break
		}
	}
}

// Stop kills all concurrent activities and returns after that's done.
func (r *TaskRunner) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.stopped = true

	for _, tb := range r.tombs {
		tb.Kill(nil)
	}

	r.wait()
}

// Wait waits for all concurrent activities and returns after that's done.
func (r *TaskRunner) Wait() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.wait()
}
