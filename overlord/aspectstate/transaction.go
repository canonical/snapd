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

package aspectstate

import (
	"sync"

	"github.com/snapcore/snapd/aspects"
)

// Transaction performs read and writes to a databag in an atomic way.
type Transaction struct {
	// TODO: is the memory overhead of having two databags too large? An alternative
	// would be to keep only deltas and then apply them on Commit. However, we
	// wouldn't be able to reuse the JSONDataBag's get/set so it would be more code
	// intensive. o/configstate/config/transaction.go does this but it still generates
	// the full tree anyway to perform Get() operations. One idea to improve on this
	// would be to keep the deltas sorted so Get() can start looking for values in
	// the newer changes, so we wouldn't need to generate a full tree and keep it in-mem
	pristineDatabag aspects.JSONDataBag
	workingDatabag  aspects.JSONDataBag
	schema          aspects.Schema
	mu              sync.RWMutex
}

func NewTransaction(databag aspects.JSONDataBag, schema aspects.Schema) *Transaction {
	return &Transaction{
		pristineDatabag: databag,
		schema:          schema,
		workingDatabag:  databag.Copy(),
	}
}

func (t *Transaction) Set(path string, value interface{}) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if err := t.workingDatabag.Set(path, value); err != nil {
		return err
	}

	data, err := t.workingDatabag.Data()
	if err != nil {
		return err
	}

	return t.schema.Validate(data)
}

func (t *Transaction) Get(path string, value interface{}) error {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return t.workingDatabag.Get(path, value)
}

// Commit saves the writes performed until now and returns the new Databag.
func (t *Transaction) Commit() {
	t.pristineDatabag = t.workingDatabag.Copy()
}

// Data returns the committed data (Commit must be called first, for any changes
// to be reflected).
func (t *Transaction) Data() ([]byte, error) {
	return t.pristineDatabag.Data()
}
