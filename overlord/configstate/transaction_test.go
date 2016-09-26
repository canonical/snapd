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
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/configstate"
	"github.com/snapcore/snapd/overlord/state"
)

func TestConfigState(t *testing.T) { TestingT(t) }

type transactionSuite struct {
	state       *state.State
	transaction *configstate.Transaction
}

var _ = Suite(&transactionSuite{})

func (s *transactionSuite) SetUpTest(c *C) {
	s.state = state.New(nil)
	var err error
	s.state.Lock()
	defer s.state.Unlock()
	s.transaction, err = configstate.NewTransaction(s.state)
	c.Check(err, IsNil)
}

func (s *transactionSuite) TestNoOptionError(c *C) {
	var v interface{}
	err := s.transaction.Get("unknown", "what", &v)
	c.Assert(err, FitsTypeOf, &configstate.NoOptionError{})
	c.Check(err, ErrorMatches, `snap "unknown" has no "what" configuration option`)
}

func (s *transactionSuite) TestSetDoesNotTouchState(c *C) {
	c.Check(s.transaction.Set("test-snap", "foo", "bar"), IsNil)

	// Create a new transaction to grab a new snapshot of the state
	s.state.Lock()
	defer s.state.Unlock()
	transaction, err := configstate.NewTransaction(s.state)
	c.Check(err, IsNil)
	var value string
	err = transaction.Get("test-snap", "foo", &value)
	c.Check(err, NotNil, Commentf("Expected config set by first transaction to not be saved"))
}

func (s *transactionSuite) TestCommit(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	c.Check(s.transaction.Set("test-snap", "foo", "bar"), IsNil)
	s.transaction.Commit()

	// Create a new transaction to grab a new snapshot of the state
	transaction, err := configstate.NewTransaction(s.state)
	c.Check(err, IsNil)
	var value string
	err = transaction.Get("test-snap", "foo", &value)
	c.Check(err, IsNil, Commentf("Expected config set by first transaction to be saved"))
	c.Check(value, Equals, "bar")
}

func (s *transactionSuite) TestCommitOnlyCommitsChanges(c *C) {
	// Set the initial config
	s.state.Lock()
	defer s.state.Unlock()
	c.Check(s.transaction.Set("test-snap", "foo", "bar"), IsNil)
	s.transaction.Commit()

	// Create two new transactions
	transaction1, err := configstate.NewTransaction(s.state)
	c.Check(err, IsNil)
	transaction2, err := configstate.NewTransaction(s.state)
	c.Check(err, IsNil)

	// transaction1 will change the configuration item that is already present.
	c.Check(transaction1.Set("test-snap", "foo", "baz"), IsNil)
	transaction1.Commit()

	// transaction2 will add a new configuration item.
	c.Check(transaction2.Set("test-snap", "qux", "quux"), IsNil)
	transaction2.Commit()

	// Now verify that the change made by both transactions actually took place
	// (i.e. transaction1's change was not overridden by the old data in
	// transaction2).
	transaction, err := configstate.NewTransaction(s.state)
	c.Check(err, IsNil)

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
	s.state.Lock()
	defer s.state.Unlock()
	c.Check(s.transaction.Set("test-snap", "foo", "bar"), IsNil)
	s.transaction.Commit()

	transaction, err := configstate.NewTransaction(s.state)
	c.Check(err, IsNil)
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
	s.state.Lock()
	defer s.state.Unlock()
	c.Check(s.transaction.Set("test-snap", "foo", "initial"), IsNil)
	s.transaction.Commit()

	// Create two new transactions
	transaction1, err := configstate.NewTransaction(s.state)
	c.Check(err, IsNil)
	transaction2, err := configstate.NewTransaction(s.state)
	c.Check(err, IsNil)

	// Change the config in one
	c.Check(transaction1.Set("test-snap", "foo", "updated"), IsNil)
	transaction1.Commit()

	// Verify that the other transaction doesn't see the changes
	var value string
	c.Check(transaction2.Get("test-snap", "foo", &value), IsNil)
	c.Check(value, Equals, "initial", Commentf("Expected transaction2 to be isolated from transaction1"))
}
