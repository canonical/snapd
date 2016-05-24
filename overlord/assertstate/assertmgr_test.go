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

package assertstate_test

import (
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/dirs"

	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/state"
)

func TestAssertManager(t *testing.T) { TestingT(t) }

type assertMgrSuite struct{}

var _ = Suite(&assertMgrSuite{})

func (ams *assertMgrSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
}

func (ams *assertMgrSuite) TestManagerAndDB(c *C) {
	s := state.New(nil)
	mgr, err := assertstate.Manager(s)
	c.Assert(err, IsNil)

	db := mgr.DB()
	c.Check(db, FitsTypeOf, (*asserts.Database)(nil))
}
