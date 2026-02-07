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

package confdbstate_test

import (
	"encoding/json"
	"errors"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/confdb"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/confdbstate"
	"github.com/snapcore/snapd/overlord/state"
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

	restore := confdbstate.MockReadDatabag(func(st *state.State, account, confdbName string) (confdb.JSONDatabag, error) {
		s.readCalled++
		return confdbstate.ReadDatabag(st, account, confdbName)
	})
	s.AddCleanup(restore)

	restore = confdbstate.MockWriteDatabag(func(st *state.State, bag confdb.JSONDatabag, account, confdbName string) error {
		s.writeCalled++
		return confdbstate.WriteDatabag(st, bag, account, confdbName)
	})
	s.AddCleanup(restore)
}

var _ = Suite(&transactionTestSuite{})

func (s *transactionTestSuite) TestSet(c *C) {
	tx, err := confdbstate.NewTransaction(s.state, "my-account", "my-confdb")
	c.Assert(err, IsNil)
	c.Assert(s.readCalled, Equals, 1)

	err = tx.Set(parsePath(c, "foo"), "bar")
	c.Assert(err, IsNil)
	c.Assert(s.writeCalled, Equals, 0)

	bag, err := confdbstate.ReadDatabag(s.state, "my-account", "my-confdb")
	c.Assert(err, IsNil)

	_, err = bag.Get(parsePath(c, "foo"), nil)
	c.Assert(err, testutil.ErrorIs, &confdb.NoDataError{})
}

func (s *transactionTestSuite) TestCommit(c *C) {
	tx, err := confdbstate.NewTransaction(s.state, "my-account", "my-confdb")
	c.Assert(err, IsNil)
	c.Assert(s.readCalled, Equals, 1)

	err = tx.Set(parsePath(c, "foo"), "bar")
	c.Assert(err, IsNil)
	c.Assert(s.readCalled, Equals, 1)
	c.Assert(s.writeCalled, Equals, 0)

	err = tx.Commit(s.state, confdb.NewJSONSchema())
	c.Assert(err, IsNil)

	bag, err := confdbstate.ReadDatabag(s.state, "my-account", "my-confdb")
	c.Assert(err, IsNil)

	value, err := bag.Get(parsePath(c, "foo"), nil)
	c.Assert(err, IsNil)
	c.Assert(value, Equals, "bar")
	c.Assert(s.writeCalled, Equals, 1)
}

func (s *transactionTestSuite) TestGetReadsUncommitted(c *C) {
	tx, err := confdbstate.NewTransaction(s.state, "my-account", "my-confdb")
	c.Assert(err, IsNil)

	bag, err := confdbstate.ReadDatabag(s.state, "my-account", "my-confdb")
	c.Assert(err, IsNil)

	err = bag.Set(parsePath(c, "foo"), "bar")
	c.Assert(err, IsNil)

	err = tx.Set(parsePath(c, "foo"), "baz")
	c.Assert(err, IsNil)

	// nothing was committed
	c.Assert(s.writeCalled, Equals, 0)

	// but Data shows uncommitted data
	c.Assert(txData(c, tx), Equals, `{"foo":"baz"}`)

	val, err := tx.Get(parsePath(c, "foo"), nil)
	c.Assert(err, IsNil)
	c.Assert(val, Equals, "baz")
}

type failingSchema struct {
	err error
}

func (f *failingSchema) Validate([]byte) error {
	return f.err
}

func (f *failingSchema) SchemaAt(path []confdb.Accessor) ([]confdb.DatabagSchema, error) {
	return []confdb.DatabagSchema{f}, nil
}

func (f *failingSchema) Type() confdb.SchemaType                 { return confdb.Any }
func (f *failingSchema) Ephemeral() bool                         { return false }
func (f *failingSchema) NestedEphemeral() bool                   { return false }
func (f *failingSchema) Visibility() confdb.Visibility           { return confdb.DefaultVisibility }
func (f *failingSchema) NestedVisibility(confdb.Visibility) bool { return false }
func (f *failingSchema) PruneByVisibility(_ []confdb.Accessor, _ int, _ []confdb.Visibility, data []byte) ([]byte, error) {
	return data, nil
}

