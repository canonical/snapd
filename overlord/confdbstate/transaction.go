// -*- Mode: Go; indent-tabs-mode: t -*-
/*
 * Copyright (C) 2023-2024 Canonical Ltd
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

package confdbstate

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/snapcore/snapd/confdb"
	"github.com/snapcore/snapd/overlord/state"
)

// Transaction performs read and writes to a databag in an atomic way.
type Transaction struct {
	// previous is the databag without any changes applied to it and is the same
	// before or after changes are committed
	previous confdb.JSONDatabag
	// pristine databag, excluding any changes being made. Once the changes are
	// committed, this will include them
	pristine confdb.JSONDatabag

	ConfdbAccount string
	ConfdbName    string

	modified      confdb.JSONDatabag
	deltas        []pathValuePair
	appliedDeltas int

	abortingSnap string
	abortReason  string

	mu sync.RWMutex
}

type pathValuePair struct {
	path  []confdb.Accessor
	value any
}

// NewTransaction takes a getter and setter to read and write the databag.
func NewTransaction(st *state.State, account, confdbName string) (*Transaction, error) {
	// TODO: if the databag can't change mid-transaction, there's no need to re-read
	// this on commit and we can instead pass the bag in (easier testing)
	databag, err := readDatabag(st, account, confdbName)
	if err != nil {
		return nil, err
	}

	return &Transaction{
		pristine:      databag,
		previous:      databag,
		ConfdbAccount: account,
		ConfdbName:    confdbName,
	}, nil
}

type marshalledTransaction struct {
	Pristine confdb.JSONDatabag `json:"pristine,omitempty"`
	Previous confdb.JSONDatabag `json:"previous,omitempty"`

	ConfdbAccount string `json:"confdb-account,omitempty"`
	ConfdbName    string `json:"confdb-name,omitempty"`

	Modified      confdb.JSONDatabag `json:"modified,omitempty"`
	Deltas        []map[string]any   `json:"deltas,omitempty"`
	AppliedDeltas int                `json:"applied-deltas,omitempty"`

	AbortingSnap string `json:"aborting-snap,omitempty"`
	AbortReason  string `json:"abort-reason,omitempty"`
}

func (t *Transaction) MarshalJSON() ([]byte, error) {
	deltas := make([]map[string]any, 0, len(t.deltas))
	for _, delta := range t.deltas {
		deltas = append(deltas, map[string]any{
			confdb.JoinAccessors(delta.path): delta.value,
		})
	}

	return json.Marshal(marshalledTransaction{
		Pristine:      t.pristine,
		Previous:      t.previous,
		ConfdbAccount: t.ConfdbAccount,
		ConfdbName:    t.ConfdbName,
		Modified:      t.modified,
		Deltas:        deltas,
		AppliedDeltas: t.appliedDeltas,
		AbortingSnap:  t.abortingSnap,
		AbortReason:   t.abortReason,
	})
}

func (t *Transaction) UnmarshalJSON(data []byte) error {
	var mt marshalledTransaction
	if err := json.Unmarshal(data, &mt); err != nil {
		return err
	}

	var deltas []pathValuePair
	for _, delta := range mt.Deltas {
		for path, value := range delta {
			accs, err := confdb.ParsePathIntoAccessors(path, nil, confdb.ParseOptions{})
			if err != nil {
				return fmt.Errorf("internal error: cannot parse path %q: %v", path, err)
			}

			deltas = append(deltas, pathValuePair{path: accs, value: value})
		}
	}

	t.pristine = mt.Pristine
	t.previous = mt.Previous
	t.ConfdbAccount = mt.ConfdbAccount
	t.ConfdbName = mt.ConfdbName
	t.modified = mt.Modified
	t.deltas = deltas
	t.appliedDeltas = mt.AppliedDeltas
	t.abortingSnap = mt.AbortingSnap
	t.abortReason = mt.AbortReason

	return nil
}

// Set sets a value in the transaction's databag. The change isn't persisted
// until Commit returns without errors.
func (t *Transaction) Set(path []confdb.Accessor, value any) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.aborted() {
		return errors.New("cannot write to aborted transaction")
	}

	t.deltas = append(t.deltas, pathValuePair{path: path, value: value})
	return nil
}

// Unset unsets a value in the transaction's databag. The change isn't persisted
// until Commit returns without errors.
func (t *Transaction) Unset(path []confdb.Accessor) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.aborted() {
		return errors.New("cannot write to aborted transaction")
	}

	t.deltas = append(t.deltas, pathValuePair{path: path})
	return nil
}

// Get reads a value from the transaction's databag including uncommitted changes.
func (t *Transaction) Get(path []confdb.Accessor, constraints map[string]any) (any, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.aborted() {
		return nil, errors.New("cannot read from aborted transaction")
	}

	// if there aren't any changes, just use the pristine bag
	if len(t.deltas) == 0 {
		return t.pristine.Get(path, constraints)
	}

	if err := t.applyChanges(); err != nil {
		return nil, err
	}

	return t.modified.Get(path, constraints)
}

// Commit applies the previous writes and validates the final databag. If any
// error occurs, the original databag is kept.
func (t *Transaction) Commit(st *state.State, schema confdb.DatabagSchema) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.aborted() {
		return errors.New("cannot commit aborted transaction")
	}

	pristine, err := readDatabag(st, t.ConfdbAccount, t.ConfdbName)
	if err != nil {
		return err
	}

	if err := applyDeltas(pristine, t.deltas); err != nil {
		return err
	}

	data, err := pristine.Data()
	if err != nil {
		return err
	}

	if err := schema.Validate(data); err != nil {
		return err
	}

	// copy the databag before writing to make sure the writer can't modify into
	// and introduce changes in the transaction
	if err := writeDatabag(st, pristine.Copy(), t.ConfdbAccount, t.ConfdbName); err != nil {
		return err
	}

	t.pristine = pristine
	t.modified = nil
	t.deltas = nil
	t.appliedDeltas = 0
	return nil
}

func (t *Transaction) Clear(st *state.State) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.aborted() {
		return errors.New("cannot write to aborted transaction")
	}

	pristine, err := readDatabag(st, t.ConfdbAccount, t.ConfdbName)
	if err != nil {
		return err
	}

	t.pristine = pristine
	t.modified = nil
	t.deltas = nil
	t.appliedDeltas = 0
	return nil
}

func (t *Transaction) AlteredPaths() [][]confdb.Accessor {
	// TODO: maybe we can extend this to recurse into delta's value and figure out
	// the most specific key possible
	paths := make([][]confdb.Accessor, 0, len(t.deltas))
	for _, delta := range t.deltas {
		paths = append(paths, delta.path)
	}
	return paths
}

func (t *Transaction) applyChanges() error {
	// use a cached bag to apply and keep the changes
	if t.modified == nil {
		t.modified = t.pristine.Copy()
		t.appliedDeltas = 0
	}

	// apply new changes since the last Get/Data call
	if err := applyDeltas(t.modified, t.deltas[t.appliedDeltas:]); err != nil {
		t.modified = nil
		t.appliedDeltas = 0
		return err
	}

	t.appliedDeltas = len(t.deltas)
	return nil
}

func applyDeltas(bag confdb.JSONDatabag, deltas []pathValuePair) error {
	// changes must be applied in the order they were written
	for _, delta := range deltas {
		var err error
		if delta.value == nil {
			err = bag.Unset(delta.path)
		} else {
			err = bag.Set(delta.path, delta.value)
		}

		if err != nil {
			return err
		}
	}

	return nil
}

// Data returns the transaction's committed data.
func (t *Transaction) Data() ([]byte, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.aborted() {
		return nil, errors.New("cannot read from aborted transaction")
	}

	if err := t.applyChanges(); err != nil {
		return nil, err
	}

	return t.modified.Data()
}

// Abort prevents any further writes or reads to the transaction. It takes a
// snap and reason that can be used to surface information to the user.
func (t *Transaction) Abort(abortingSnap, reason string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.abortingSnap = abortingSnap
	t.abortReason = reason
}

func (t *Transaction) aborted() bool {
	return t.abortReason != ""
}

func (t *Transaction) AbortInfo() (snap, reason string) {
	return t.abortingSnap, t.abortReason
}

func (t *Transaction) Previous() confdb.Databag {
	return t.previous
}
