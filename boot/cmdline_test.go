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
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/boot/boottest"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	"github.com/snapcore/snapd/osutil"
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
	restore := osutil.MockProcCmdline(filepath.Join(s.rootDir, "proc/cmdline"))
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
		cmd: "snapd_recovery_system=1234",
		err: `cannot specify system label without a mode`,
	}, {
		// multiple kernel command line params end up using the last one - this
		// effectively matches the kernel handling too
		cmd:  "snapd_recovery_mode=install snapd_recovery_system=1234 snapd_recovery_mode=run",
		mode: "run",
		// label gets unset because it's not used for run mode
		label: "",
	}, {
		cmd:   "snapd_recovery_system=not-this-one snapd_recovery_mode=install snapd_recovery_system=1234",
		mode:  "install",
		label: "1234",
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

func (s *kernelCommandLineSuite) TestComposeCommandLineNotManagedHappy(c *C) {
	model := boottest.MakeMockUC20Model()

	bl := bootloadertest.Mock("btloader", c.MkDir())
	bootloader.Force(bl)
	defer bootloader.Force(nil)

	cmdline, err := boot.ComposeRecoveryCommandLine(model, "20200314")
	c.Assert(err, IsNil)
	c.Assert(cmdline, Equals, "")

	cmdline, err = boot.ComposeCommandLine(model)
	c.Assert(err, IsNil)
	c.Assert(cmdline, Equals, "")

	tbl := bl.WithTrustedAssets()
	bootloader.Force(tbl)

	cmdline, err = boot.ComposeRecoveryCommandLine(model, "20200314")
	c.Assert(err, IsNil)
	c.Assert(cmdline, Equals, "snapd_recovery_mode=recover snapd_recovery_system=20200314")

	cmdline, err = boot.ComposeCommandLine(model)
	c.Assert(err, IsNil)
	c.Assert(cmdline, Equals, "snapd_recovery_mode=run")
}

func (s *kernelCommandLineSuite) TestComposeCommandLineNotUC20(c *C) {
	model := boottest.MakeMockModel()

	bl := bootloadertest.Mock("btloader", c.MkDir())
	bootloader.Force(bl)
	defer bootloader.Force(nil)
	cmdline, err := boot.ComposeRecoveryCommandLine(model, "20200314")
	c.Assert(err, IsNil)
	c.Check(cmdline, Equals, "")

	cmdline, err = boot.ComposeCommandLine(model)
	c.Assert(err, IsNil)
	c.Check(cmdline, Equals, "")
}

func (s *kernelCommandLineSuite) TestComposeCommandLineManagedHappy(c *C) {
	model := boottest.MakeMockUC20Model()

	tbl := bootloadertest.Mock("btloader", c.MkDir()).WithTrustedAssets()
	bootloader.Force(tbl)
	defer bootloader.Force(nil)

	tbl.StaticCommandLine = "panic=-1"

	cmdline, err := boot.ComposeRecoveryCommandLine(model, "20200314")
	c.Assert(err, IsNil)
	c.Assert(cmdline, Equals, "snapd_recovery_mode=recover snapd_recovery_system=20200314 panic=-1")
	cmdline, err = boot.ComposeCommandLine(model)
	c.Assert(err, IsNil)
	c.Assert(cmdline, Equals, "snapd_recovery_mode=run panic=-1")

	cmdline, err = boot.ComposeRecoveryCommandLine(model, "20200314")
	c.Assert(err, IsNil)
	c.Assert(cmdline, Equals, "snapd_recovery_mode=recover snapd_recovery_system=20200314 panic=-1")
	cmdline, err = boot.ComposeCommandLine(model)
	c.Assert(err, IsNil)
	c.Assert(cmdline, Equals, "snapd_recovery_mode=run panic=-1")
}

func (s *kernelCommandLineSuite) TestComposeCandidateCommandLineManagedHappy(c *C) {
	model := boottest.MakeMockUC20Model()

	tbl := bootloadertest.Mock("btloader", c.MkDir()).WithTrustedAssets()
	bootloader.Force(tbl)
	defer bootloader.Force(nil)

	tbl.StaticCommandLine = "panic=-1"
	tbl.CandidateStaticCommandLine = "candidate panic=0"

	cmdline, err := boot.ComposeCandidateCommandLine(model)
	c.Assert(err, IsNil)
	c.Assert(cmdline, Equals, "snapd_recovery_mode=run candidate panic=0")
}

func (s *kernelCommandLineSuite) TestComposeCandidateRecoveryCommandLineManagedHappy(c *C) {
	model := boottest.MakeMockUC20Model()

	tbl := bootloadertest.Mock("btloader", c.MkDir()).WithTrustedAssets()
	bootloader.Force(tbl)
	defer bootloader.Force(nil)

	tbl.StaticCommandLine = "panic=-1"
	tbl.CandidateStaticCommandLine = "candidate panic=0"

	cmdline, err := boot.ComposeCandidateRecoveryCommandLine(model, "1234")
	c.Assert(err, IsNil)
	c.Check(cmdline, Equals, "snapd_recovery_mode=recover snapd_recovery_system=1234 candidate panic=0")

	cmdline, err = boot.ComposeCandidateRecoveryCommandLine(model, "")
	c.Assert(err, ErrorMatches, "internal error: system is unset")
	c.Check(cmdline, Equals, "")
}
