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
	undoes []func() error
	t      *state.Task
}

func taskNotNil(t *state.Task) {
	if t == nil {
		panic("internal error: task cannot be nil")
	}
}

// NewUndoTracker creates a new UndoTracker associated with the given task.
// The task is used to log undo errors.
func NewUndoTracker(t *state.Task) *UndoTracker {
	taskNotNil(t)
	return &UndoTracker{t: t}
}

// AddUndo registers an undo closure to be executed if the handler returns an error.
// The closure should reverse a change to the system and return an error if the
// undo action itself fails. The closure runs with the state lock held.
func (ut *UndoTracker) AddUndo(f func() error) {
	taskNotNil(ut.t)
	ut.undoes = append(ut.undoes, f)
}

// AddUnlockedUndo registers an undo closure to be executed if the handler returns an error.
// The closure should reverse a change to the system and return an error if the
// undo action itself fails. The closure runs with the state lock released, i.e.,
// the state is unlocked before calling the closure and re-locked after it returns.
func (ut *UndoTracker) AddUnlockedUndo(f func() error) {
	taskNotNil(ut.t)
	st := ut.t.State()
	ut.undoes = append(ut.undoes, func() error {
		st.Unlock()
		defer st.Lock()
		return f()
	})
}

// RunOnError should generally be deferred at the start of the handler.
// It runs all registered undo closures in reverse order if *retErr is
// a real error (not nil and not a *state.Wait). Undo errors are logged
// via the task's Errorf, so the caller should lock the state before
// calling this function.
func (ut *UndoTracker) RunOnError(retErr *error) {
	taskNotNil(ut.t)
	if retErr == nil {
		panic("internal error: retErr pointer cannot be nil")
	}
	if !IsErrAndNotWait(*retErr) {
		return
	}
	for i := len(ut.undoes) - 1; i >= 0; i-- {
		if err := ut.undoes[i](); err != nil {
			ut.t.Errorf("cannot undo: %v", err)
		}
	}
}
