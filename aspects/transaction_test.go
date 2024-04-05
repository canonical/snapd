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

package aspects_test

import (
	"errors"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/aspects"
)

type transactionTestSuite struct{}

var _ = Suite(&transactionTestSuite{})

type witnessReadWriter struct {
	readCalled     int
	writeCalled    int
	bag            aspects.JSONDataBag
	writtenDatabag aspects.JSONDataBag
}

func (w *witnessReadWriter) read() (aspects.JSONDataBag, error) {
	w.readCalled++
	return w.bag, nil
}

func (w *witnessReadWriter) write(bag aspects.JSONDataBag) error {
	w.writeCalled++
	w.writtenDatabag = bag
	return nil
}

func (s *transactionTestSuite) TestSet(c *C) {
	bag := aspects.NewJSONDataBag()
	witness := &witnessReadWriter{bag: bag}
	schema := aspects.NewJSONSchema()
	tx, err := aspects.NewTransaction(witness.read, witness.write, schema)
	c.Assert(err, IsNil)
	c.Assert(witness.readCalled, Equals, 1)

	err = tx.Set("foo", "bar")
	c.Assert(err, IsNil)
	c.Assert(witness.writeCalled, Equals, 0)

	_, err = witness.writtenDatabag.Get("foo")
	c.Assert(err, FitsTypeOf, aspects.PathError(""))
}

func (s *transactionTestSuite) TestCommit(c *C) {
	witness := &witnessReadWriter{bag: aspects.NewJSONDataBag()}
	schema := aspects.NewJSONSchema()
	tx, err := aspects.NewTransaction(witness.read, witness.write, schema)
	c.Assert(err, IsNil)
	c.Assert(witness.readCalled, Equals, 1)

	err = tx.Set("foo", "bar")
	c.Assert(err, IsNil)
	c.Assert(witness.readCalled, Equals, 1)
	c.Assert(witness.writeCalled, Equals, 0)
	c.Assert(witness.writtenDatabag, IsNil)

	err = tx.Commit()
	c.Assert(err, IsNil)

	value, err := witness.writtenDatabag.Get("foo")
	c.Assert(err, IsNil)
	c.Assert(value, Equals, "bar")
	c.Assert(witness.writeCalled, Equals, 1)
}

