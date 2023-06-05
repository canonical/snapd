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

	"github.com/snapcore/snapd/overlord/aspectstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/testutil"
)

type sysctlHijackerTestSuite struct {
	state *state.State
}

var _ = Suite(&sysctlHijackerTestSuite{})

func (s *sysctlHijackerTestSuite) TestSysctlHijackGet(c *C) {
	mockedCmd := testutil.MockCommand(c, "sysctl", "echo 'foo.bar = baz'")
	defer mockedCmd.Restore()

	hj := aspectstate.SysctlHijacker{}

	var val interface{}
	err := hj.Get("foo", &val)
	c.Assert(err, IsNil)
	c.Check(val, Equals, "baz")
}

func (s *sysctlHijackerTestSuite) TestSysctlHijackGetAll(c *C) {
	mockedCmd := testutil.MockCommand(c, "sysctl", `
echo 'foo.bar
a.b
c.d'`)
	defer mockedCmd.Restore()

	hj := aspectstate.SysctlHijacker{}

	var val interface{}
	err := hj.Get("all", &val)
	c.Assert(err, IsNil)
	c.Check(val, Equals, `foo.bar
a.b
c.d
`)
}

func (s *sysctlHijackerTestSuite) TestSysctlHijackSet(c *C) {
	hj := aspectstate.SysctlHijacker{}
	err := hj.Set("foo", "bar")
	c.Assert(err, testutil.ErrorIs, &aspectstate.UnsupportedOpError{})
	c.Assert(err, ErrorMatches, `cannot set aspect: unsupported operation`)
}
