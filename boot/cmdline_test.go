// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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
	"errors"
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	"github.com/snapcore/snapd/testutil"
)

var _ = Suite(&kernelCommandLineSuite{})

// baseBootSuite is used to setup the common test environment
type kernelCommandLineSuite struct {
	testutil.BaseTest
	rootDir string
}

func (s *kernelCommandLineSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.rootDir = c.MkDir()

	err := os.MkdirAll(filepath.Join(s.rootDir, "proc"), 0755)
	c.Assert(err, IsNil)
	restore := boot.MockProcCmdline(filepath.Join(s.rootDir, "proc/cmdline"))
	s.AddCleanup(restore)
}

func (s *kernelCommandLineSuite) mockProcCmdlineContent(c *C, newContent string) {
	mockProcCmdline := filepath.Join(s.rootDir, "proc/cmdline")
	err := ioutil.WriteFile(mockProcCmdline, []byte(newContent), 0644)
	c.Assert(err, IsNil)
}

func (s *kernelCommandLineSuite) TestModeAndLabel(c *C) {
	for _, tc := range []struct {
		cmd   string
		mode  string
		label string
		err   string
	}{{
		cmd:   "snapd_recovery_mode= snapd_recovery_system=this-is-a-label other-option=foo",
		mode:  boot.ModeInstall,
		label: "this-is-a-label",
	}, {
		cmd:   "snapd_recovery_system=label foo=bar foobaz=\\0\\0123 snapd_recovery_mode=install",
		label: "label",
		mode:  boot.ModeInstall,
	}, {
		cmd:  "snapd_recovery_mode=run snapd_recovery_system=1234",
		mode: boot.ModeRun,
	}, {
		cmd: "option=1 other-option=\0123 none",
		err: "cannot detect mode nor recovery system to use",
	}, {
		cmd: "snapd_recovery_mode=install-foo",
		err: `cannot use unknown mode "install-foo"`,
	}, {
		// no recovery system label
		cmd: "snapd_recovery_mode=install foo=bar",
		err: `cannot specify install mode without system label`,
	}, {
		// boot scripts couldn't decide on mode
		cmd: "snapd_recovery_mode=install snapd_recovery_system=1234 snapd_recovery_mode=run",
		err: "cannot specify mode more than once",
	}, {
		// boot scripts couldn't decide which system to use
		cmd: "snapd_recovery_system=not-this-one snapd_recovery_mode=install snapd_recovery_system=1234",
		err: "cannot specify recovery system label more than once",
	}} {
		c.Logf("tc: %q", tc)
		s.mockProcCmdlineContent(c, tc.cmd)

		mode, label, err := boot.ModeAndRecoverySystemFromKernelCommandLine()
		if tc.err == "" {
			c.Assert(err, IsNil)
			c.Check(mode, Equals, tc.mode)
			c.Check(label, Equals, tc.label)
		} else {
			c.Assert(err, ErrorMatches, tc.err)
		}
	}
}

func (s *kernelCommandLineSuite) TestCommandLineNotManagedHappy(c *C) {
	model := makeMockUC20Model()

	bl := bootloadertest.Mock("btloader", c.MkDir())
	bootloader.Force(bl)
	defer bootloader.Force(nil)

	cmdline, err := boot.RecoveryCommandLine(model, "20200314")
	c.Assert(err, IsNil)
	c.Assert(cmdline, Equals, "")

	cmdline, err = boot.CommandLine(model)
	c.Assert(err, IsNil)
	c.Assert(cmdline, Equals, "")

	mbl := bl.WithManagedAssets()
	bootloader.Force(mbl)
	mbl.IsManaged = false

	cmdline, err = boot.RecoveryCommandLine(model, "20200314")
	c.Assert(err, IsNil)
	c.Assert(cmdline, Equals, "")

	cmdline, err = boot.CommandLine(model)
	c.Assert(err, IsNil)
	c.Assert(cmdline, Equals, "")
}

func (s *kernelCommandLineSuite) TestCommandLineNotUC20(c *C) {
	headers := map[string]interface{}{
		"type":         "model",
		"authority-id": "my-brand",
		"series":       "16",
		"brand-id":     "my-brand",
		"model":        "my-model",
		"display-name": "My Model",
		"architecture": "amd64",
		"base":         "core18",
		"gadget":       "pc=18",
		"kernel":       "pc-kernel=18",
		"timestamp":    "2018-01-01T08:00:00+00:00",
	}
	model := assertstest.FakeAssertion(headers).(*asserts.Model)

	bl := bootloadertest.Mock("btloader", c.MkDir())
	bootloader.Force(bl)
	defer bootloader.Force(nil)
	cmdline, err := boot.RecoveryCommandLine(model, "20200314")
	c.Assert(err, IsNil)
	c.Check(cmdline, Equals, "")

	cmdline, err = boot.CommandLine(model)
	c.Assert(err, IsNil)
	c.Check(cmdline, Equals, "")
}

func (s *kernelCommandLineSuite) TestComamndLineSystemManagedErr(c *C) {
	model := makeMockUC20Model()

	mbl := bootloadertest.Mock("btloader", c.MkDir()).WithManagedAssets()
	bootloader.Force(mbl)
	defer bootloader.Force(nil)

	errFail := errors.New("is managed fail")
	mbl.IsManagedErr = errFail

	cmdline, err := boot.RecoveryCommandLine(model, "20200314")
	c.Assert(err, ErrorMatches, "is managed fail")
	c.Assert(cmdline, Equals, "")
	cmdline, err = boot.CommandLine(model)
	c.Assert(err, ErrorMatches, "is managed fail")
	c.Assert(cmdline, Equals, "")

	mbl.IsManagedErr = nil
	mbl.IsManaged = true
	mbl.CommandLineErr = errors.New("kernel command line fail")

	cmdline, err = boot.RecoveryCommandLine(model, "20200314")
	c.Assert(err, ErrorMatches, "kernel command line fail")
	c.Assert(cmdline, Equals, "")
	cmdline, err = boot.CommandLine(model)
	c.Assert(err, ErrorMatches, "kernel command line fail")
	c.Assert(cmdline, Equals, "")
}

func (s *kernelCommandLineSuite) TestCommandLineManagedHappy(c *C) {
	model := makeMockUC20Model()

	mbl := bootloadertest.Mock("btloader", c.MkDir()).WithManagedAssets()
	bootloader.Force(mbl)
	defer bootloader.Force(nil)

	mbl.IsManaged = true
	mbl.StaticCommandLine = "panic=-1"

	cmdline, err := boot.RecoveryCommandLine(model, "20200314")
	c.Assert(err, IsNil)
	c.Assert(cmdline, Equals, "panic=-1 snapd_recovery_mode=recover snapd_recovery_system=20200314")
	cmdline, err = boot.CommandLine(model)
	c.Assert(err, IsNil)
	c.Assert(cmdline, Equals, "panic=-1")
}
