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
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/asserts/sysdb"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/state"
)

func TestAssertManager(t *testing.T) { TestingT(t) }

type assertMgrSuite struct {
	state *state.State
	mgr   *assertstate.AssertManager

	storeSigning *assertstest.StoreStack

	restore func()
}

var _ = Suite(&assertMgrSuite{})

func (s *assertMgrSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())

	rootPrivKey, _ := assertstest.GenerateKey(1024)
	storePrivKey, _ := assertstest.GenerateKey(752)
	s.storeSigning = assertstest.NewStoreStack("can0nical", rootPrivKey, storePrivKey)
	s.restore = sysdb.InjectTrusted(s.storeSigning.Trusted)

	s.state = state.New(nil)
	mgr, err := assertstate.Manager(s.state)
	c.Assert(err, IsNil)
	s.mgr = mgr
}

func (s *assertMgrSuite) TearDownTest(c *C) {
	s.restore()
}

func (s *assertMgrSuite) TestDB(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	db := assertstate.DB(s.state)
	c.Check(db, FitsTypeOf, (*asserts.Database)(nil))
}

func (s *assertMgrSuite) TestAdd(c *C) {
	dev1Acct := assertstest.NewAccount(s.storeSigning, "developer1", nil, "")

	s.state.Lock()
	defer s.state.Unlock()

	// prereq store key
	err := assertstate.Add(s.state, s.storeSigning.StoreAccountKey(""))
	c.Assert(err, IsNil)

	err = assertstate.Add(s.state, dev1Acct)
	c.Assert(err, IsNil)

	db := assertstate.DB(s.state)
	devAcct, err := db.Find(asserts.AccountType, map[string]string{
		"account-id": dev1Acct.AccountID(),
	})
	c.Assert(err, IsNil)
	c.Check(devAcct.(*asserts.Account).Username(), Equals, "developer1")

	// same revision => nop
	err = assertstate.Add(s.state, dev1Acct)
	c.Assert(err, IsNil)
}
