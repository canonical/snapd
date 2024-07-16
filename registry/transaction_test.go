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

package registry_test

import (
	"errors"

	"github.com/snapcore/snapd/registry"
	. "gopkg.in/check.v1"
)

type transactionTestSuite struct{}

func newRegistry(c *C, schema registry.Schema) *registry.Registry {
	registry, err := registry.New("my-account", "my-reg", map[string]interface{}{
		"my-view": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo", "storage": "foo"},
			},
		},
	}, schema)
	c.Assert(err, IsNil)
	return registry
}

var _ = Suite(&transactionTestSuite{})

type witnessReadWriter struct {
	readCalled     int
	writeCalled    int
	bag            registry.JSONDataBag
	writtenDatabag registry.JSONDataBag
}

func (w *witnessReadWriter) read() (registry.JSONDataBag, error) {
	w.readCalled++
	return w.bag, nil
}

func (w *witnessReadWriter) write(bag registry.JSONDataBag) error {
	w.writeCalled++
	w.writtenDatabag = bag
	return nil
}

func (s *transactionTestSuite) TestSet(c *C) {
	bag := registry.NewJSONDataBag()
	witness := &witnessReadWriter{bag: bag}
	reg := newRegistry(c, registry.NewJSONSchema())
	tx, err := registry.NewTransaction(reg, witness.read, witness.write)
	c.Assert(err, IsNil)
	c.Assert(witness.readCalled, Equals, 1)

	err = tx.Set("foo", "bar")
	c.Assert(err, IsNil)
	c.Assert(witness.writeCalled, Equals, 0)

	_, err = witness.writtenDatabag.Get("foo")
	c.Assert(err, FitsTypeOf, registry.PathError(""))
}

func (s *transactionTestSuite) TestCommit(c *C) {
	witness := &witnessReadWriter{bag: registry.NewJSONDataBag()}
	reg := newRegistry(c, registry.NewJSONSchema())
	tx, err := registry.NewTransaction(reg, witness.read, witness.write)
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
	databag := registry.NewJSONDataBag()
	witness := &witnessReadWriter{bag: databag}
	reg := newRegistry(c, registry.NewJSONSchema())
	tx, err := registry.NewTransaction(reg, witness.read, witness.write)
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

func (f *failingSchema) SchemaAt(path []string) ([]registry.Schema, error) {
	return []registry.Schema{f}, nil
}

func (f *failingSchema) Type() registry.SchemaType {
	return registry.Any
}

func (s *transactionTestSuite) TestRollBackOnCommitError(c *C) {
	databag := registry.NewJSONDataBag()
	witness := &witnessReadWriter{bag: databag}
	reg := newRegistry(c, &failingSchema{err: errors.New("expected error")})
	tx, err := registry.NewTransaction(reg, witness.read, witness.write)
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
	databag := registry.NewJSONDataBag()
	witness := &witnessReadWriter{bag: databag}
	reg := newRegistry(c, registry.NewJSONSchema())
	tx, err := registry.NewTransaction(reg, witness.read, witness.write)
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
	databag := registry.NewJSONDataBag()
	witness := &witnessReadWriter{bag: databag}
	reg := newRegistry(c, registry.NewJSONSchema())
	tx, err := registry.NewTransaction(reg, witness.read, witness.write)
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
	var databag registry.JSONDataBag
	readBag := func() (registry.JSONDataBag, error) {
		if databag == nil {
			return registry.NewJSONDataBag(), nil
		}
		return databag, nil
	}

	writeBag := func(bag registry.JSONDataBag) error {
		databag = bag
		return nil
	}

	reg := newRegistry(c, registry.NewJSONSchema())
	txOne, err := registry.NewTransaction(reg, readBag, writeBag)
	c.Assert(err, IsNil)

	txTwo, err := registry.NewTransaction(reg, readBag, writeBag)
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
	c.Assert(err, FitsTypeOf, registry.PathError(""))
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
	readBag := func() (registry.JSONDataBag, error) {
		return nil, readErr
	}
	writeBag := func(_ registry.JSONDataBag) error {
		return nil
	}

	reg := newRegistry(c, registry.NewJSONSchema())
	txOne, err := registry.NewTransaction(reg, readBag, writeBag)
	c.Assert(err, IsNil)

	readErr = errors.New("expected")
	// Commit()'s databag read fails
	err = txOne.Commit()
	c.Assert(err, ErrorMatches, "expected")

	// NewTransaction()'s databag read fails
	txOne, err = registry.NewTransaction(reg, readBag, writeBag)
	c.Assert(err, ErrorMatches, "expected")
}

func (s *transactionTestSuite) TestTransactionBagWriteError(c *C) {
	readBag := func() (registry.JSONDataBag, error) {
		return nil, nil
	}
	var writeErr error
	writeBag := func(_ registry.JSONDataBag) error {
		return writeErr
	}

	reg := newRegistry(c, registry.NewJSONSchema())
	txOne, err := registry.NewTransaction(reg, readBag, writeBag)
	c.Assert(err, IsNil)

	writeErr = errors.New("expected")
	// Commit()'s databag write fails
	err = txOne.Commit()
	c.Assert(err, ErrorMatches, "expected")
}

func (s *transactionTestSuite) TestTransactionReadsIsolated(c *C) {
	databag := registry.NewJSONDataBag()
	readBag := func() (registry.JSONDataBag, error) {
		return databag, nil
	}
	writeBag := func(registry.JSONDataBag) error {
		return nil
	}

	reg := newRegistry(c, registry.NewJSONSchema())
	tx, err := registry.NewTransaction(reg, readBag, writeBag)
	c.Assert(err, IsNil)

	err = databag.Set("foo", "bar")
	c.Assert(err, IsNil)

	_, err = tx.Get("foo")
	c.Assert(err, FitsTypeOf, registry.PathError(""))
}

func (s *transactionTestSuite) TestReadDatabagsAreCopiedForIsolation(c *C) {
	witness := &witnessReadWriter{bag: registry.NewJSONDataBag()}
	schema := &failingSchema{}
	reg := newRegistry(c, schema)
	tx, err := registry.NewTransaction(reg, witness.read, witness.write)
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
	witness := &witnessReadWriter{bag: registry.NewJSONDataBag()}
	reg := newRegistry(c, registry.NewJSONSchema())
	tx, err := registry.NewTransaction(reg, witness.read, witness.write)
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
	c.Assert(err, FitsTypeOf, registry.PathError(""))
}

func txData(c *C, tx *registry.Transaction) string {
	data, err := tx.Data()
	c.Assert(err, IsNil)
	return string(data)
}