func (s *transactionTestSuite) TestGetReadsUncommitted(c *C) {
	databag := aspects.NewJSONDataBag()
	witness := &witnessReadWriter{bag: databag}
	schema := aspects.NewJSONSchema()
	tx, err := aspects.NewTransaction(witness.read, witness.write, schema)
	c.Assert(err, IsNil)

	err = databag.Set("foo", "bar")
	c.Assert(err, IsNil)

	err = tx.Set("foo", "baz")
	c.Assert(err, IsNil)
	// nothing was committed
	c.Assert(witness.writeCalled, Equals, 0)
	c.Assert(txData(c, tx), Equals, "{}")

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

func (f *failingSchema) SchemaAt(path []string) ([]aspects.Schema, error) {
	return []aspects.Schema{f}, nil
}

func (f *failingSchema) Type() aspects.SchemaType {
	return aspects.Any
}

func (s *transactionTestSuite) TestRollBackOnCommitError(c *C) {
	databag := aspects.NewJSONDataBag()
	witness := &witnessReadWriter{bag: databag}
	schema := &failingSchema{err: errors.New("expected error")}
	tx, err := aspects.NewTransaction(witness.read, witness.write, schema)
	c.Assert(err, IsNil)

	err = tx.Set("foo", "bar")
	c.Assert(err, IsNil)

	err = tx.Commit()
	c.Assert(err, ErrorMatches, "expected error")

	// nothing was committed
	c.Assert(witness.writeCalled, Equals, 0)
	c.Assert(txData(c, tx), Equals, "{}")

	// but subsequent Gets still read the uncommitted values
	val, err := tx.Get("foo")
	c.Assert(err, IsNil)
	c.Assert(val, Equals, "bar")
}

func (s *transactionTestSuite) TestManyWrites(c *C) {
	databag := aspects.NewJSONDataBag()
	witness := &witnessReadWriter{bag: databag}
	schema := aspects.NewJSONSchema()
	tx, err := aspects.NewTransaction(witness.read, witness.write, schema)
	c.Assert(err, IsNil)

	err = tx.Set("foo", "bar")
	c.Assert(err, IsNil)
	err = tx.Set("foo", "baz")
	c.Assert(err, IsNil)

	err = tx.Commit()
	c.Assert(err, IsNil)
	c.Assert(witness.writeCalled, Equals, 1)

	// writes are applied in chronological order
	c.Assert(txData(c, tx), Equals, `{"foo":"baz"}`)

	value, err := witness.writtenDatabag.Get("foo")
	c.Assert(err, IsNil)
	c.Assert(value, Equals, "baz")
}

func (s *transactionTestSuite) TestCommittedIncludesRecentWrites(c *C) {
	databag := aspects.NewJSONDataBag()
	witness := &witnessReadWriter{bag: databag}
	schema := aspects.NewJSONSchema()
	tx, err := aspects.NewTransaction(witness.read, witness.write, schema)
	c.Assert(err, IsNil)
	c.Assert(witness.readCalled, Equals, 1)

	err = tx.Set("foo", "bar")
	c.Assert(err, IsNil)

	err = databag.Set("bar", "baz")
	c.Assert(err, IsNil)

	err = tx.Commit()
	c.Assert(err, IsNil)
	// databag was read from state before writing
	c.Assert(witness.readCalled, Equals, 2)
	c.Assert(witness.writeCalled, Equals, 1)

	// writes are applied in chronological order
	value, err := witness.writtenDatabag.Get("foo")
	c.Assert(err, IsNil)
	c.Assert(value, Equals, "bar")

	// contains recent values not written by the transaction
	value, err = witness.writtenDatabag.Get("bar")
	c.Assert(err, IsNil)
	c.Assert(value, Equals, "baz")
}

func (s *transactionTestSuite) TestCommittedIncludesPreviousCommit(c *C) {
	var databag aspects.JSONDataBag
	readBag := func() (aspects.JSONDataBag, error) {
		if databag == nil {
			return aspects.NewJSONDataBag(), nil
		}
		return databag, nil
	}

	writeBag := func(bag aspects.JSONDataBag) error {
		databag = bag
		return nil
	}

	schema := aspects.NewJSONSchema()
	txOne, err := aspects.NewTransaction(readBag, writeBag, schema)
	c.Assert(err, IsNil)

	txTwo, err := aspects.NewTransaction(readBag, writeBag, schema)
	c.Assert(err, IsNil)

	err = txOne.Set("foo", "bar")
	c.Assert(err, IsNil)

	err = txTwo.Set("bar", "baz")
	c.Assert(err, IsNil)

	err = txOne.Commit()
	c.Assert(err, IsNil)

	value, err := databag.Get("foo")
	c.Assert(err, IsNil)
	c.Assert(value, Equals, "bar")

	value, err = databag.Get("bar")
	c.Assert(err, FitsTypeOf, aspects.PathError(""))
	c.Assert(value, IsNil)

	err = txTwo.Commit()
	c.Assert(err, IsNil)

	value, err = databag.Get("foo")
	c.Assert(err, IsNil)
	c.Assert(value, Equals, "bar")

	value, err = databag.Get("bar")
	c.Assert(err, IsNil)
	c.Assert(value, Equals, "baz")
}

func (s *transactionTestSuite) TestTransactionBagReadError(c *C) {
	var readErr error
	readBag := func() (aspects.JSONDataBag, error) {
		return nil, readErr
	}
	writeBag := func(_ aspects.JSONDataBag) error {
		return nil
	}

	schema := aspects.NewJSONSchema()
	txOne, err := aspects.NewTransaction(readBag, writeBag, schema)
	c.Assert(err, IsNil)

	readErr = errors.New("expected")
	// Commit()'s databag read fails
	err = txOne.Commit()
	c.Assert(err, ErrorMatches, "expected")

	// NewTransaction()'s databag read fails
	txOne, err = aspects.NewTransaction(readBag, writeBag, schema)
	c.Assert(err, ErrorMatches, "expected")
}

func (s *transactionTestSuite) TestTransactionBagWriteError(c *C) {
	readBag := func() (aspects.JSONDataBag, error) {
		return nil, nil
	}
	var writeErr error
	writeBag := func(_ aspects.JSONDataBag) error {
		return writeErr
	}

	schema := aspects.NewJSONSchema()
	txOne, err := aspects.NewTransaction(readBag, writeBag, schema)
	c.Assert(err, IsNil)

	writeErr = errors.New("expected")
	// Commit()'s databag write fails
	err = txOne.Commit()
	c.Assert(err, ErrorMatches, "expected")
}

func (s *transactionTestSuite) TestTransactionReadsIsolated(c *C) {
	databag := aspects.NewJSONDataBag()
	readBag := func() (aspects.JSONDataBag, error) {
		return databag, nil
	}
	writeBag := func(aspects.JSONDataBag) error {
		return nil
	}

	schema := aspects.NewJSONSchema()
	tx, err := aspects.NewTransaction(readBag, writeBag, schema)
	c.Assert(err, IsNil)

	err = databag.Set("foo", "bar")
	c.Assert(err, IsNil)

	_, err = tx.Get("foo")
	c.Assert(err, FitsTypeOf, aspects.PathError(""))
}

func (s *transactionTestSuite) TestReadDatabagsAreCopiedForIsolation(c *C) {
	witness := &witnessReadWriter{bag: aspects.NewJSONDataBag()}
	schema := &failingSchema{}
	tx, err := aspects.NewTransaction(witness.read, witness.write, schema)
	c.Assert(err, IsNil)

	err = tx.Set("foo", "bar")
	c.Assert(err, IsNil)

	err = tx.Commit()
	c.Assert(err, IsNil)

	err = tx.Set("foo", "baz")
	c.Assert(err, IsNil)

	value, err := witness.writtenDatabag.Get("foo")
	c.Assert(err, IsNil)
	c.Assert(value, Equals, "bar")

	schema.err = errors.New("expected error")
	err = tx.Commit()
	c.Assert(err, ErrorMatches, "expected error")

	value, err = witness.writtenDatabag.Get("foo")
	c.Assert(err, IsNil)
	c.Assert(value, Equals, "bar")
}

func (s *transactionTestSuite) TestUnset(c *C) {
	witness := &witnessReadWriter{bag: aspects.NewJSONDataBag()}
	tx, err := aspects.NewTransaction(witness.read, witness.write, aspects.NewJSONSchema())
	c.Assert(err, IsNil)

	err = tx.Set("foo", "bar")
	c.Assert(err, IsNil)

	err = tx.Commit()
	c.Assert(err, IsNil)

	val, err := witness.writtenDatabag.Get("foo")
	c.Assert(err, IsNil)
	c.Assert(val, Equals, "bar")

	err = tx.Unset("foo")
	c.Assert(err, IsNil)

	err = tx.Commit()
	c.Assert(err, IsNil)

	_, err = witness.writtenDatabag.Get("foo")
	c.Assert(err, FitsTypeOf, aspects.PathError(""))
}

func txData(c *C, tx *aspects.Transaction) string {
	data, err := tx.Data()
	c.Assert(err, IsNil)
	return string(data)
}
