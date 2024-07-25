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

package registrystate_test

import (
	"encoding/json"
	"errors"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/registrystate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/registry"
	"github.com/snapcore/snapd/testutil"
)

type transactionTestSuite struct {
	testutil.BaseTest

	state *state.State

	readCalled  int
	writeCalled int
}

func (s *transactionTestSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())

	s.state = state.New(nil)
	s.state.Lock()
	s.AddCleanup(func() { s.state.Unlock() })

	s.readCalled = 0
	s.writeCalled = 0

	restore := registrystate.MockReadDatabag(func(st *state.State, account, registryName string) (registry.JSONDataBag, error) {
		s.readCalled++
		return registrystate.ReadDatabag(st, account, registryName)
	})
	s.AddCleanup(restore)

	restore = registrystate.MockWriteDatabag(func(st *state.State, bag registry.JSONDataBag, account, registryName string) error {
		s.writeCalled++
		return registrystate.WriteDatabag(st, bag, account, registryName)
	})
	s.AddCleanup(restore)
}

var _ = Suite(&transactionTestSuite{})

func (s *transactionTestSuite) TestSet(c *C) {
	tx, err := registrystate.NewTransaction(s.state, false, "my-account", "my-reg")
	c.Assert(err, IsNil)
	c.Assert(s.readCalled, Equals, 1)

	err = tx.Set("foo", "bar")
	c.Assert(err, IsNil)
	c.Assert(s.writeCalled, Equals, 0)

	bag, err := registrystate.ReadDatabag(s.state, "my-account", "my-reg")
	c.Assert(err, IsNil)

	_, err = bag.Get("foo")
	c.Assert(err, FitsTypeOf, registry.PathError(""))
}

func (s *transactionTestSuite) TestCommit(c *C) {
	tx, err := registrystate.NewTransaction(s.state, false, "my-account", "my-reg")
	c.Assert(err, IsNil)
	c.Assert(s.readCalled, Equals, 1)

	err = tx.Set("foo", "bar")
	c.Assert(err, IsNil)
	c.Assert(s.readCalled, Equals, 1)
	c.Assert(s.writeCalled, Equals, 0)

	err = tx.Commit(s.state, registry.NewJSONSchema())
	c.Assert(err, IsNil)

	bag, err := registrystate.ReadDatabag(s.state, "my-account", "my-reg")
	c.Assert(err, IsNil)

	value, err := bag.Get("foo")
	c.Assert(err, IsNil)
	c.Assert(value, Equals, "bar")
	c.Assert(s.writeCalled, Equals, 1)
}

func (s *transactionTestSuite) TestGetReadsUncommitted(c *C) {
	tx, err := registrystate.NewTransaction(s.state, false, "my-account", "my-reg")
	c.Assert(err, IsNil)

	bag, err := registrystate.ReadDatabag(s.state, "my-account", "my-reg")
	c.Assert(err, IsNil)

	err = bag.Set("foo", "bar")
	c.Assert(err, IsNil)

	err = tx.Set("foo", "baz")
	c.Assert(err, IsNil)

	// nothing was committed
	c.Assert(s.writeCalled, Equals, 0)

	// but Data shows uncommitted data
	c.Assert(txData(c, tx), Equals, `{"foo":"baz"}`)

	val, err := tx.Get("foo")
	c.Assert(err, IsNil)
	c.Assert(val, Equals, "baz")
}

type failingSchema struct {
	err error
}

func (f *failingSchema) Validate([]byte) error {
	return f.err
}

func (f *failingSchema) SchemaAt(path []string) ([]registry.Schema, error) {
	return []registry.Schema{f}, nil
}

func (f *failingSchema) Type() registry.SchemaType {
	return registry.Any
}

func (s *transactionTestSuite) TestRollBackOnCommitError(c *C) {
	tx, err := registrystate.NewTransaction(s.state, false, "my-account", "my-reg")
	c.Assert(err, IsNil)

	err = tx.Set("foo", "bar")
	c.Assert(err, IsNil)

	err = tx.Commit(s.state, &failingSchema{err: errors.New("expected error")})
	c.Assert(err, ErrorMatches, "expected error")

	// nothing was committed
	c.Assert(s.writeCalled, Equals, 0)
	bag, err := registrystate.ReadDatabag(s.state, "my-account", "my-reg")
	c.Assert(err, IsNil)

	_, err = bag.Get("foo")
	c.Assert(err, FitsTypeOf, registry.PathError(""))

	// but subsequent Gets still read the uncommitted values
	val, err := tx.Get("foo")
	c.Assert(err, IsNil)
	c.Assert(val, Equals, "bar")
}

