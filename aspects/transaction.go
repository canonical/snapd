// -*- Mode: Go; indent-tabs-mode: t -*-
/*
 * Copyright (C) 2023 Canonical Ltd
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

package aspects

import (
	"sync"
)

type DatabagRead func() (JSONDataBag, error)
type DatabagWrite func(JSONDataBag) error

// Transaction performs read and writes to a databag in an atomic way.
type Transaction struct {
	pristine JSONDataBag
	deltas   []map[string]interface{}
	schema   Schema

	readDatabag  DatabagRead
	writeDatabag DatabagWrite
	mu           sync.RWMutex
}

// NewTransaction takes a getter and setter to read and write the databag.
func NewTransaction(readDatabag DatabagRead, writeDatabag DatabagWrite, schema Schema) (*Transaction, error) {
	databag, err := readDatabag()
	if err != nil {
		return nil, err
	}

	return &Transaction{
		pristine:     databag.Copy(),
		schema:       schema,
		readDatabag:  readDatabag,
		writeDatabag: writeDatabag,
	}, nil
}

// Set sets a value in the transaction's databag. The change isn't persisted
// until Commit returns without errors.
func (t *Transaction) Set(path string, value interface{}) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.deltas = append(t.deltas, map[string]interface{}{path: value})
	return nil
}

// Get reads a value from the transaction's databag including uncommitted changes.
func (t *Transaction) Get(path string, value interface{}) error {
	t.mu.RLock()
	defer t.mu.RUnlock()

	// if there are changes, create a copy before applying (for isolation)
	bag := t.pristine
	if len(t.deltas) != 0 {
		bag = t.pristine.Copy()

		if err := applyDeltas(bag, t.deltas); err != nil {
			return err
		}
	}

	return bag.Get(path, value)
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

	if err := t.schema.Validate(data); err != nil {
		return err
	}

	// copy the databag before writing to make sure the writer can't modify into
	// and introduce changes in the transaction
	if err := t.writeDatabag(pristine.Copy()); err != nil {
		return err
	}

	t.pristine = pristine
	t.deltas = nil
	return nil
}

func applyDeltas(bag JSONDataBag, deltas []map[string]interface{}) error {
	// changes must be applied in the order they were written
	for _, delta := range deltas {
		for k, v := range delta {
			if err := bag.Set(k, v); err != nil {
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
