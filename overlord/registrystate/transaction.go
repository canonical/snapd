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

package registrystate

import (
	"encoding/json"
	"sync"

	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/registry"
)

// Transaction performs read and writes to a databag in an atomic way.
type Transaction struct {
	pristine registry.JSONDataBag

	RegistryAccount string
	RegistryName    string

	modified      registry.JSONDataBag
	deltas        []map[string]interface{}
	appliedDeltas int

	mu sync.RWMutex
}

// NewTransaction takes a getter and setter to read and write the databag.
func NewTransaction(st *state.State, account, registryName string) (*Transaction, error) {
	databag, err := readDatabag(st, account, registryName)
	if err != nil {
		return nil, err
	}

	return &Transaction{
		pristine:        databag,
		RegistryAccount: account,
		RegistryName:    registryName,
	}, nil
}

type marshalledTransaction struct {
	Pristine registry.JSONDataBag `json:"pristine,omitempty"`

	RegistryAccount string `json:"registry-account,omitempty"`
	RegistryName    string `json:"registry-name,omitempty"`

	Modified      registry.JSONDataBag     `json:"modified,omitempty"`
	Deltas        []map[string]interface{} `json:"deltas,omitempty"`
	AppliedDeltas int                      `json:"applied-deltas,omitempty"`
}

func (t *Transaction) MarshalJSON() ([]byte, error) {
	return json.Marshal(marshalledTransaction{
		Pristine:        t.pristine,
		RegistryAccount: t.RegistryAccount,
		RegistryName:    t.RegistryName,
		Modified:        t.modified,
		Deltas:          t.deltas,
		AppliedDeltas:   t.appliedDeltas,
	})
}

func (t *Transaction) UnmarshalJSON(data []byte) error {
	var mt marshalledTransaction
	if err := json.Unmarshal(data, &mt); err != nil {
		return err
	}

	t.pristine = mt.Pristine
	t.RegistryAccount = mt.RegistryAccount
	t.RegistryName = mt.RegistryName
	t.modified = mt.Modified
	t.deltas = mt.Deltas
	t.appliedDeltas = mt.AppliedDeltas

	return nil
}

// Set sets a value in the transaction's databag. The change isn't persisted
// until Commit returns without errors.
func (t *Transaction) Set(path string, value interface{}) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.deltas = append(t.deltas, map[string]interface{}{path: value})
	return nil
}

// Unset unsets a value in the transaction's databag. The change isn't persisted
// until Commit returns without errors.
func (t *Transaction) Unset(path string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.deltas = append(t.deltas, map[string]interface{}{path: nil})
	return nil
}

// Get reads a value from the transaction's databag including uncommitted changes.
func (t *Transaction) Get(path string) (interface{}, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// if there aren't any changes, just use the pristine bag
	if len(t.deltas) == 0 {
		return t.pristine.Get(path)
	}

	if err := t.applyChanges(); err != nil {
		return nil, err
	}

	return t.modified.Get(path)
}

// Commit applies the previous writes and validates the final databag. If any
// error occurs, the original databag is kept.
func (t *Transaction) Commit(st *state.State, schema registry.Schema) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	pristine, err := readDatabag(st, t.RegistryAccount, t.RegistryName)
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
	if err := writeDatabag(st, pristine.Copy(), t.RegistryAccount, t.RegistryName); err != nil {
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

	pristine, err := readDatabag(st, t.RegistryAccount, t.RegistryName)
	if err != nil {
		return err
	}

	t.pristine = pristine
	t.modified = nil
	t.deltas = nil
	t.appliedDeltas = 0
	return nil
}

func (t *Transaction) AlteredPaths() []string {
	// TODO: maybe we can extend this to recurse into delta's value and figure out
	// the most specific key possible
	paths := make([]string, 0, len(t.deltas))
	for _, delta := range t.deltas {
		for path := range delta {
			paths = append(paths, path)
		}
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

func applyDeltas(bag registry.JSONDataBag, deltas []map[string]interface{}) error {
	// changes must be applied in the order they were written
	for _, delta := range deltas {
		for k, v := range delta {
			var err error
			if v == nil {
				err = bag.Unset(k)
			} else {
				err = bag.Set(k, v)
			}

			if err != nil {
				return err
			}
		}
	}

	return nil
}

// Data returns the transaction's committed data.
func (t *Transaction) Data() ([]byte, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if err := t.applyChanges(); err != nil {
		return nil, err
	}

	return t.modified.Data()
}