func (s *transactionTestSuite) TestRollBackOnCommitError(c *C) {
	tx, err := confdbstate.NewTransaction(s.state, "my-account", "my-confdb")
	c.Assert(err, IsNil)

	err = tx.Set(parsePath(c, "foo"), "bar")
	c.Assert(err, IsNil)

	err = tx.Commit(s.state, &failingSchema{err: errors.New("expected error")})
	c.Assert(err, ErrorMatches, "expected error")

	// nothing was committed
	c.Assert(s.writeCalled, Equals, 0)
	bag, err := confdbstate.ReadDatabag(s.state, "my-account", "my-confdb")
	c.Assert(err, IsNil)

	_, err = bag.Get(parsePath(c, "foo"), nil)
	c.Assert(err, testutil.ErrorIs, &confdb.NoDataError{})

	// but subsequent Gets still read the uncommitted values
	val, err := tx.Get(parsePath(c, "foo"), nil)
	c.Assert(err, IsNil)
	c.Assert(val, Equals, "bar")
}

func (s *transactionTestSuite) TestManyWrites(c *C) {
	tx, err := confdbstate.NewTransaction(s.state, "my-account", "my-confdb")
	c.Assert(err, IsNil)

	err = tx.Set(parsePath(c, "foo"), "bar")
	c.Assert(err, IsNil)
	err = tx.Set(parsePath(c, "foo"), "baz")
	c.Assert(err, IsNil)

	err = tx.Commit(s.state, confdb.NewJSONSchema())
	c.Assert(err, IsNil)
	c.Assert(s.writeCalled, Equals, 1)

	// writes are applied in chronological order
	c.Assert(txData(c, tx), Equals, `{"foo":"baz"}`)

	bag, err := confdbstate.ReadDatabag(s.state, "my-account", "my-confdb")
	c.Assert(err, IsNil)

	value, err := bag.Get(parsePath(c, "foo"), nil)
	c.Assert(err, IsNil)
	c.Assert(value, Equals, "baz")
}

func (s *transactionTestSuite) TestCommittedIncludesRecentWrites(c *C) {
	tx, err := confdbstate.NewTransaction(s.state, "my-account", "my-confdb")
	c.Assert(err, IsNil)
	c.Assert(s.readCalled, Equals, 1)

	err = tx.Set(parsePath(c, "foo"), "bar")
	c.Assert(err, IsNil)

	bag, err := confdbstate.ReadDatabag(s.state, "my-account", "my-confdb")
	c.Assert(err, IsNil)

	err = bag.Set(parsePath(c, "bar"), "baz")
	c.Assert(err, IsNil)
	err = confdbstate.WriteDatabag(s.state, bag, "my-account", "my-confdb")
	c.Assert(err, IsNil)

	err = tx.Commit(s.state, confdb.NewJSONSchema())
	c.Assert(err, IsNil)
	// databag was read from state before writing
	c.Assert(s.readCalled, Equals, 2)
	c.Assert(s.writeCalled, Equals, 1)

	// writes are applied in chronological order
	bag, err = confdbstate.ReadDatabag(s.state, "my-account", "my-confdb")
	c.Assert(err, IsNil)

	value, err := bag.Get(parsePath(c, "foo"), nil)
	c.Assert(err, IsNil)
	c.Assert(value, Equals, "bar")

	// contains recent values not written by the transaction
	value, err = bag.Get(parsePath(c, "bar"), nil)
	c.Assert(err, IsNil)
	c.Assert(value, Equals, "baz")
}

