// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package snapstate

import (
	"errors"
	"sync/atomic"

	"github.com/snapcore/snapd/overlord/state"
)

// Undoer collects undo actions to reverse system changes on error.
type Undoer interface {
	AddUndo(f func() error)
}

// UndoTracker collects undo closures during task handler execution
// and runs them if the handler returns an error (not nil and neither
// a *state.Wait nor a *state.Retry).
// It should only be used within the task handler goroutine.
// It is meant to be used as follows: at the start of the handler,
// a new UndoTracker is created via NewUndoTracker() and the
// returned closure is deferred. As the handler makes progress undos
// are registered via Locked().AddUndo() or Unlocked().AddUndo() depending
// on whether the undo needs to run with the state locked or unlocked.
// If the handler fails in-flight, the returned closure executes the
// undos in reverse (LIFO) order. On success, nothing runs. If skipped
// using Skip() and there was a real error, the undos are not executed
// but the skip reason is logged in the task.
// This allows do handlers to incrementally register undo actions for
// each step, ensuring partial progress is automatically rolled back on
// failure.
type UndoTracker struct {
	undos      []undoEntry
	t          *state.Task
	runCalled  uint32 // TODO:GOVERSION: use atomic.Uint32 once on go 1.19+
	skipReason string
}

// undoEntry represents a single undo closure with its required state
// lock context.
type undoEntry struct {
	f        func() error
	unlocked bool // if true, the closure runs with the state unlocked
}

// NewUndoTracker returns a new UndoTracker associated with the given
// task and a closure that should be deferred to run the undo closures
// on error. The returned closure expects the state to be locked on
// entry and guarantees it is locked on return.
// The task is used to log undo errors and to maintain the state lock
// context as required for each undo.
// retErr is the pointer to the error return value of the task handler,
// which the returned closure checks to decide whether to run the undos.
func NewUndoTracker(t *state.Task, retErr *error) (*UndoTracker, func()) {
	ut := &UndoTracker{t: t}
	return ut, func() { ut.run(retErr) }
}

// run runs all registered undo closures in reverse order if
// *retErr is a real error (not nil and neither a *state.Wait nor a
// *state.Retry). It expects the state to be locked on entry and
// guarantees it is locked on return. It transitions the state lock
// as needed for each undo entry: locked entries run with the state
// locked, unlocked entries run with the state unlocked. Undo errors
// are collected and logged in the task after all undos complete.
// If skipped using Skip() and there was a real error, the undos are
// not executed but the skip reason is logged in the task.
func (ut *UndoTracker) run(retErr *error) {
	if atomic.SwapUint32(&ut.runCalled, 1) != 0 {
		panic("internal error: cannot call UndoTracker.run more than once")
	}

	re := *retErr
	var w *state.Wait
	var r *state.Retry
	if re == nil || errors.As(re, &w) || errors.As(re, &r) {
		return
	}

	if ut.skipReason != "" {
		ut.t.Logf("skipping undos, reason: %s", ut.skipReason)
		return
	}

	st := ut.t.State()
	locked := true
	var errs []error

	defer func() {
		// Ensure state is locked before returning and errors are logged
		if !locked {
			st.Lock()
		}
		for _, err := range errs {
			ut.t.Errorf("cannot undo: %v", err)
		}
	}()

	// keep the state locked or unlocked between consecutive entries with
	// the same lock context requirement and switch only when needed
	for i := len(ut.undos) - 1; i >= 0; i-- {
		entry := ut.undos[i]
		if entry.unlocked && locked {
			st.Unlock()
			locked = false
		} else if !entry.unlocked && !locked {
			st.Lock()
			locked = true
		}
		if err := entry.f(); err != nil {
			errs = append(errs, err)
		}
	}
}

// Skip marks that undos should be skipped even if there is an error
// in the task handler but the skip reason will be logged in the task.
func (ut *UndoTracker) Skip(reason string) {
	if atomic.LoadUint32(&ut.runCalled) != 0 {
		panic("internal error: cannot Skip after undos execution has started")
	}
	ut.skipReason = reason
}

func (ut *UndoTracker) addUndo(entry undoEntry) {
	if atomic.LoadUint32(&ut.runCalled) != 0 {
		panic("internal error: cannot register undo after undos execution has started")
	}

	if ut.skipReason != "" {
		panic("internal error: cannot register undo as they are marked to be skipped")
	}
	ut.undos = append(ut.undos, entry)
}

// Locked returns an Undoer which allows registering
// undo closures that need to be run with the state locked.
func (ut *UndoTracker) Locked() Undoer {
	return &undoerWithLockContext{ut: ut, unlocked: false}
}

// Unlocked returns an Undoer which allows registering
// undo closures that need to be run with the state unlocked.
func (ut *UndoTracker) Unlocked() Undoer {
	return &undoerWithLockContext{ut: ut, unlocked: true}
}

// undoerWithLockContext implements the Undoer interface.
// It can be constructed via UndoTracker.Locked() or
// UndoTracker.Unlocked() and uses the underlying UndoTracker's undo
// stack for collecting the undo closures.
type undoerWithLockContext struct {
	ut       *UndoTracker
	unlocked bool
}

// AddUndo registers an undo closure to be executed if the handler
// returns an error. The closure should reverse a change to the system
// and return an error if the undo action itself fails.
// The closure is added to the undo stack of the underlying UndoTracker.
func (u *undoerWithLockContext) AddUndo(f func() error) {
	u.ut.addUndo(undoEntry{f: f, unlocked: u.unlocked})
}

// nullUndoer is a no-op implementation of the Undoer interface.
type nullUndoer struct{}

func (nu nullUndoer) AddUndo(f func() error) {}

// NullUndoer is an Undoer that does nothing. It is meant to be used
// when system changes should not be undone, for example in the undo
// functions of task handlers.
var NullUndoer = nullUndoer{}

// TodoUndoer is an Undoer that does nothing. It is meant to be used
// when system changes should be undone, but the caller has not yet
// added support for passing UndoTracker.Locked() or UndoTracker.Unlocked().
var TodoUndoer = nullUndoer{}
