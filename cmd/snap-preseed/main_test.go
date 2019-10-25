// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
	"path/filepath"
	"testing"

	"github.com/jessevdk/go-flags"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/cmd/snap-preseed"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/testutil"
)

func Test(t *testing.T) { TestingT(t) }

var _ = Suite(&startPreseedSuite{})

type startPreseedSuite struct {
	testutil.BaseTest
}

func (s *startPreseedSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
}

func (s *startPreseedSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
	dirs.SetRootDir("")
}

func testParser(c *C) *flags.Parser {
	parser := main.Parser()
	_, err := parser.ParseArgs([]string{})
	c.Assert(err, IsNil)
	return parser
}

func mockChrootDirs(c *C, rootDir string) {
	c.Assert(os.MkdirAll(filepath.Join(rootDir, "/sys/kernel/security/apparmor"), 0755), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(rootDir, "/proc/self"), 0755), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(rootDir, "/dev/mem"), 0755), IsNil)
}

func (s *startPreseedSuite) TestRequiresRoot(c *C) {
	restore := main.MockOsGetuid(func() int {
		return 1000
	})
	defer restore()

	parser := testParser(c)
	c.Check(main.Run(parser, []string{"/"}), ErrorMatches, `must be run as root`)
}

func (s *startPreseedSuite) TestMissingArg(c *C) {
	restore := main.MockOsGetuid(func() int {
		return 0
	})
	defer restore()

	parser := testParser(c)
	c.Check(main.Run(parser, nil), ErrorMatches, `need the chroot path as argument`)
}

func (s *startPreseedSuite) TestChrootDoesntExist(c *C) {
	restore := main.MockOsGetuid(func() int { return 0 })
	defer restore()

	parser := testParser(c)
	c.Check(main.Run(parser, []string{"/non-existing-dir"}), ErrorMatches, `target chroot directory /non-existing-dir doesn't exist or is not a directory`)
}

func (s *startPreseedSuite) TestChrootValidationUnhappy(c *C) {
	restore := main.MockOsGetuid(func() int { return 0 })
	defer restore()

	tmpDir := c.MkDir()

	parser := testParser(c)
	c.Check(main.Run(parser, []string{tmpDir}), ErrorMatches, "target chroot directory validation error: .*/sys/kernel/security/apparmor doesn't exist")
}

func (s *startPreseedSuite) TestChrootFailure(c *C) {
	restoreOsGuid := main.MockOsGetuid(func() int { return 0 })
	defer restoreOsGuid()

	restoreSyscallChroot := main.MockSyscallChroot(func(path string) error {
		return fmt.Errorf("FAIL: %s", path)
	})
	defer restoreSyscallChroot()

	tmpDir := c.MkDir()
	mockChrootDirs(c, tmpDir)

	parser := testParser(c)
	c.Check(main.Run(parser, []string{tmpDir}), ErrorMatches, fmt.Sprintf("cannot chroot into %s: FAIL: %s", tmpDir, tmpDir))
}

func (s *startPreseedSuite) TestStartPrebakeHappy(c *C) {
	tmpDir := c.MkDir()
	dirs.SetRootDir(tmpDir)
	mockChrootDirs(c, tmpDir)

	restoreOsGuid := main.MockOsGetuid(func() int { return 0 })
	defer restoreOsGuid()

	restoreSyscallChroot := main.MockSyscallChroot(func(path string) error { return nil })
	defer restoreSyscallChroot()

	mockMountCmd := testutil.MockCommand(c, "mount", "")
	defer mockMountCmd.Restore()

	mockUmountCmd := testutil.MockCommand(c, "umount", "")
	defer mockUmountCmd.Restore()

	targetSnapdRoot := filepath.Join(tmpDir, "target-core-mounted-here")
	restoreMountPath := main.MockMountPath(targetSnapdRoot)
	defer restoreMountPath()

	restoreSystemSnapFromSeeds := main.MockSystemSnapFromSeeds(func(string) (string, error) { return "/a/core.snap", nil })
	defer restoreSystemSnapFromSeeds()

	c.Assert(os.MkdirAll(filepath.Join(targetSnapdRoot, "usr/lib/snapd/"), 0755), IsNil)
	mockTargetSnapd := testutil.MockCommand(c, filepath.Join(targetSnapdRoot, "usr/lib/snapd/snapd"), `#!/bin/sh
	# the expression below ensures SNAPD_PRESEED env var is set
	exit "$(( 1 - "$SNAPD_PRESEED" ))"
`)
	defer mockTargetSnapd.Restore()

	parser := testParser(c)
	c.Check(main.Run(parser, []string{tmpDir}), IsNil)

	c.Assert(mockMountCmd.Calls(), HasLen, 1)
	// note, tmpDir, targetSnapdRoot are contactenated again cause we're not really chrooting in the test
	// and mocking dirs.RootDir
	c.Check(mockMountCmd.Calls()[0], DeepEquals, []string{"mount", "-t", "squashfs", "/a/core.snap", filepath.Join(tmpDir, targetSnapdRoot)})

	c.Assert(mockTargetSnapd.Calls(), HasLen, 1)
	c.Check(mockTargetSnapd.Calls()[0], DeepEquals, []string{"snapd"})
}
