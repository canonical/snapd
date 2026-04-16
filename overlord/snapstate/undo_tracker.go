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
	"github.com/snapcore/snapd/overlord/state"
)

// UndoTracker accumulates undo closures during task handler execution.
// When the handler returns an error, RunOnError executes the undo
// closures in reverse (LIFO) order. On success, nothing runs. This
// allows do handlers to incrementally register undo actions for each
// step, ensuring partial progress is automatically rolled back on
// failure.
type UndoTracker struct {
	undoes []undoEntry
	t      *state.Task
}

// undoEntry represents a single undo closure with its required state lock
// context.
type undoEntry struct {
	f        func() error
	unlocked bool // if true, the closure runs with the state lock released
}

// NewUndoTracker creates a new UndoTracker associated with the given task.
// The task is used to log undo errors.
func NewUndoTracker(t *state.Task) *UndoTracker {
	if t == nil {
		panic("internal error: task cannot be nil")
	}
	return &UndoTracker{t: t}
}

// AddUndo registers an undo closure to be executed if the handler returns an error.
// The closure should reverse a change to the system and return an error if the
// undo action itself fails. The closure runs with the state lock held.
func (ut *UndoTracker) AddUndo(f func() error) {
	ut.undoes = append(ut.undoes, undoEntry{f: f})
}

// RunOnError should generally be deferred at the start of the handler.
// It runs all registered undo closures in reverse order if *retErr is
// a real error (not nil and not a *state.Wait).
//
// RunOnError expects the state lock to be held on entry and guarantees
// it is held on return. It transitions the lock state as needed for
// each undo entry: locked entries run with the lock held, unlocked
// entries run with the lock released. Undo errors are collected and
// logged via the task's Errorf after all undoes complete, with the
// state lock held.
func (ut *UndoTracker) RunOnError(retErr *error) {
	if retErr == nil {
		panic("internal error: retErr pointer cannot be nil")
	}
	if !IsErrAndNotWait(*retErr) {
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

	for i := len(ut.undoes) - 1; i >= 0; i-- {
		entry := ut.undoes[i]
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

// Unlocked returns an adapter whose AddUndo method tags all registered
// undo closures as requiring unlocked state execution. The caller
// passes this where only AddUndo should be visible, while retaining
// control over lock context decisions.
func (ut *UndoTracker) Unlocked() *UnlockedUndoTracker {
	return &UnlockedUndoTracker{ut}
}

// UnlockedUndoTracker embeds *UndoTracker and shadows AddUndo method to tag
// registered closures as needing unlocked state execution.
type UnlockedUndoTracker struct {
	*UndoTracker
}

// AddUndo registers an undo closure that will run with the state lock
// released.
func (u *UnlockedUndoTracker) AddUndo(f func() error) {
	u.undoes = append(u.undoes, undoEntry{f: f, unlocked: true})
}
