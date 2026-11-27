// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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
 */

package boot_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/testutil"
)

type runSystemSuite struct {
	testutil.BaseTest
	bl *bootloadertest.MockBootloader
}

var _ = Suite(&runSystemSuite{})

func (s *runSystemSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })

	s.bl = bootloadertest.Mock("mock", c.MkDir())
	bootloader.Force(s.bl)
	s.AddCleanup(func() { bootloader.Force(nil) })
}

func (s *runSystemSuite) TestTryRejectsDifferentPendingSystem(c *C) {
	c.Assert(s.bl.SetBootVars(map[string]string{"try_run_system": "poc-b"}), IsNil)
	err := boot.SetTryRunSystem("poc-c")
	c.Assert(err, ErrorMatches, `cannot try run system "poc-c" while "poc-b" is pending`)
}

func (s *runSystemSuite) TestClearTryWithoutLabelClearsPending(c *C) {
	c.Assert(s.bl.SetBootVars(map[string]string{"run_system": "poc-a", "try_run_system": "poc-b"}), IsNil)

	c.Assert(boot.ClearTryRunSystem(""), IsNil)

	state, err := boot.CurrentRunSystem()
	c.Assert(err, IsNil)
	c.Check(state, DeepEquals, &boot.RunSystemState{Current: "poc-a"})
}

func (s *runSystemSuite) TestClearTryMatchingPendingSystem(c *C) {
	c.Assert(s.bl.SetBootVars(map[string]string{"run_system": "poc-a", "try_run_system": "poc-b"}), IsNil)

	c.Assert(boot.ClearTryRunSystem("poc-b"), IsNil)

	state, err := boot.CurrentRunSystem()
	c.Assert(err, IsNil)
	c.Check(state, DeepEquals, &boot.RunSystemState{Current: "poc-a"})
}

func (s *runSystemSuite) TestClearTryRejectsDifferentPendingSystem(c *C) {
	c.Assert(s.bl.SetBootVars(map[string]string{"run_system": "poc-a", "try_run_system": "poc-b"}), IsNil)
	s.bl.SetBootVarsCalls = 0

	err := boot.ClearTryRunSystem("poc-c")
	c.Assert(err, ErrorMatches, `cannot clear run system "poc-c" while "poc-b" is pending`)
	c.Check(s.bl.SetBootVarsCalls, Equals, 0)

	state, err := boot.CurrentRunSystem()
	c.Assert(err, IsNil)
	c.Check(state, DeepEquals, &boot.RunSystemState{Current: "poc-a", Try: "poc-b"})
}

func (s *runSystemSuite) TestClearTryRejectsInvalidLabel(c *C) {
	c.Assert(s.bl.SetBootVars(map[string]string{"try_run_system": "poc-b"}), IsNil)
	s.bl.SetBootVarsCalls = 0

	err := boot.ClearTryRunSystem("Poc-B")
	c.Assert(err, ErrorMatches, `invalid seed system label: "Poc-B"`)
	c.Check(s.bl.SetBootVarsCalls, Equals, 0)
}