func (s *transactionTestSuite) TestManyWrites(c *C) {
	tx, err := registrystate.NewTransaction(s.state, false, "my-account", "my-reg")
	c.Assert(err, IsNil)

	err = tx.Set("foo", "bar")
	c.Assert(err, IsNil)
	err = tx.Set("foo", "baz")
	c.Assert(err, IsNil)

	err = tx.Commit(s.state, registry.NewJSONSchema())
	c.Assert(err, IsNil)
	c.Assert(s.writeCalled, Equals, 1)

	// writes are applied in chronological order
	c.Assert(txData(c, tx), Equals, `{"foo":"baz"}`)

	bag, err := registrystate.ReadDatabag(s.state, "my-account", "my-reg")
	c.Assert(err, IsNil)

	value, err := bag.Get("foo")
	c.Assert(err, IsNil)
	c.Assert(value, Equals, "baz")
}

func (s *transactionTestSuite) TestCommittedIncludesRecentWrites(c *C) {
	tx, err := registrystate.NewTransaction(s.state, false, "my-account", "my-reg")
	c.Assert(err, IsNil)
	c.Assert(s.readCalled, Equals, 1)

	err = tx.Set("foo", "bar")
	c.Assert(err, IsNil)

	bag, err := registrystate.ReadDatabag(s.state, "my-account", "my-reg")
	c.Assert(err, IsNil)

	err = bag.Set("bar", "baz")
	c.Assert(err, IsNil)
	err = registrystate.WriteDatabag(s.state, bag, "my-account", "my-reg")
	c.Assert(err, IsNil)

	err = tx.Commit(s.state, registry.NewJSONSchema())
	c.Assert(err, IsNil)
	// databag was read from state before writing
	c.Assert(s.readCalled, Equals, 2)
	c.Assert(s.writeCalled, Equals, 1)

	// writes are applied in chronological order
	bag, err = registrystate.ReadDatabag(s.state, "my-account", "my-reg")
	c.Assert(err, IsNil)

	value, err := bag.Get("foo")
	c.Assert(err, IsNil)
	c.Assert(value, Equals, "bar")

	// contains recent values not written by the transaction
	value, err = bag.Get("bar")
	c.Assert(err, IsNil)
	c.Assert(value, Equals, "baz")
}

func (s *transactionTestSuite) TestCommittedIncludesPreviousCommit(c *C) {
	txOne, err := registrystate.NewTransaction(s.state, false, "my-account", "my-reg")
	c.Assert(err, IsNil)

	txTwo, err := registrystate.NewTransaction(s.state, false, "my-account", "my-reg")
	c.Assert(err, IsNil)

	err = txOne.Set("foo", "bar")
	c.Assert(err, IsNil)

	err = txTwo.Set("bar", "baz")
	c.Assert(err, IsNil)

	err = txOne.Commit(s.state, registry.NewJSONSchema())
	c.Assert(err, IsNil)

	bag, err := registrystate.ReadDatabag(s.state, "my-account", "my-reg")
	c.Assert(err, IsNil)

	value, err := bag.Get("foo")
	c.Assert(err, IsNil)
	c.Assert(value, Equals, "bar")

	value, err = bag.Get("bar")
	c.Assert(err, FitsTypeOf, registry.PathError(""))
	c.Assert(value, IsNil)

	err = txTwo.Commit(s.state, registry.NewJSONSchema())
	c.Assert(err, IsNil)

	bag, err = registrystate.ReadDatabag(s.state, "my-account", "my-reg")
	c.Assert(err, IsNil)

	value, err = bag.Get("foo")
	c.Assert(err, IsNil)
	c.Assert(value, Equals, "bar")

	value, err = bag.Get("bar")
	c.Assert(err, IsNil)
	c.Assert(value, Equals, "baz")
}

