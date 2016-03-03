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

// Package state implements the representation of system state.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ubuntu-core/snappy/logger"
	"github.com/ubuntu-core/snappy/strutil"
)

// A Backend is used by State to checkpoint on every unlock operation.
type Backend interface {
	Checkpoint(data []byte) error
}

type customEntries map[string]*json.RawMessage

func (entries customEntries) get(key string, value interface{}) error {
	entryJSON := entries[key]
	if entryJSON == nil {
		return ErrNoState
	}
	err := json.Unmarshal(*entryJSON, value)
	if err != nil {
		return fmt.Errorf("internal error: could not unmarshal state entry %q: %v", key, err)
	}
	return nil
}

func (entries customEntries) set(key string, value interface{}) {
	serialized, err := json.Marshal(value)
	if err != nil {
		logger.Panicf("internal error: could not marshal value for state entry %q: %v", key, err)
	}
	entryJSON := json.RawMessage(serialized)
	entries[key] = &entryJSON
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
	backend Backend
	entries customEntries
	changes map[string]*Change
}

// New returns a new empty state.
func New(backend Backend) *State {
	return &State{
		backend: backend,
		entries: make(customEntries),
		changes: make(map[string]*Change),
	}
}

type marshalledState struct {
	Entries map[string]*json.RawMessage `json:"entries"`
	Changes map[string]*Change          `json:"changes"`
}

// MarshalJSON makes State a json.Marshaller
func (s *State) MarshalJSON() ([]byte, error) {
	return json.Marshal(marshalledState{
		Entries: s.entries,
		Changes: s.changes,
	})
}

// UnmarshalJSON makes State a json.Unmarshaller
func (s *State) UnmarshalJSON(data []byte) error {
	var unmarshalled marshalledState
	err := json.Unmarshal(data, &unmarshalled)
	if err != nil {
		return err
	}
	s.entries = unmarshalled.Entries
	s.changes = unmarshalled.Changes
	// backlink state again
	for _, chg := range s.changes {
		chg.state = s
	}
	return nil
}

func (s *State) checkpointData() []byte {
	data, err := json.Marshal(s)
	if err != nil {
		// this shouldn't happen, because the actual delicate serializing happens at various Set()s
		logger.Panicf("internal error: could not marshal state for checkpointing: %v", err)
	}
	return data
}

// Lock acquires the state lock.
func (s *State) Lock() {
	s.mu.Lock()
	atomic.AddInt32(&s.muC, 1)
}

// unlock checkpoint retry parameters (5 mins of retries by default)
var (
	unlockCheckpointRetryMaxTime  = 5 * time.Minute
	unlockCheckpointRetryInterval = 3 * time.Second
)

// Unlock releases the state lock and checkpoints the state.
// It does not return until the state is correctly checkpointed.
// After too many unsuccessful checkpoint attempts, it panics.
func (s *State) Unlock() {
	defer func() {
		atomic.AddInt32(&s.muC, -1)
		s.mu.Unlock()
	}()
	if s.backend != nil {
		data := s.checkpointData()
		var err error
		start := time.Now()
		for time.Since(start) <= unlockCheckpointRetryMaxTime {
			if err = s.backend.Checkpoint(data); err == nil {
				return
			}
			time.Sleep(unlockCheckpointRetryInterval)
		}
		logger.Panicf("cannot checkpoint even after %v of retries every %v: %v", unlockCheckpointRetryMaxTime, unlockCheckpointRetryInterval, err)
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
	return s.entries.get(key, value)
}

// Set associates value with key for future consulting by managers.
// The provided value must properly marshal and unmarshal with encoding/json.
func (s *State) Set(key string, value interface{}) {
	s.ensureLocked()
	s.entries.set(key, value)
}

func (s *State) genID() string {
	for {
		id := strutil.MakeRandomString(6)
		if _, ok := s.changes[id]; ok {
			continue
		}
		return id
	}
}

// NewChange adds a new change to the state.
func (s *State) NewChange(kind, summary string) *Change {
	s.ensureLocked()
	id := s.genID()
	chg := newChange(s, id, kind, summary)
	s.changes[id] = chg
	return chg
}

// Changes returns all changes currently known to the state.
func (s *State) Changes() []*Change {
	s.ensureLocked()
	res := make([]*Change, 0, len(s.changes))
	for _, chg := range s.changes {
		res = append(res, chg)
	}
	return res
}

// ReadState returns the state deserialized from r.
func ReadState(backend Backend, r io.Reader) (*State, error) {
	s := new(State)
	d := json.NewDecoder(r)
	err := d.Decode(&s)
	if err != nil {
		return nil, err
	}
	s.backend = backend
	return s, err
}
