// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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

package lsm_test

import (
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/sandbox/lsm"
	"github.com/snapcore/snapd/testutil"
)

func TestLSM(t *testing.T) {
	TestingT(t)
}

type lsmSuite struct {
	testutil.BaseTest
}

var _ = Suite(&lsmSuite{})

func (*lsmSuite) TestIDString(c *C) {
	c.Check(lsm.LSM_ID_UNDEF.String(), Equals, "undef")
	c.Check(lsm.LSM_ID_APPARMOR.String(), Equals, "apparmor")
	c.Check(lsm.LSM_ID_SELINUX.String(), Equals, "selinux")
	c.Check(lsm.ID(999).String(), Equals, "(lsm-id:999)")
}

func (*lsmSuite) TestIDHasStringContext(c *C) {
	c.Check(lsm.LSM_ID_UNDEF.HasStringContext(), Equals, false)
	c.Check(lsm.ID(999).HasStringContext(), Equals, false)

	c.Check(lsm.LSM_ID_APPARMOR.HasStringContext(), Equals, true)
	c.Check(lsm.LSM_ID_SELINUX.HasStringContext(), Equals, true)
}

func (*lsmSuite) TestContextAsString(c *C) {
	c.Check(lsm.ContextAsString([]byte("foo bar baz")), Equals, "foo bar baz")
	c.Check(lsm.ContextAsString([]byte{'f', 'o', 'o', 0}), Equals, "foo")
}
