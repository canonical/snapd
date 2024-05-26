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

	"github.com/ddkwork/golibrary/mylog"
)

type (
	DatabagRead  func() (JSONDataBag, error)
	DatabagWrite func(JSONDataBag) error
)

// Transaction performs read and writes to a databag in an atomic way.
type Transaction struct {
	pristine JSONDataBag
	schema   Schema

	modified      JSONDataBag
	deltas        []map[string]interface{}
	appliedDeltas int

	readDatabag  DatabagRead
	writeDatabag DatabagWrite
	mu           sync.RWMutex
}

// NewTransaction takes a getter and setter to read and write the databag.
func NewTransaction(readDatabag DatabagRead, writeDatabag DatabagWrite, schema Schema) (*Transaction, error) {
	databag := mylog.Check2(readDatabag())

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
	mylog.Check(

		// apply new changes since the last get
		applyDeltas(t.modified, t.deltas[t.appliedDeltas:]))

	t.appliedDeltas = len(t.deltas)

	return t.modified.Get(path)
}

// Commit applies the previous writes and validates the final databag. If any
// error occurs, the original databag is kept.
func (t *Transaction) Commit() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	pristine := mylog.Check2(t.readDatabag())

	// ensure we're using a different databag, so outside changes can't affect
	// the transaction
	pristine = pristine.Copy()
	mylog.Check(applyDeltas(pristine, t.deltas))

	data := mylog.Check2(pristine.Data())
	mylog.Check(t.schema.Validate(data))
	mylog.Check(

		// copy the databag before writing to make sure the writer can't modify into
		// and introduce changes in the transaction
		t.writeDatabag(pristine.Copy()))

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
			if v == nil {
				mylog.Check(bag.Unset(k))
			} else {
				mylog.Check(bag.Set(k, v))
			}
		}
	}

	return nil
}

// Data returns the transaction's committed data.
func (t *Transaction) Data() ([]byte, error) {
	return t.pristine.Data()
}