func (s *transactionTestSuite) TestTransactionBagReadError(c *C) {
	var readErr error
	restore := registrystate.MockReadDatabag(func(st *state.State, account, registryName string) (registry.JSONDataBag, error) {
		return nil, readErr
	})
	defer restore()

	txOne, err := registrystate.NewTransaction(s.state, false, "my-account", "my-reg")
	c.Assert(err, IsNil)

	readErr = errors.New("expected")
	// Commit()'s databag read fails
	err = txOne.Commit(s.state, registry.NewJSONSchema())
	c.Assert(err, ErrorMatches, "expected")

	// NewTransaction()'s databag read fails
	txOne, err = registrystate.NewTransaction(s.state, false, "my-account", "my-reg")
	c.Assert(err, ErrorMatches, "expected")
}

func (s *transactionTestSuite) TestTransactionBagWriteError(c *C) {
	writeErr := errors.New("expected")
	restore := registrystate.MockWriteDatabag(func(st *state.State, bag registry.JSONDataBag, account, registryName string) error {
		return writeErr
	})
	defer restore()

	txOne, err := registrystate.NewTransaction(s.state, false, "my-account", "my-reg")
	c.Assert(err, IsNil)

	// Commit()'s databag write fails
	err = txOne.Commit(s.state, registry.NewJSONSchema())
	c.Assert(err, ErrorMatches, "expected")
}

func (s *transactionTestSuite) TestTransactionReadsIsolated(c *C) {
	tx, err := registrystate.NewTransaction(s.state, false, "my-account", "my-reg")
	c.Assert(err, IsNil)

	bag, err := registrystate.ReadDatabag(s.state, "my-account", "my-reg")
	c.Assert(err, IsNil)

	err = bag.Set("foo", "bar")
	c.Assert(err, IsNil)

	_, err = tx.Get("foo")
	c.Assert(err, FitsTypeOf, registry.PathError(""))
}

func (s *transactionTestSuite) TestUnset(c *C) {
	tx, err := registrystate.NewTransaction(s.state, false, "my-account", "my-reg")
	c.Assert(err, IsNil)

	err = tx.Set("foo", "bar")
	c.Assert(err, IsNil)

	err = tx.Commit(s.state, registry.NewJSONSchema())
	c.Assert(err, IsNil)

	bag, err := registrystate.ReadDatabag(s.state, "my-account", "my-reg")
	c.Assert(err, IsNil)

	val, err := bag.Get("foo")
	c.Assert(err, IsNil)
	c.Assert(val, Equals, "bar")

	err = tx.Unset("foo")
	c.Assert(err, IsNil)

	err = tx.Commit(s.state, registry.NewJSONSchema())
	c.Assert(err, IsNil)

	bag, err = registrystate.ReadDatabag(s.state, "my-account", "my-reg")
	c.Assert(err, IsNil)
	_, err = bag.Get("foo")
	c.Assert(err, FitsTypeOf, registry.PathError(""))
}

func (s *transactionTestSuite) TestSerializable(c *C) {
	bag := registry.NewJSONDataBag()
	err := bag.Set("other", "value")
	c.Assert(err, IsNil)

	err = registrystate.WriteDatabag(s.state, bag, "my-account", "my-reg")
	c.Assert(err, IsNil)

	tx, err := registrystate.NewTransaction(s.state, false, "my-account", "my-reg")
	c.Assert(err, IsNil)

	err = tx.Set("foo", "bar")
	c.Assert(err, IsNil)

	jsonData, err := json.Marshal(tx)
	c.Assert(err, IsNil)

	tx = nil
	err = json.Unmarshal(jsonData, &tx)
	c.Assert(err, IsNil)

	// transaction deltas are preserved
	val, err := tx.Get("foo")
	c.Assert(err, IsNil)
	c.Assert(val, Equals, "bar")

	// we can commit as normal
	err = tx.Commit(s.state, registry.NewJSONSchema())
	c.Assert(err, IsNil)

	bag, err = registrystate.ReadDatabag(s.state, "my-account", "my-reg")
	c.Assert(err, IsNil)

	value, err := bag.Get("foo")
	c.Assert(err, IsNil)
	c.Assert(value, Equals, "bar")

	value, err = bag.Get("other")
	c.Assert(err, IsNil)
	c.Assert(value, Equals, "value")
}

func txData(c *C, tx *registrystate.Transaction) string {
	data, err := tx.Data()
	c.Assert(err, IsNil)
	return string(data)
}
