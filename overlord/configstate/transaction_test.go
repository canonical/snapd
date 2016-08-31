// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package configstate_test

import (
	"encoding/json"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/configstate"
	"github.com/snapcore/snapd/overlord/state"
)

func TestConfigstate(t *testing.T) { TestingT(t) }

type transactionSuite struct {
	state       *state.State
	transaction *configstate.Transaction
}

var _ = Suite(&transactionSuite{})

func (s *transactionSuite) SetUpTest(c *C) {
	s.state = state.New(nil)
	s.transaction = configstate.NewTransaction(s.state)
}

func (s *transactionSuite) TestSetDoesNotTouchState(c *C) {
	c.Check(s.transaction.Set("test-snap", "foo", "bar"), IsNil)

	// Create a new transaction to grab a new snapshot of the state
	transaction := configstate.NewTransaction(s.state)
	var value string
	err := transaction.Get("test-snap", "foo", &value)
	c.Check(err, NotNil, Commentf("Expected config set by first transaction to not be saved"))
}

func (s *transactionSuite) TestCommit(c *C) {
	c.Check(s.transaction.Set("test-snap", "foo", "bar"), IsNil)
	s.transaction.Commit()

	// Create a new transaction to grab a new snapshot of the state
	transaction := configstate.NewTransaction(s.state)
	var value string
	err := transaction.Get("test-snap", "foo", &value)
	c.Check(err, IsNil, Commentf("Expected config set by first transaction to be saved"))
	c.Check(value, Equals, "bar")
}

func (s *transactionSuite) TestCommitOnlyCommitsChanges(c *C) {
	// Set the initial config
	c.Check(s.transaction.Set("test-snap", "foo", "bar"), IsNil)
	s.transaction.Commit()

	// Create two new transactions
	transaction1 := configstate.NewTransaction(s.state)
	transaction2 := configstate.NewTransaction(s.state)

	// transaction1 will change the configuration item that is already present.
	c.Check(transaction1.Set("test-snap", "foo", "baz"), IsNil)
	transaction1.Commit()

	// transaction2 will add a new configuration item.
	c.Check(transaction2.Set("test-snap", "qux", "quux"), IsNil)
	transaction2.Commit()

	// Now verify that the change made by both transactions actually took place
	// (i.e. transaction1's change was not overridden by the old data in
	// transaction2).
	transaction := configstate.NewTransaction(s.state)

	var value string
	c.Check(transaction.Get("test-snap", "foo", &value), IsNil)
	c.Check(value, Equals, "baz", Commentf("Expected 'test-snap' value for 'foo' to be set by transaction1"))

	c.Check(transaction.Get("test-snap", "qux", &value), IsNil)
	c.Check(value, Equals, "quux", Commentf("Expected 'test-snap' value for 'qux' to be set by transaction2"))
}

func (s *transactionSuite) TestGetNothing(c *C) {
	var value string
	err := s.transaction.Get("test-snap", "foo", &value)
	c.Check(err, NotNil, Commentf("Expected Get to fail if key not set"))
}

func (s *transactionSuite) TestGetCachedWrites(c *C) {
	// Get() should read the cached writes, even without a Commit()
	s.transaction.Set("test-snap", "foo", "bar")
	var value string
	err := s.transaction.Get("test-snap", "foo", &value)
	c.Check(err, IsNil, Commentf("Expected 'test-snap' config to contain 'foo'"))
	c.Check(value, Equals, "bar")
}

func (s *transactionSuite) TestGetOriginalEvenWithCachedWrites(c *C) {
	// Set the initial config
	c.Check(s.transaction.Set("test-snap", "foo", "bar"), IsNil)
	s.transaction.Commit()

	transaction := configstate.NewTransaction(s.state)
	c.Check(transaction.Set("test-snap", "baz", "qux"), IsNil)

	// Now get both the cached write as well as the initial config
	var value string
	c.Check(transaction.Get("test-snap", "foo", &value), IsNil)
	c.Check(value, Equals, "bar")
	c.Check(transaction.Get("test-snap", "baz", &value), IsNil)
	c.Check(value, Equals, "qux")
}

