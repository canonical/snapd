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

package aspectstate_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/aspects"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/aspectstate"
	"github.com/snapcore/snapd/overlord/state"
)

type transactionTestSuite struct {
	state *state.State
}

var _ = Suite(&transactionTestSuite{})

func (s *transactionTestSuite) SetUpTest(_ *C) {
	s.state = overlord.Mock().State()
}

func (s *transactionTestSuite) TestDataIsCommitted(c *C) {
	databag := aspects.NewJSONDataBag()
	schema := aspects.NewJSONSchema()
	tx := aspectstate.NewTransaction(databag, schema)

	err := tx.Set("foo", "bar")
	c.Assert(err, IsNil)
	c.Assert(txData(c, tx), Equals, "{}")

	tx.Commit()
	c.Assert(txData(c, tx), Equals, `{"foo":"bar"}`)

	err = tx.Set("foo", "baz")
	c.Assert(err, IsNil)
	c.Assert(txData(c, tx), Equals, `{"foo":"bar"}`)
}

func (s *transactionTestSuite) TestGetUncommitted(c *C) {
	databag := aspects.NewJSONDataBag()
	schema := aspects.NewJSONSchema()
	tx := aspectstate.NewTransaction(databag, schema)

	err := tx.Set("foo", "bar")
	c.Assert(err, IsNil)
	// nothing was committed
	c.Assert(txData(c, tx), Equals, "{}")

	var val string
	err = tx.Get("foo", &val)
	c.Assert(err, IsNil)
	c.Assert(val, Equals, "bar")
}

func txData(c *C, tx *aspectstate.Transaction) string {
	data, err := tx.Data()
	c.Assert(err, IsNil)
	return string(data)
}
