// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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

package boot_test

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	"github.com/snapcore/snapd/testutil"
)

type rebootSuite struct {
	baseBootenvSuite
}

var _ = Suite(&rebootSuite{})

func (s *rebootSuite) SetUpTest(c *C) {
	s.baseBootenvSuite.SetUpTest(c)
	s.AddCleanup(boot.EnableTestingRebootFunction())
}

func (s *rebootSuite) TestRebootActionString(c *C) {
	c.Assert(fmt.Sprint(boot.RebootReboot), Equals, "system reboot")
	c.Assert(fmt.Sprint(boot.RebootHalt), Equals, "system halt")
	c.Assert(fmt.Sprint(boot.RebootPoweroff), Equals, "system poweroff")
}

func (s *rebootSuite) TestRebootHelper(c *C) {
	cmd := testutil.MockCommand(c, "shutdown", "")
	defer cmd.Restore()

	tests := []struct {
		delay    time.Duration
		delayArg string
	}{
		{-1, "+0"},
		{0, "+0"},
		{time.Minute, "+1"},
		{10 * time.Minute, "+10"},
		{30 * time.Second, "+0"},
	}

	args := []struct {
		a   boot.RebootAction
		arg string
		msg string
	}{
		{boot.RebootReboot, "-r", "reboot scheduled to update the system"},
		{boot.RebootHalt, "--halt", "system halt scheduled"},
		{boot.RebootPoweroff, "--poweroff", "system poweroff scheduled"},
	}

	for _, arg := range args {
		for _, t := range tests {
			err := boot.Reboot(arg.a, t.delay, nil)
			c.Assert(err, IsNil)
			c.Check(cmd.Calls(), DeepEquals, [][]string{
				{"shutdown", arg.arg, t.delayArg, arg.msg},
			})

			cmd.ForgetCalls()
		}
	}
}

func (s *rebootSuite) TestRebootWithArguments(c *C) {
	rbl := bootloadertest.Mock("rebootargs", "").WithRebootBootloader()
	bootloader.Force(rbl)
	s.AddCleanup(func() { bootloader.Force(nil) })
	rbl.RebootArgs = "0 tryboot"
	dir := c.MkDir()
	rebArgsPath := filepath.Join(dir, "reboot-param")
	restoreRebootArgs := boot.MockRebootArgsPath(rebArgsPath)
	defer restoreRebootArgs()

	cmd := testutil.MockCommand(c, "shutdown", "")
	defer cmd.Restore()

	err := boot.Reboot(0, 0, &boot.RebootInfo{RebootRequired: true, RebootBootloader: rbl})
	c.Assert(err, IsNil)
	c.Assert(rebArgsPath, testutil.FileEquals, "0 tryboot\n")
	c.Check(cmd.Calls(), DeepEquals, [][]string{
		{"shutdown", "-r", "+0", "reboot scheduled to update the system"},
	})
}

func (s *rebootSuite) TestRebootNoArguments(c *C) {
	rbl := bootloadertest.Mock("rebootargs", "").WithRebootBootloader()
	bootloader.Force(rbl)
	s.AddCleanup(func() { bootloader.Force(nil) })
	rbl.RebootArgs = ""
	dir := c.MkDir()
	rebArgsPath := filepath.Join(dir, "reboot-param")
	restoreRebootArgs := boot.MockRebootArgsPath(rebArgsPath)
	defer restoreRebootArgs()

	cmd := testutil.MockCommand(c, "shutdown", "")
	defer cmd.Restore()

	err := boot.Reboot(0, 0, nil)
	c.Assert(err, IsNil)

	_, err = os.Stat(rebArgsPath)
	c.Check(os.IsNotExist(err), Equals, true)

	c.Check(cmd.Calls(), DeepEquals, [][]string{
		{"shutdown", "-r", "+0", "reboot scheduled to update the system"},
	})
}
