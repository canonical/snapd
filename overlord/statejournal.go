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

package overlord

// StateJournal is responsible for keeping track of multiple states
// persistently and recording intended state changes so that the system
// may be properly recovered from an interruption by moving into the
// intended state completely, or retroceding to a prior good state.
type StateJournal struct {
	eng *StateEngine
}

// NewStateJournal returns a new state journal using dir as its configuration directory.
func NewStateJournal(engine *StateEngine, dir string) (*StateJournal, error) {
	// XXX: get current state or be lazy?
	return &StateJournal{eng: engine}, nil
}

// Current returns the current system state.
func (j *StateJournal) Current() (*State, error) {
	return nil, nil
}

// Commit attempts to apply the s state, recording it in the journal
// first to ensure the operation may be continued later if necessary.
func (j *StateJournal) Commit(s *State) error {
	return nil
}

// Pending returns the last attempted but unfinished commit, if any.
// It returns ErrNotPending if there are no pending states.
func (j *StateJournal) Pending() (*State, error) {
	return nil, nil
}

// Recover attempts to apply the currently pending state, if any.
// If applying the pending state fails for any reason and there's
// a previous good state, applying that will be attempted instead.
func (j *StateJournal) Recover() error {
	return nil
}

// Revert attempts to revert the system to the previous known good
// state, whether the current state is valid or not. This is unlike
// Recover in that it ignores whether a pending state exists.
func (j *StateJournal) Revert() error {
	return nil
}
