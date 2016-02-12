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
	"io"
)

// State represents a snapshot of the system state.
type State struct{}

// Get unmarshals the stored value associated with the provided key
// into the value parameter.
func (s *State) Get(key string, value interface{}) {
}

// Set associates value with key for future consulting by managers.
// The provided value must properly marshal and unmarshal with encoding/json.
func (s *State) Set(key string, value interface{}) {
}

// Copy returns an indepent copy of the state.
func (s *State) Copy() *State {
	return nil
}

// WriteState serializes the provided state into w.
func WriteState(s *State, w io.Writer) error {
	return nil
}

// ReadState returns the state deserialized from r.
func ReadState(r io.Reader) (*State, error) {
	return nil, nil
}

// Delta represents a list of state changes.
type Delta []DeltaItem

// DeltaItem represent a single state change, possibly with a reason for it.
type DeltaItem struct {
	Header  string // Header for grouping
	Summary string // Summary with textual description
	Reason  string // Optional reason for the change, available with sanitization
}

// MarshalText returns a human-oriented textual representation of the delta.
//
// This function turns Delta into an encoding.TextMarshaler.
func (d Delta) MarshalText() ([]byte, error) {
	return nil, nil
}
