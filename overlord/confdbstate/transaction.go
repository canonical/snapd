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

	modified   confdb.JSONDatabag
	operations []operation
	appliedOps int

	abortingSnap string
	abortReason  string

	mu sync.RWMutex
}

// operation is a change that can be applied to a databag. A transaction
// records a sequence of operations which are replayed in order when the
// changes are applied or committed.
type operation interface {
	// apply performs the change on the given databag.
	apply(bag confdb.Databag) error
}

// setOperation sets a value at a path of the databag.
type setOperation struct {
	path  []confdb.Accessor
	value any
}

func (op *setOperation) apply(bag confdb.Databag) error {
	return bag.Set(op.path, op.value)
}

func (op *setOperation) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Operation string `json:"operation"`
		Path      string `json:"path"`
		Value     any    `json:"value"`
	}{
		Operation: "set",
		Path:      confdb.JoinAccessors(op.path),
		Value:     op.value,
	})
}

func (op *setOperation) UnmarshalJSON(data []byte) error {
	var wire struct {
		Path  string `json:"path"`
		Value any    `json:"value"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}

	accs, err := parseOperationPath(wire.Path)
	if err != nil {
		return err
	}
	op.path = accs
	op.value = wire.Value
	return nil
}

// unsetOperation removes the value at a path of the databag.
type unsetOperation struct {
	path []confdb.Accessor
}

func (op *unsetOperation) apply(bag confdb.Databag) error {
	// TODO: add support for constraints in transactions.
	return bag.Unset(op.path, nil)
}

func (op *unsetOperation) MarshalJSON() ([]byte, error) {
	// TODO: marshal constraints once transactions support constrained unsets.
	return json.Marshal(struct {
		Operation string `json:"operation"`
		Path      string `json:"path"`
	}{
		Operation: "unset",
		Path:      confdb.JoinAccessors(op.path),
	})
}

func (op *unsetOperation) UnmarshalJSON(data []byte) error {
	var wire struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}

	accs, err := parseOperationPath(wire.Path)
	if err != nil {
		return err
	}
	op.path = accs
	return nil
}

// pruneEmptyOperation removes every container that is left without entries
// from the databag.
type pruneEmptyOperation struct{}

func (op *pruneEmptyOperation) apply(bag confdb.Databag) error {
	return bag.PruneEmpty()
}

func (op *pruneEmptyOperation) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Operation string `json:"operation"`
	}{
		Operation: "prune-empty",
	})
}

func (op *pruneEmptyOperation) UnmarshalJSON(data []byte) error {
	// this operations doesn't have any parameters
	return nil
}

func parseOperationPath(path string) ([]confdb.Accessor, error) {
	opts := confdb.ParseOptions{AllowPlaceholders: true}
	accs, err := confdb.ParsePathIntoAccessors(path, opts)
	if err != nil {
		return nil, fmt.Errorf("internal error: cannot parse path %q: %v", path, err)
	}
	return accs, nil
}

// unmarshalOperation decodes a marshalled operation into its concrete type.
func unmarshalOperation(data []byte) (operation, error) {
	var header struct {
		Operation string `json:"operation"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return nil, err
	}

	var op operation
	switch header.Operation {
	case "set":
		op = &setOperation{}
	case "unset":
		op = &unsetOperation{}
	case "prune-empty":
		op = &pruneEmptyOperation{}
	default:
		return nil, fmt.Errorf("internal error: unknown transaction operation %q", header.Operation)
	}

	if err := json.Unmarshal(data, op); err != nil {
		return nil, err
	}
	return op, nil
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

	Modified          confdb.JSONDatabag `json:"modified,omitempty"`
	Operations        []json.RawMessage  `json:"operations,omitempty"`
	AppliedOperations int                `json:"applied-operations,omitempty"`

	AbortingSnap string `json:"aborting-snap,omitempty"`
	AbortReason  string `json:"abort-reason,omitempty"`
}

func (t *Transaction) MarshalJSON() ([]byte, error) {
	operations := make([]json.RawMessage, 0, len(t.operations))
	for _, op := range t.operations {
		data, err := json.Marshal(op)
		if err != nil {
			return nil, err
		}
		operations = append(operations, data)
	}

	return json.Marshal(marshalledTransaction{
		Pristine:          t.pristine,
		Previous:          t.previous,
		ConfdbAccount:     t.ConfdbAccount,
		ConfdbName:        t.ConfdbName,
		Modified:          t.modified,
		Operations:        operations,
		AppliedOperations: t.appliedOps,
		AbortingSnap:      t.abortingSnap,
		AbortReason:       t.abortReason,
	})
}

