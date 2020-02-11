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
	"io/ioutil"
	"os"
	"path/filepath"

	"testing"

	"github.com/jessevdk/go-flags"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/cmd/snap-preseed"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/timings"
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
	c.Check(main.Run(parser, nil), ErrorMatches, `need chroot path as argument`)
}

func (s *startPreseedSuite) TestChrootDoesntExist(c *C) {
	restore := main.MockOsGetuid(func() int { return 0 })
	defer restore()

	parser := testParser(c)
	c.Check(main.Run(parser, []string{"/non-existing-dir"}), ErrorMatches, `cannot verify "/non-existing-dir": is not a directory`)
}

func (s *startPreseedSuite) TestChrootValidationUnhappy(c *C) {
	restore := main.MockOsGetuid(func() int { return 0 })
	defer restore()

	tmpDir := c.MkDir()

	parser := testParser(c)
	c.Check(main.Run(parser, []string{tmpDir}), ErrorMatches, `cannot pre-seed without access to ".*sys/kernel/security/apparmor"`)
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

func (s *startPreseedSuite) TestRunPreseedHappy(c *C) {
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
	restoreMountPath := main.MockSnapdMountPath(targetSnapdRoot)
	defer restoreMountPath()

	restoreSystemSnapFromSeed := main.MockSystemSnapFromSeed(func(string) (string, error) { return "/a/core.snap", nil })
	defer restoreSystemSnapFromSeed()

	c.Assert(os.MkdirAll(filepath.Join(targetSnapdRoot, "usr/lib/snapd/"), 0755), IsNil)
	mockTargetSnapd := testutil.MockCommand(c, filepath.Join(targetSnapdRoot, "usr/lib/snapd/snapd"), `#!/bin/sh
	if [ "$SNAPD_PRESEED" != "1" ]; then
		exit 1
	fi
`)
	defer mockTargetSnapd.Restore()

	infoFile := filepath.Join(filepath.Join(targetSnapdRoot, dirs.CoreLibExecDir, "info"))
	c.Assert(ioutil.WriteFile(infoFile, []byte("VERSION=2.44.0"), 0644), IsNil)

	parser := testParser(c)
	c.Check(main.Run(parser, []string{tmpDir}), IsNil)

	c.Assert(mockMountCmd.Calls(), HasLen, 1)
	// note, tmpDir, targetSnapdRoot are contactenated again cause we're not really chrooting in the test
	// and mocking dirs.RootDir
	c.Check(mockMountCmd.Calls()[0], DeepEquals, []string{"mount", "-t", "squashfs", "/a/core.snap", filepath.Join(tmpDir, targetSnapdRoot)})

	c.Assert(mockTargetSnapd.Calls(), HasLen, 1)
	c.Check(mockTargetSnapd.Calls()[0], DeepEquals, []string{"snapd"})
}

type Fake16Seed struct {
	Essential         []*seed.Snap
	LoadMetaErr       error
	LoadAssertionsErr error
	UsesSnapd         bool
}

// Fake implementation of seed.Seed interface

func (fs *Fake16Seed) LoadAssertions(db asserts.RODatabase, commitTo func(*asserts.Batch) error) error {
	return fs.LoadAssertionsErr
}

func (fs *Fake16Seed) Model() (*asserts.Model, error) {
	panic("not implemented")
}

func (fs *Fake16Seed) LoadMeta(tm timings.Measurer) error {
	return fs.LoadMetaErr
}

func (fs *Fake16Seed) UsesSnapdSnap() bool {
	return fs.UsesSnapd
}

func (fs *Fake16Seed) EssentialSnaps() []*seed.Snap {
	return fs.Essential
}

func (fs *Fake16Seed) ModeSnaps(mode string) ([]*seed.Snap, error) {
	return nil, nil
}

func (s *startPreseedSuite) TestSystemSnapFromSeed(c *C) {
	tmpDir := c.MkDir()

	restore := main.MockSeedOpen(func(rootDir, label string) (seed.Seed, error) {
		return &Fake16Seed{
			Essential: []*seed.Snap{{Path: "/some/path/core", SideInfo: &snap.SideInfo{RealName: "core"}}},
		}, nil
	})
	defer restore()

	path, err := main.SystemSnapFromSeed(tmpDir)
	c.Assert(err, IsNil)
	c.Check(path, Equals, "/some/path/core")
}

func (s *startPreseedSuite) TestSystemSnapFromSeedOpenError(c *C) {
	tmpDir := c.MkDir()

	restore := main.MockSeedOpen(func(rootDir, label string) (seed.Seed, error) { return nil, fmt.Errorf("fail") })
	defer restore()

	_, err := main.SystemSnapFromSeed(tmpDir)
	c.Assert(err, ErrorMatches, "fail")
}

func (s *startPreseedSuite) TestSystemSnapFromSeedErrors(c *C) {
	tmpDir := c.MkDir()

	fakeSeed := &Fake16Seed{}

	restore := main.MockSeedOpen(func(rootDir, label string) (seed.Seed, error) { return fakeSeed, nil })
	defer restore()

	fakeSeed.Essential = []*seed.Snap{{Path: "", SideInfo: &snap.SideInfo{RealName: "core"}}}
	_, err := main.SystemSnapFromSeed(tmpDir)
	c.Assert(err, ErrorMatches, "core snap not found")

	fakeSeed.Essential = []*seed.Snap{{Path: "/some/path", SideInfo: &snap.SideInfo{RealName: "foosnap"}}}
	_, err = main.SystemSnapFromSeed(tmpDir)
	c.Assert(err, ErrorMatches, "core snap not found")

	fakeSeed.UsesSnapd = true
	_, err = main.SystemSnapFromSeed(tmpDir)
	c.Assert(err, ErrorMatches, "preseeding with snapd snap is not supported yet")

	fakeSeed.LoadMetaErr = fmt.Errorf("load meta failed")
	_, err = main.SystemSnapFromSeed(tmpDir)
	c.Assert(err, ErrorMatches, "load meta failed")

	fakeSeed.LoadMetaErr = nil
	fakeSeed.LoadAssertionsErr = fmt.Errorf("load assertions failed")
	_, err = main.SystemSnapFromSeed(tmpDir)
	c.Assert(err, ErrorMatches, "load assertions failed")
}

func (s *startPreseedSuite) TestRunPreseedUnsupportedVersion(c *C) {
	tmpDir := c.MkDir()
	dirs.SetRootDir(tmpDir)
	mockChrootDirs(c, tmpDir)

	restoreOsGuid := main.MockOsGetuid(func() int { return 0 })
	defer restoreOsGuid()

	restoreSyscallChroot := main.MockSyscallChroot(func(path string) error { return nil })
	defer restoreSyscallChroot()

	mockMountCmd := testutil.MockCommand(c, "mount", "")
	defer mockMountCmd.Restore()

	targetSnapdRoot := filepath.Join(tmpDir, "target-core-mounted-here")
	restoreMountPath := main.MockSnapdMountPath(targetSnapdRoot)
	defer restoreMountPath()

	restoreSystemSnapFromSeed := main.MockSystemSnapFromSeed(func(string) (string, error) { return "/a/core.snap", nil })
	defer restoreSystemSnapFromSeed()

	c.Assert(os.MkdirAll(filepath.Join(targetSnapdRoot, "usr/lib/snapd/"), 0755), IsNil)
	mockTargetSnapd := testutil.MockCommand(c, filepath.Join(targetSnapdRoot, "usr/lib/snapd/snapd"), "")
	defer mockTargetSnapd.Restore()

	infoFile := filepath.Join(filepath.Join(targetSnapdRoot, dirs.CoreLibExecDir, "info"))
	c.Assert(ioutil.WriteFile(infoFile, []byte("VERSION=2.43.0"), 0644), IsNil)

	parser := testParser(c)
	c.Check(main.Run(parser, []string{tmpDir}), ErrorMatches,
		`snapd 2.43.0 from the target system does not support preseeding, the minimum required version is 2.44.0`)
}
