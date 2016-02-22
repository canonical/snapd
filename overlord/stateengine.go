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

import (
	"encoding"
)

// StateManager is implemented by types responsible for observing
// the system state and directly manipulating it.
//
// See the interface of StateEngine for details on those methods.
type StateManager interface {
	Apply(s *State) error
	Learn(s *State) error
	Sanitize(s *State) error
	Delta(a, b *State) (Delta, error)
}

// sanity
var _ encoding.TextMarshaler = Delta(nil)

// StateEngine controls the dispatching of state changes to state managers.
//
// The operations performed by StateEngine resemble in some ways a
// transaction system, but it needs to deal with the fact that many of the
// system changes involved in a snappy system are not atomic, and may
// actually become invalid without notice (e.g. USB device physically removed).
type StateEngine struct{}

// NewStateEngine returns a new state engine.
func NewStateEngine() *StateEngine {
	return &StateEngine{}
}

// Apply attempts to perform the necessary changes in the system to make s
// the current state.
func (se *StateEngine) Apply(s *State) error {
	return nil
}

// Learn records the current state into s.
func (se *StateEngine) Learn(s *State) error {
	return nil
}

// Delta returns the differences between state a and b,
// or nil if there are no relevant differences.
func (se *StateEngine) Delta(a, b *State) (Delta, error) {
	return nil, nil
}

// Sanitize attempts to make the necessary changes in the
// provided state to make it ready for applying. It returns
// a Delta with the changes performed and the reasoning for them.
func (se *StateEngine) Sanitize(s *State) (Delta, error) {
	return nil, nil
}

// Validate checks whether the s state might be applied as-is if desired.
// It's implemented in terms of Sanitize and Delta.
func (se *StateEngine) Validate(s *State) error {
	return nil
}

// AddManager adds the provided manager to take part in state operations.
func (se *StateEngine) AddManager(m StateManager) {
	// XXX: how is Apply and Sanitize order picked?
}
