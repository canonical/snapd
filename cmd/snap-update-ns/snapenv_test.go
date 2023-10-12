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
	"fmt"
	"os"

	. "gopkg.in/check.v1"

	update "github.com/snapcore/snapd/cmd/snap-update-ns"
	"github.com/snapcore/snapd/osutil"
)

type envSuite struct{}

var _ = Suite(&envSuite{})

func (s *envSuite) TestSnapEnvRealHomeHappy(c *C) {
	restore := update.MockSnapConfineUserEnv("/run/user/1000/snap.snapname", "/home/user", "1000")
	defer restore()
	realHome, err := update.SnapEnvRealHome()
	c.Assert(err, IsNil)
	c.Assert(realHome, Equals, "/home/user")
}

func (s *envSuite) TestSnapEnvRealHomeErrorOSEnvironmentUnescapeUnsafe(c *C) {
	restore := update.MockOSEnvironmentUnescapeUnsafe(func(unsafeEscapePrefix string) (osutil.Environment, error) {
		return nil, fmt.Errorf("OSEnvironmentUnescapeUnsafe error")
	})
	defer restore()
	realHome, err := update.SnapEnvRealHome()
	c.Assert(realHome, Equals, "")
	c.Assert(err, ErrorMatches, "OSEnvironmentUnescapeUnsafe error")
}

func (s *envSuite) TestSnapEnvRealHomeErrorNoRealHome(c *C) {
	restore := update.MockSnapConfineUserEnv("/run/user/1000/snap.snapname", "/home/user", "1000")
	defer restore()
	os.Unsetenv("SNAP_REAL_HOME")
	realHome, err := update.SnapEnvRealHome()
	c.Assert(realHome, Equals, "")
	c.Assert(err, ErrorMatches, "cannot find environment variable \"SNAP_REAL_HOME\"")
}

func (s *envSuite) TestSnapEnvRealHomeErrorRealHomeEmpty(c *C) {
	restore := update.MockSnapConfineUserEnv("/run/user/1000/snap.snapname", "", "1000")
	defer restore()
	realHome, err := update.SnapEnvRealHome()
	c.Assert(realHome, Equals, "")
	c.Assert(err, ErrorMatches, "environment variable \"SNAP_REAL_HOME\" value is empty")
}
