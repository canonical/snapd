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
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/ubuntu-core/snappy/logger"
)

// State represents a snapshot of the system state.
type State struct {
	entries map[string]*json.RawMessage
}

// NewState returns an empty system state.
func NewState() *State {
	return &State{
		entries: make(map[string]*json.RawMessage),
	}
}

// ErrNoState represents the case of no state entry for a given key.
var ErrNoState = errors.New("no state entry for key")

// Get unmarshals the stored value associated with the provided key
// into the value parameter.
// It returns ErrNoState if there is no entry for key.
func (s *State) Get(key string, value interface{}) error {
	entryJSON := s.entries[key]
	if entryJSON == nil {
		return ErrNoState
	}
	err := json.Unmarshal(*entryJSON, value)
	if err != nil {
		return fmt.Errorf("internal error: could not unmarshal state entry %q: %v", key, err)
	}
	return nil
}

// Set associates value with key for future consulting by managers.
// The provided value must properly marshal and unmarshal with encoding/json.
func (s *State) Set(key string, value interface{}) {
	serialized, err := json.Marshal(value)
	if err != nil {
		logger.Panicf("internal error: could not marshal value for state entry %q: %v", key, err)
	}
	entryJSON := json.RawMessage(serialized)
	s.entries[key] = &entryJSON
}

// Copy returns an independent copy of the state.
func (s *State) Copy() *State {
	entries := make(map[string]*json.RawMessage, len(s.entries))
	for k, s := range s.entries {
		entries[k] = s
	}
	return &State{entries: entries}
}

// WriteState serializes the provided state into w.
func WriteState(s *State, w io.Writer) error {
	e := json.NewEncoder(w)
	return e.Encode(s.entries)
}

// ReadState returns the state deserialized from r.
func ReadState(r io.Reader) (*State, error) {
	s := new(State)
	d := json.NewDecoder(r)
	err := d.Decode(&s.entries)
	if err != nil {
		return nil, err
	}
	return s, err
}