func (t *Transaction) UnmarshalJSON(data []byte) error {
	var mt marshalledTransaction
	if err := json.Unmarshal(data, &mt); err != nil {
		return err
	}

	operations := make([]operation, 0, len(mt.Operations))
	for _, data := range mt.Operations {
		op, err := unmarshalOperation(data)
		if err != nil {
			return err
		}
		operations = append(operations, op)
	}

	t.pristine = mt.Pristine
	t.previous = mt.Previous
	t.ConfdbAccount = mt.ConfdbAccount
	t.ConfdbName = mt.ConfdbName
	t.modified = mt.Modified
	t.operations = operations
	t.appliedOps = mt.AppliedOperations
	t.abortingSnap = mt.AbortingSnap
	t.abortReason = mt.AbortReason

	return nil
}

// Set sets a value in the transaction's databag. The change isn't persisted
// until Commit returns without errors. A nil value unsets the value at the
// path.
func (t *Transaction) Set(path []confdb.Accessor, value any) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.aborted() {
		return errors.New("cannot write to aborted transaction")
	}

	if value == nil {
		t.operations = append(t.operations, &unsetOperation{path: path})
	} else {
		t.operations = append(t.operations, &setOperation{path: path, value: value})
	}

	return nil
}

// Unset unsets a value in the transaction's databag. The change isn't persisted
// until Commit returns without errors.
func (t *Transaction) Unset(path []confdb.Accessor, _ map[string]any) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.aborted() {
		return errors.New("cannot write to aborted transaction")
	}

	t.operations = append(t.operations, &unsetOperation{path: path})
	return nil
}

// Get reads a value from the transaction's databag including uncommitted changes.
func (t *Transaction) Get(path []confdb.Accessor, cstrs map[string]any) (any, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.aborted() {
		return nil, errors.New("cannot read from aborted transaction")
	}

	// if there aren't any changes, just use the pristine bag
	if len(t.operations) == 0 {
		return t.pristine.Get(path, cstrs)
	}

	if err := t.applyChanges(); err != nil {
		return nil, err
	}

	return t.modified.Get(path, cstrs)
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

	if err := applyOperations(pristine, t.operations); err != nil {
		return err
	}

	// remove the containers left empty by the changes
	if err := pristine.PruneEmpty(); err != nil {
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
	t.operations = nil
	t.appliedOps = 0
	return nil
}

func (t *Transaction) Reset(st *state.State) error {
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
	t.operations = nil
	t.appliedOps = 0
	return nil
}

func (t *Transaction) AlteredPaths() [][]confdb.Accessor {
	// TODO: maybe we can extend this to recurse into delta's value and figure out
	// the most specific key possible
	paths := make([][]confdb.Accessor, 0, len(t.operations))
	for _, op := range t.operations {
		// pruning doesn't alter a specific path
		if _, ok := op.(*pruneEmptyOperation); ok {
			continue
		}

		switch op := op.(type) {
		case *setOperation:
			paths = append(paths, op.path)
		case *unsetOperation:
			paths = append(paths, op.path)
		}
	}
	return paths
}

func (t *Transaction) applyChanges() error {
	// use a cached bag to apply and keep the changes
	if t.modified == nil {
		t.modified = t.pristine.Copy()
		t.appliedOps = 0
	}

	// apply new changes since the last Get/Data call
	if err := applyOperations(t.modified, t.operations[t.appliedOps:]); err != nil {
		t.modified = nil
		t.appliedOps = 0
		return err
	}

	t.appliedOps = len(t.operations)
	return nil
}

func applyOperations(bag confdb.Databag, operations []operation) error {
	// operations must be applied in the order they were written
	for _, op := range operations {
		if err := op.apply(bag); err != nil {
			return err
		}
	}

	return nil
}

// PruneEmpty records an operation that removes every container left without
// entries by the preceding operations.
func (t *Transaction) PruneEmpty() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.aborted() {
		return errors.New("cannot write to aborted transaction")
	}

	t.operations = append(t.operations, &pruneEmptyOperation{})
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
