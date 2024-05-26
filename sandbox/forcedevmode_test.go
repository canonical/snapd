// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2020 Canonical Ltd
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

package sandbox_test

import (
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/sandbox"
	"github.com/snapcore/snapd/sandbox/apparmor"
	"github.com/snapcore/snapd/sandbox/cgroup"
)

func Test(t *testing.T) { TestingT(t) }

type forceDevModeSuite struct{}

var _ = Suite(&forceDevModeSuite{})

func (s *forceDevModeSuite) TestForceDevMode(c *C) {
	runTest := func(apparmorLevel apparmor.LevelType, cgroupVersion int, expect bool) {
		restore := apparmor.MockLevel(apparmorLevel)
		defer restore()
		restore = cgroup.MockVersion(cgroupVersion, nil)
		defer restore()
		devMode := sandbox.ForceDevMode()
		c.Check(devMode, Equals, expect, Commentf("unexpected force-dev-mode for AppArmor level %v cgroup v%v", apparmorLevel, cgroupVersion))
	}

	for _, tc := range []struct {
		apparmorLevel apparmor.LevelType
		// cgroup v2 used to be a factor when checking for forced dev
		// mode
		cgroupVersion int
		exp           bool
	}{
		{apparmor.Full, cgroup.V1, false},
		{apparmor.Partial, cgroup.V1, true},
		// unified mode
		{apparmor.Full, cgroup.V2, false},
		{apparmor.Partial, cgroup.V2, true},
	} {
		runTest(tc.apparmorLevel, tc.cgroupVersion, tc.exp)
	}
}

func (s *forceDevModeSuite) TestMockForceDevMode(c *C) {
	for _, devmode := range []bool{true, false} {
		restore := sandbox.MockForceDevMode(devmode)
		defer restore()
		c.Assert(sandbox.ForceDevMode(), Equals, devmode, Commentf("wrong result for %#v", devmode))
	}
}
