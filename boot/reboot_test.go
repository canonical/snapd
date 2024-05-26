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
	"path/filepath"
	"time"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
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
	bl := bootloadertest.Mock("test", "")
	bootloader.Force(bl)
	s.AddCleanup(func() { bootloader.Force(nil) })

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
			mylog.Check(boot.Reboot(arg.a, t.delay, nil))

			c.Check(cmd.Calls(), DeepEquals, [][]string{
				{"shutdown", arg.arg, t.delayArg, arg.msg},
			})

			cmd.ForgetCalls()
		}
	}
}

func (s *rebootSuite) TestRebootWithBootloaderError(c *C) {
	rbl := bootloadertest.Mock("rebootargs", "")
	bootloader.Force(rbl)
	s.AddCleanup(func() { bootloader.Force(nil) })

	r := boot.MockBootloaderFind(func(rootdir string, opts *bootloader.Options) (bootloader.Bootloader, error) {
		c.Check(rootdir, Equals, "")
		c.Check(opts, IsNil)
		return nil, fmt.Errorf("oh no")
	})
	defer r()

	cmd := testutil.MockCommand(c, "shutdown", "")
	defer cmd.Restore()
	mylog.Check(boot.Reboot(0, 0, &boot.RebootInfo{
		BootloaderOptions: nil,
	}))
	c.Assert(err, ErrorMatches, `cannot resolve bootloader: oh no`)
	c.Check(cmd.Calls(), HasLen, 0)
}

func (s *rebootSuite) TestRebootWithBootloader(c *C) {
	rbl := bootloadertest.Mock("rebootargs", "")
	bootloader.Force(rbl)
	s.AddCleanup(func() { bootloader.Force(nil) })

	// still get the file-path so we can ensure that the file
	// has not been written
	dir := c.MkDir()
	rebArgsPath := filepath.Join(dir, "reboot-param")
	restoreRebootArgs := boot.MockRebootArgsPath(rebArgsPath)
	defer restoreRebootArgs()

	cmd := testutil.MockCommand(c, "shutdown", "")
	defer cmd.Restore()
	mylog.Check(boot.Reboot(0, 0, &boot.RebootInfo{
		BootloaderOptions: &bootloader.Options{
			Role: bootloader.RoleRunMode,
		},
	}))


	// ensure the arguments file is absent
	c.Assert(rebArgsPath, testutil.FileAbsent)
	c.Check(cmd.Calls(), DeepEquals, [][]string{
		{"shutdown", "-r", "+0", "reboot scheduled to update the system"},
	})
}

func (s *rebootSuite) TestRebootWithRebootBootloader(c *C) {
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
	mylog.Check(boot.Reboot(0, 0, &boot.RebootInfo{
		BootloaderOptions: &bootloader.Options{
			Role: bootloader.RoleRunMode,
		},
	}))

	c.Assert(rebArgsPath, testutil.FileEquals, "0 tryboot\n")
	c.Check(cmd.Calls(), DeepEquals, [][]string{
		{"shutdown", "-r", "+0", "reboot scheduled to update the system"},
	})
}

func (s *rebootSuite) TestRebootWithRebootBootloaderNoArguments(c *C) {
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
	mylog.Check(boot.Reboot(0, 0, nil))


	// ensure the arguments file is absent
	c.Assert(rebArgsPath, testutil.FileAbsent)
	c.Check(cmd.Calls(), DeepEquals, [][]string{
		{"shutdown", "-r", "+0", "reboot scheduled to update the system"},
	})
}