func (s *transactionSuite) TestIsolationFromOtherTransactions(c *C) {
	// Set the initial config
	c.Check(s.transaction.Set("test-snap", "foo", "initial"), IsNil)
	s.transaction.Commit()

	// Create two new transactions
	transaction1 := configstate.NewTransaction(s.state)
	transaction2 := configstate.NewTransaction(s.state)

	// Change the config in one
	c.Check(transaction1.Set("test-snap", "foo", "updated"), IsNil)
	transaction1.Commit()

	// Verify that the other transaction doesn't see the changes
	var value string
	c.Check(transaction2.Get("test-snap", "foo", &value), IsNil)
	c.Check(value, Equals, "initial", Commentf("Expected transaction2 to be isolated from transaction1"))
}

func (s *transactionSuite) TestSetUnmarshalable(c *C) {
	err := s.transaction.Set("test-snap", "foo", func() {})
	c.Check(err, ErrorMatches, ".*cannot marshal snap.*config value.*")
}

func (s *transactionSuite) TestMarshalTransactionConfigOnly(c *C) {
	c.Check(s.transaction.Set("test-snap", "foo", "bar"), IsNil)
	s.transaction.Commit()

	bytes, err := json.Marshal(s.transaction)
	c.Check(err, IsNil)
	c.Check(string(bytes), Equals,
		"{\"config\":{\"test-snap\":{\"foo\":\"bar\"}},\"write-cache\":{}}")
}

func (s *transactionSuite) TestMarshalTransactionCacheOnly(c *C) {
	c.Check(s.transaction.Set("test-snap", "foo", "bar"), IsNil)

	bytes, err := json.Marshal(s.transaction)
	c.Check(err, IsNil)
	c.Check(string(bytes), Equals,
		"{\"config\":{},\"write-cache\":{\"test-snap\":{\"foo\":\"bar\"}}}")
}

func (s *transactionSuite) TestMarshalTransactionConfigAndCache(c *C) {
	// Set an initial config
	c.Check(s.transaction.Set("test-snap", "foo", "bar"), IsNil)
	s.transaction.Commit()

	transaction := configstate.NewTransaction(s.state)

	// Make another, uncommitted change
	c.Check(transaction.Set("test-snap", "baz", "qux"), IsNil)

	// Now marshal the transaction, and expect to see the initial config along
	// with the write cache.
	bytes, err := json.Marshal(transaction)
	c.Check(err, IsNil)
	c.Check(string(bytes), Equals,
		"{\"config\":{\"test-snap\":{\"foo\":\"bar\"}},\"write-cache\":{\"test-snap\":{\"baz\":\"qux\"}}}")
}

func (s *transactionSuite) TestUnmarshalTransaction(c *C) {
	// Set an initial config
	c.Check(s.transaction.Set("test-snap", "foo", "bar"), IsNil)
	s.transaction.Commit()

	transaction := configstate.NewTransaction(s.state)

	// Make another, uncommitted change
	c.Check(transaction.Set("test-snap", "baz", "qux"), IsNil)

	// Now marshal the transaction, and expect to see the initial config along
	// with the write cache.
	bytes, err := json.Marshal(transaction)
	c.Check(err, IsNil)

	// Now unmarshal into a new transaction
	var newTransaction configstate.Transaction
	err = json.Unmarshal(bytes, &newTransaction)
	c.Check(err, IsNil)

	// Verify that it unmarshaled as expected
	var value string
	c.Check(newTransaction.Get("test-snap", "foo", &value), IsNil)
	c.Check(value, Equals, "bar")
	c.Check(newTransaction.Get("test-snap", "baz", &value), IsNil)
	c.Check(value, Equals, "qux")
}
