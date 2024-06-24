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

package registry

import (
	"sync"
)

type DatabagRead func() (JSONDataBag, error)
type DatabagWrite func(JSONDataBag) error

// Transaction performs read and writes to a databag in an atomic way.
type Transaction struct {
	pristine JSONDataBag
	registry *Registry

	modified      JSONDataBag
	deltas        []map[string]interface{}
	appliedDeltas int

	readDatabag  DatabagRead
	writeDatabag DatabagWrite
	mu           sync.RWMutex
}

// NewTransaction takes a getter and setter to read and write the databag.
func NewTransaction(reg *Registry, readDatabag DatabagRead, writeDatabag DatabagWrite) (*Transaction, error) {
	databag, err := readDatabag()
	if err != nil {
		return nil, err
	}

	return &Transaction{
		pristine:     databag.Copy(),
		registry:     reg,
		readDatabag:  readDatabag,
		writeDatabag: writeDatabag,
	}, nil
}

func (t *Transaction) RegistryInfo() (account string, registryName string) {
	return t.registry.Account, t.registry.Name
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
	t.mu.RLock()
	defer t.mu.RUnlock()

	// if there aren't any changes, just use the pristine bag
	if len(t.deltas) == 0 {
		return t.pristine.Get(path)
	}

	// if there are changes, use a cached bag with modifications to do the Get
	if t.modified == nil {
		t.modified = t.pristine.Copy()
		t.appliedDeltas = 0
	}

	// apply new changes since the last get
	if err := applyDeltas(t.modified, t.deltas[t.appliedDeltas:]); err != nil {
		t.modified = nil
		t.appliedDeltas = 0
		return nil, err
	}
	t.appliedDeltas = len(t.deltas)

	return t.modified.Get(path)
}

// Commit applies the previous writes and validates the final databag. If any
// error occurs, the original databag is kept.
func (t *Transaction) Commit() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	pristine, err := t.readDatabag()
	if err != nil {
		return err
	}

	// ensure we're using a different databag, so outside changes can't affect
	// the transaction
	pristine = pristine.Copy()

	if err := applyDeltas(pristine, t.deltas); err != nil {
		return err
	}

	data, err := pristine.Data()
	if err != nil {
		return err
	}

	if err := t.registry.Schema.Validate(data); err != nil {
		return err
	}

	// copy the databag before writing to make sure the writer can't modify into
	// and introduce changes in the transaction
	if err := t.writeDatabag(pristine.Copy()); err != nil {
		return err
	}

	t.pristine = pristine
	t.modified = nil
	t.deltas = nil
	t.appliedDeltas = 0
	return nil
}

func applyDeltas(bag JSONDataBag, deltas []map[string]interface{}) error {
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
	return t.pristine.Data()
}