func (s *transactionTestSuite) TestCommittedIncludesPreviousCommit(c *C) {
	txOne, err := confdbstate.NewTransaction(s.state, "my-account", "my-confdb")
	c.Assert(err, IsNil)

	txTwo, err := confdbstate.NewTransaction(s.state, "my-account", "my-confdb")
	c.Assert(err, IsNil)

	err = txOne.Set(parsePath(c, "foo"), "bar")
	c.Assert(err, IsNil)

	err = txTwo.Set(parsePath(c, "bar"), "baz")
	c.Assert(err, IsNil)

	err = txOne.Commit(s.state, confdb.NewJSONSchema())
	c.Assert(err, IsNil)

	bag, err := confdbstate.ReadDatabag(s.state, "my-account", "my-confdb")
	c.Assert(err, IsNil)

	value, err := bag.Get(parsePath(c, "foo"), nil)
	c.Assert(err, IsNil)
	c.Assert(value, Equals, "bar")

	value, err = bag.Get(parsePath(c, "bar"), nil)
	c.Assert(err, testutil.ErrorIs, &confdb.NoDataError{})
	c.Assert(value, IsNil)

	err = txTwo.Commit(s.state, confdb.NewJSONSchema())
	c.Assert(err, IsNil)

	bag, err = confdbstate.ReadDatabag(s.state, "my-account", "my-confdb")
	c.Assert(err, IsNil)

	value, err = bag.Get(parsePath(c, "foo"), nil)
	c.Assert(err, IsNil)
	c.Assert(value, Equals, "bar")

	value, err = bag.Get(parsePath(c, "bar"), nil)
	c.Assert(err, IsNil)
	c.Assert(value, Equals, "baz")
}

func (s *transactionTestSuite) TestTransactionBagReadError(c *C) {
	var readErr error
	restore := confdbstate.MockReadDatabag(func(st *state.State, account, confdbName string) (confdb.JSONDatabag, error) {
		return nil, readErr
	})
	defer restore()

	txOne, err := confdbstate.NewTransaction(s.state, "my-account", "my-confdb")
	c.Assert(err, IsNil)

	readErr = errors.New("expected")
	// Commit()'s databag read fails
	err = txOne.Commit(s.state, confdb.NewJSONSchema())
	c.Assert(err, ErrorMatches, "expected")

	// NewTransaction()'s databag read fails
	txOne, err = confdbstate.NewTransaction(s.state, "my-account", "my-confdb")
	c.Assert(err, ErrorMatches, "expected")
}

func (s *transactionTestSuite) TestTransactionBagWriteError(c *C) {
	writeErr := errors.New("expected")
	restore := confdbstate.MockWriteDatabag(func(st *state.State, bag confdb.JSONDatabag, account, confdbName string) error {
		return writeErr
	})
	defer restore()

	txOne, err := confdbstate.NewTransaction(s.state, "my-account", "my-confdb")
	c.Assert(err, IsNil)

	// Commit()'s databag write fails
	err = txOne.Commit(s.state, confdb.NewJSONSchema())
	c.Assert(err, ErrorMatches, "expected")
}

func (s *transactionTestSuite) TestTransactionReadsIsolated(c *C) {
	tx, err := confdbstate.NewTransaction(s.state, "my-account", "my-confdb")
	c.Assert(err, IsNil)

	bag, err := confdbstate.ReadDatabag(s.state, "my-account", "my-confdb")
	c.Assert(err, IsNil)

	err = bag.Set(parsePath(c, "foo"), "bar")
	c.Assert(err, IsNil)

	_, err = tx.Get(parsePath(c, "foo"), nil)
	c.Assert(err, testutil.ErrorIs, &confdb.NoDataError{})
}

func (s *transactionTestSuite) TestUnset(c *C) {
	tx, err := confdbstate.NewTransaction(s.state, "my-account", "my-confdb")
	c.Assert(err, IsNil)

	err = tx.Set(parsePath(c, "foo"), "bar")
	c.Assert(err, IsNil)

	err = tx.Commit(s.state, confdb.NewJSONSchema())
	c.Assert(err, IsNil)

	bag, err := confdbstate.ReadDatabag(s.state, "my-account", "my-confdb")
	c.Assert(err, IsNil)

	val, err := bag.Get(parsePath(c, "foo"), nil)
	c.Assert(err, IsNil)
	c.Assert(val, Equals, "bar")

	err = tx.Unset(parsePath(c, "foo"))
	c.Assert(err, IsNil)

	err = tx.Commit(s.state, confdb.NewJSONSchema())
	c.Assert(err, IsNil)

	bag, err = confdbstate.ReadDatabag(s.state, "my-account", "my-confdb")
	c.Assert(err, IsNil)
	_, err = bag.Get(parsePath(c, "foo"), nil)
	c.Assert(err, testutil.ErrorIs, &confdb.NoDataError{})
}

