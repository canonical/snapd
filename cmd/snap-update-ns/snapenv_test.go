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

package main_test

import (
	"os"

	. "gopkg.in/check.v1"

	update "github.com/snapcore/snapd/cmd/snap-update-ns"
)

type envSuite struct{}

var _ = Suite(&envSuite{})

func (s *envSuite) TestSnapEnvHappy(c *C) {
	restore := update.MockSnapConfineUserEnv("/run/user/1000/snap.snapname", "/home/user", "1000")
	defer restore()
	env, err := update.SnapEnv(1000)
	c.Assert(err, IsNil)
	c.Assert(env["XDG_RUNTIME_DIR"], Equals, "/run/user/1000/snap.snapname")
	c.Assert(env["SNAP_REAL_HOME"], Equals, "/home/user")
	c.Assert(env["SNAP_UID"], Equals, "1000")
}

// TODO: Find a way to test this
func (s *envSuite) TestSnapEnvErrorOsEnv(c *C) {
}

func (s *envSuite) TestSnapEnvErrorNoSnapUID(c *C) {
	restore := update.MockSnapConfineUserEnv("/run/user/1000/snap.snapname", "/home/user", "1000")
	defer restore()
	os.Unsetenv("SNAP_UID")
	env, err := update.SnapEnv(1000)
	c.Assert(env, IsNil)
	c.Assert(err, ErrorMatches, "cannot find environment variable \"SNAP_UID\"")
}

func (s *envSuite) TestSnapEnvErrorConv(c *C) {
	restore := update.MockSnapConfineUserEnv("/run/user/1000/snap.snapname", "/home/user", "100x")
	defer restore()
	env, err := update.SnapEnv(1000)
	c.Assert(env, IsNil)
	c.Assert(err, ErrorMatches, "cannot convert environment variable \"SNAP_UID\" value \"100x\" to an integer")
}

func (s *envSuite) TestSnapEnvErrorUIDMismatch(c *C) {
	restore := update.MockSnapConfineUserEnv("/run/user/1000/snap.snapname", "/home/user", "1000")
	defer restore()
	env, err := update.SnapEnv(1001)
	c.Assert(env, IsNil)
	c.Assert(err, ErrorMatches, "environment variable \"SNAP_UID\" value 1000 does not match current uid 1001")
}

func (s *envSuite) TestSnapEnvRealHomeHappy(c *C) {
	restore := update.MockSnapConfineUserEnv("/run/user/1000/snap.snapname", "/home/user", "1000")
	defer restore()
	realHome, err := update.SnapEnvRealHome(1000)
	c.Assert(err, IsNil)
	c.Assert(realHome, Equals, "/home/user")
}

func (s *envSuite) TestSnapEnvRealHomeErrorNoRealHome(c *C) {
	restore := update.MockSnapConfineUserEnv("/run/user/1000/snap.snapname", "/home/user", "1000")
	defer restore()
	os.Unsetenv("SNAP_REAL_HOME")
	realHome, err := update.SnapEnvRealHome(1000)
	c.Assert(realHome, Equals, "")
	c.Assert(err, ErrorMatches, "cannot find environment variable \"SNAP_REAL_HOME\"")
}

func (s *envSuite) TestSnapEnvRealHomeErrorRealHomeEmpty(c *C) {
	restore := update.MockSnapConfineUserEnv("/run/user/1000/snap.snapname", "", "1000")
	defer restore()
	realHome, err := update.SnapEnvRealHome(1000)
	c.Assert(realHome, Equals, "")
	c.Assert(err, ErrorMatches, "environment variable \"SNAP_REAL_HOME\" value is empty")
}
