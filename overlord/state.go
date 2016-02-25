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
	"sync"
	"sync/atomic"

	"github.com/ubuntu-core/snappy/logger"
)

// StateBackend is used by State to checkpoint on every unlock operation.
type StateBackend interface {
	Checkpoint(data []byte) error
}

// State represents an evolving system state that persists across restarts.
//
// The State is concurrency-safe, and all reads and writes to it must be
// performed with the state locked. It's a runtime error (panic) to perform
// operations without it.
//
// The state is persisted on every unlock operation via the StateBackend
// it was initialized with.
type State struct {
	// locking
	mu  sync.Mutex
	muC int32
	// storage
	backend StateBackend
	entries map[string]*json.RawMessage
}

// NewState returns a new empty state.
func NewState(backend StateBackend) *State {
	return &State{
		backend: backend,
		entries: make(map[string]*json.RawMessage),
	}
}

func (s *State) checkpointData() []byte {
	data, err := json.Marshal(s.entries)
	if err != nil {
		// this shouldn't happen, because the actual delicate serializing happens at various Set()s
		logger.Panicf("internal error: could not marshal state for checkpoiting: %v", err)
	}
	return data
}

// Lock acquires the state lock.
func (s *State) Lock() {
	s.mu.Lock()
	atomic.AddInt32(&s.muC, 1)
}

// Unlock releases the state lock and checkpoints the state.
// It does not return until the state is correctly checkpointed.
// After too many unsuccessful checkpoint attempts, it panics.
func (s *State) Unlock() {
	defer func() {
		atomic.AddInt32(&s.muC, -1)
		s.mu.Unlock()
	}()
	if s.backend != nil {
		// XXX: return error?
		s.backend.Checkpoint(s.checkpointData())
	}
}

func (s *State) ensureLocked() {
	c := atomic.LoadInt32(&s.muC)
	if c != 1 {
		panic("internal error: accessing state without lock")
	}
}

// ErrNoState represents the case of no state entry for a given key.
var ErrNoState = errors.New("no state entry for key")

// Get unmarshals the stored value associated with the provided key
// into the value parameter.
// It returns ErrNoState if there is no entry for key.
func (s *State) Get(key string, value interface{}) error {
	s.ensureLocked()
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
	s.ensureLocked()
	serialized, err := json.Marshal(value)
	if err != nil {
		logger.Panicf("internal error: could not marshal value for state entry %q: %v", key, err)
	}
	entryJSON := json.RawMessage(serialized)
	s.entries[key] = &entryJSON
}

// ReadState returns the state deserialized from r.
func ReadState(backend StateBackend, r io.Reader) (*State, error) {
	s := new(State)
	d := json.NewDecoder(r)
	err := d.Decode(&s.entries)
	if err != nil {
		return nil, err
	}
	return s, err
}