func (s *transactionTestSuite) TestSerializable(c *C) {
	bag := confdb.NewJSONDatabag()
	err := bag.Set(parsePath(c, "other"), "value")
	c.Assert(err, IsNil)

	err = confdbstate.WriteDatabag(s.state, bag, "my-account", "my-confdb")
	c.Assert(err, IsNil)

	tx, err := confdbstate.NewTransaction(s.state, "my-account", "my-confdb")
	c.Assert(err, IsNil)

	err = tx.Set(parsePath(c, "foo"), "bar")
	c.Assert(err, IsNil)

	jsonData, err := json.Marshal(tx)
	c.Assert(err, IsNil)

	tx = nil
	err = json.Unmarshal(jsonData, &tx)
	c.Assert(err, IsNil)

	// transaction deltas are preserved
	val, err := tx.Get(parsePath(c, "foo"), nil)
	c.Assert(err, IsNil)
	c.Assert(val, Equals, "bar")

	// we can commit as normal
	err = tx.Commit(s.state, confdb.NewJSONSchema())
	c.Assert(err, IsNil)

	bag, err = confdbstate.ReadDatabag(s.state, "my-account", "my-confdb")
	c.Assert(err, IsNil)

	value, err := bag.Get(parsePath(c, "foo"), nil)
	c.Assert(err, IsNil)
	c.Assert(value, Equals, "bar")

	value, err = bag.Get(parsePath(c, "other"), nil)
	c.Assert(err, IsNil)
	c.Assert(value, Equals, "value")
}

func txData(c *C, tx *confdbstate.Transaction) string {
	data, err := tx.Data()
	c.Assert(err, IsNil)
	return string(data)
}

func (s *transactionTestSuite) TestAbortPreventsReadsAndWrites(c *C) {
	tx, err := confdbstate.NewTransaction(s.state, "my-account", "my-confdb")
	c.Assert(err, IsNil)

	err = tx.Set(parsePath(c, "foo"), "bar")
	c.Assert(err, IsNil)

	val, err := tx.Get(parsePath(c, "foo"), nil)
	c.Assert(err, IsNil)
	c.Check(val, Equals, "bar")

	tx.Abort("my-snap", "don't like the changes")

	snap, reason := tx.AbortInfo()
	c.Assert(reason, Equals, "don't like the changes")
	c.Assert(snap, Equals, "my-snap")

	err = tx.Set(parsePath(c, "foo"), "bar")
	c.Assert(err, ErrorMatches, "cannot write to aborted transaction")

	err = tx.Clear(s.state)
	c.Assert(err, ErrorMatches, "cannot write to aborted transaction")

	err = tx.Unset(parsePath(c, "foo"))
	c.Assert(err, ErrorMatches, "cannot write to aborted transaction")

	_, err = tx.Get(parsePath(c, "foo"), nil)
	c.Assert(err, ErrorMatches, "cannot read from aborted transaction")

	_, err = tx.Data()
	c.Assert(err, ErrorMatches, "cannot read from aborted transaction")

	err = tx.Commit(s.state, confdb.NewJSONSchema())
	c.Assert(err, ErrorMatches, "cannot commit aborted transaction")
}

func (s *transactionTestSuite) TestTransactionPrevious(c *C) {
	bag := confdb.NewJSONDatabag()
	err := bag.Set(parsePath(c, "foo"), "bar")
	c.Assert(err, IsNil)

	err = confdbstate.WriteDatabag(s.state, bag, "my-account", "my-confdb")
	c.Assert(err, IsNil)

	tx, err := confdbstate.NewTransaction(s.state, "my-account", "my-confdb")
	c.Assert(err, IsNil)

	err = tx.Set(parsePath(c, "foo"), "baz")
	c.Assert(err, IsNil)

	checkPrevious := func() {
		previousBag := tx.Previous()
		val, err := previousBag.Get(parsePath(c, "foo"), nil)
		c.Assert(err, IsNil)
		c.Check(val, Equals, "bar")
	}
	checkPrevious()

	err = tx.Commit(s.state, confdb.NewJSONSchema())
	c.Assert(err, IsNil)

	checkPrevious()
}
