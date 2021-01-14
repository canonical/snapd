// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019-2020 Canonical Ltd
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
	"strings"

	"testing"

	"github.com/jessevdk/go-flags"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/cmd/snap-preseed"
	"github.com/snapcore/snapd/cmd/snaplock/runinhibit"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/squashfs"
	apparmor_sandbox "github.com/snapcore/snapd/sandbox/apparmor"
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
	restore := squashfs.MockNeedsFuse(false)
	s.BaseTest.AddCleanup(restore)
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

func mockVersionFiles(c *C, rootDir1, version1, rootDir2, version2 string) {
	versions := []string{version1, version2}
	for i, root := range []string{rootDir1, rootDir2} {
		c.Assert(os.MkdirAll(filepath.Join(root, dirs.CoreLibExecDir), 0755), IsNil)
		infoFile := filepath.Join(root, dirs.CoreLibExecDir, "info")
		c.Assert(ioutil.WriteFile(infoFile, []byte(fmt.Sprintf("VERSION=%s", versions[i])), 0644), IsNil)
	}
}

func mockChrootDirs(c *C, rootDir string, apparmorDir bool) func() {
	if apparmorDir {
		c.Assert(os.MkdirAll(filepath.Join(rootDir, "/sys/kernel/security/apparmor"), 0755), IsNil)
	}
	mockMountInfo := `912 920 0:57 / ${rootDir}/proc rw,nosuid,nodev,noexec,relatime - proc proc rw
914 913 0:7 / ${rootDir}/sys/kernel/security rw,nosuid,nodev,noexec,relatime master:8 - securityfs securityfs rw
915 920 0:58 / ${rootDir}/dev rw,relatime - tmpfs none rw,size=492k,mode=755,uid=100000,gid=100000
`
	return osutil.MockMountInfo(strings.Replace(mockMountInfo, "${rootDir}", rootDir, -1))
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
	defer osutil.MockMountInfo("")()

	parser := testParser(c)
	c.Check(main.Run(parser, []string{tmpDir}), ErrorMatches, "cannot preseed without the following mountpoints:\n - .*/dev\n - .*/proc\n - .*/sys/kernel/security")
}

func (s *startPreseedSuite) TestChrootValidationUnhappyNoApparmor(c *C) {
	restore := main.MockOsGetuid(func() int { return 0 })
	defer restore()

	tmpDir := c.MkDir()
	defer mockChrootDirs(c, tmpDir, false)()

	parser := testParser(c)
	c.Check(main.Run(parser, []string{tmpDir}), ErrorMatches, `cannot preseed without access to ".*sys/kernel/security/apparmor"`)
}

func (s *startPreseedSuite) TestChrootValidationAlreadyPreseeded(c *C) {
	restore := main.MockOsGetuid(func() int { return 0 })
	defer restore()

	tmpDir := c.MkDir()
	snapdDir := filepath.Dir(dirs.SnapStateFile)
	c.Assert(os.MkdirAll(filepath.Join(tmpDir, snapdDir), 0755), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(tmpDir, dirs.SnapStateFile), nil, os.ModePerm), IsNil)

	parser := testParser(c)
	c.Check(main.Run(parser, []string{tmpDir}), ErrorMatches, fmt.Sprintf("the system at %q appears to be preseeded, pass --reset flag to clean it up", tmpDir))
}

func (s *startPreseedSuite) TestChrootFailure(c *C) {
	restoreOsGuid := main.MockOsGetuid(func() int { return 0 })
	defer restoreOsGuid()

	restoreSyscallChroot := main.MockSyscallChroot(func(path string) error {
		return fmt.Errorf("FAIL: %s", path)
	})
	defer restoreSyscallChroot()

	tmpDir := c.MkDir()
	defer mockChrootDirs(c, tmpDir, true)()

	parser := testParser(c)
	c.Check(main.Run(parser, []string{tmpDir}), ErrorMatches, fmt.Sprintf("cannot chroot into %s: FAIL: %s", tmpDir, tmpDir))
}

func (s *startPreseedSuite) TestRunPreseedHappy(c *C) {
	tmpDir := c.MkDir()
	dirs.SetRootDir(tmpDir)
	defer mockChrootDirs(c, tmpDir, true)()

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

	mockTargetSnapd := testutil.MockCommand(c, filepath.Join(targetSnapdRoot, "usr/lib/snapd/snapd"), `#!/bin/sh
	if [ "$SNAPD_PRESEED" != "1" ]; then
		exit 1
	fi
`)
	defer mockTargetSnapd.Restore()

	mockSnapdFromDeb := testutil.MockCommand(c, filepath.Join(tmpDir, "usr/lib/snapd/snapd"), `#!/bin/sh
	exit 1
`)
	defer mockSnapdFromDeb.Restore()

	// snapd from the snap is newer than deb
	mockVersionFiles(c, targetSnapdRoot, "2.44.0", tmpDir, "2.41.0")

	parser := testParser(c)
	c.Check(main.Run(parser, []string{tmpDir}), IsNil)

	c.Assert(mockMountCmd.Calls(), HasLen, 1)
	// note, tmpDir, targetSnapdRoot are contactenated again cause we're not really chrooting in the test
	// and mocking dirs.RootDir
	c.Check(mockMountCmd.Calls()[0], DeepEquals, []string{"mount", "-t", "squashfs", "-o", "ro,x-gdu.hide", "/a/core.snap", filepath.Join(tmpDir, targetSnapdRoot)})

	c.Assert(mockTargetSnapd.Calls(), HasLen, 1)
	c.Check(mockTargetSnapd.Calls()[0], DeepEquals, []string{"snapd"})

	c.Assert(mockSnapdFromDeb.Calls(), HasLen, 0)

	// relative chroot path works too
	tmpDirPath, relativeChroot := filepath.Split(tmpDir)
	pwd, err := os.Getwd()
	c.Assert(err, IsNil)
	defer func() {
		os.Chdir(pwd)
	}()
	c.Assert(os.Chdir(tmpDirPath), IsNil)
	c.Check(main.Run(parser, []string{relativeChroot}), IsNil)
}

func (s *startPreseedSuite) TestRunPreseedHappyDebVersionIsNewer(c *C) {
	tmpDir := c.MkDir()
	dirs.SetRootDir(tmpDir)
	defer mockChrootDirs(c, tmpDir, true)()

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
	mockSnapdFromSnap := testutil.MockCommand(c, filepath.Join(targetSnapdRoot, "usr/lib/snapd/snapd"), `#!/bin/sh
	exit 1
`)
	defer mockSnapdFromSnap.Restore()

	mockSnapdFromDeb := testutil.MockCommand(c, filepath.Join(tmpDir, "usr/lib/snapd/snapd"), `#!/bin/sh
	if [ "$SNAPD_PRESEED" != "1" ]; then
		exit 1
	fi
`)
	defer mockSnapdFromDeb.Restore()

	// snapd from the deb is newer than snap
	mockVersionFiles(c, targetSnapdRoot, "2.44.0", tmpDir, "2.45.0")

	parser := testParser(c)
	c.Check(main.Run(parser, []string{tmpDir}), IsNil)

	c.Assert(mockMountCmd.Calls(), HasLen, 1)
	// note, tmpDir, targetSnapdRoot are contactenated again cause we're not really chrooting in the test
	// and mocking dirs.RootDir
	c.Check(mockMountCmd.Calls()[0], DeepEquals, []string{"mount", "-t", "squashfs", "-o", "ro,x-gdu.hide", "/a/core.snap", filepath.Join(tmpDir, targetSnapdRoot)})

	c.Assert(mockSnapdFromDeb.Calls(), HasLen, 1)
	c.Check(mockSnapdFromDeb.Calls()[0], DeepEquals, []string{"snapd"})
	c.Assert(mockSnapdFromSnap.Calls(), HasLen, 0)
}

type Fake16Seed struct {
	AssertsModel      *asserts.Model
	Essential         []*seed.Snap
	LoadMetaErr       error
	LoadAssertionsErr error
	UsesSnapd         bool
}

// Fake implementation of seed.Seed interface

func mockClassicModel() *asserts.Model {
	headers := map[string]interface{}{
		"type":         "model",
		"authority-id": "brand",
		"series":       "16",
		"brand-id":     "brand",
		"model":        "classicbaz-3000",
		"classic":      "true",
		"timestamp":    "2018-01-01T08:00:00+00:00",
	}
	return assertstest.FakeAssertion(headers, nil).(*asserts.Model)
}

func (fs *Fake16Seed) LoadAssertions(db asserts.RODatabase, commitTo func(*asserts.Batch) error) error {
	return fs.LoadAssertionsErr
}

func (fs *Fake16Seed) Model() *asserts.Model {
	return fs.AssertsModel
}

func (fs *Fake16Seed) Brand() (*asserts.Account, error) {
	headers := map[string]interface{}{
		"type":         "account",
		"account-id":   "brand",
		"display-name": "fake brand",
		"username":     "brand",
		"timestamp":    "2018-01-01T08:00:00+00:00",
	}
	return assertstest.FakeAssertion(headers, nil).(*asserts.Account), nil
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
			AssertsModel: mockClassicModel(),
			Essential:    []*seed.Snap{{Path: "/some/path/core", SideInfo: &snap.SideInfo{RealName: "core"}}},
		}, nil
	})
	defer restore()

	path, err := main.SystemSnapFromSeed(tmpDir)
	c.Assert(err, IsNil)
	c.Check(path, Equals, "/some/path/core")
}

func (s *startPreseedSuite) TestSystemSnapFromSnapdSeed(c *C) {
	tmpDir := c.MkDir()

	restore := main.MockSeedOpen(func(rootDir, label string) (seed.Seed, error) {
		return &Fake16Seed{
			AssertsModel: mockClassicModel(),
			Essential:    []*seed.Snap{{Path: "/some/path/snapd.snap", SideInfo: &snap.SideInfo{RealName: "snapd"}}},
			UsesSnapd:    true,
		}, nil
	})
	defer restore()

	path, err := main.SystemSnapFromSeed(tmpDir)
	c.Assert(err, IsNil)
	c.Check(path, Equals, "/some/path/snapd.snap")
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
	fakeSeed.AssertsModel = mockClassicModel()

	restore := main.MockSeedOpen(func(rootDir, label string) (seed.Seed, error) { return fakeSeed, nil })
	defer restore()

	fakeSeed.Essential = []*seed.Snap{{Path: "", SideInfo: &snap.SideInfo{RealName: "core"}}}
	_, err := main.SystemSnapFromSeed(tmpDir)
	c.Assert(err, ErrorMatches, "core snap not found")

	fakeSeed.Essential = []*seed.Snap{{Path: "/some/path", SideInfo: &snap.SideInfo{RealName: "foosnap"}}}
	_, err = main.SystemSnapFromSeed(tmpDir)
	c.Assert(err, ErrorMatches, "core snap not found")

	fakeSeed.LoadMetaErr = fmt.Errorf("load meta failed")
	_, err = main.SystemSnapFromSeed(tmpDir)
	c.Assert(err, ErrorMatches, "load meta failed")

	fakeSeed.LoadMetaErr = nil
	fakeSeed.LoadAssertionsErr = fmt.Errorf("load assertions failed")
	_, err = main.SystemSnapFromSeed(tmpDir)
	c.Assert(err, ErrorMatches, "load assertions failed")
}

func (s *startPreseedSuite) TestClassicRequired(c *C) {
	tmpDir := c.MkDir()

	headers := map[string]interface{}{
		"type":         "model",
		"authority-id": "brand",
		"series":       "16",
		"brand-id":     "brand",
		"model":        "baz-3000",
		"architecture": "armhf",
		"gadget":       "brand-gadget",
		"kernel":       "kernel",
		"timestamp":    "2018-01-01T08:00:00+00:00",
	}

	fakeSeed := &Fake16Seed{}
	fakeSeed.AssertsModel = assertstest.FakeAssertion(headers, nil).(*asserts.Model)

	restore := main.MockSeedOpen(func(rootDir, label string) (seed.Seed, error) { return fakeSeed, nil })
	defer restore()

	_, err := main.SystemSnapFromSeed(tmpDir)
	c.Assert(err, ErrorMatches, "preseeding is only supported on classic systems")
}

func (s *startPreseedSuite) TestRunPreseedUnsupportedVersion(c *C) {
	tmpDir := c.MkDir()
	dirs.SetRootDir(tmpDir)
	c.Assert(os.MkdirAll(filepath.Join(tmpDir, "usr/lib/snapd/"), 0755), IsNil)
	defer mockChrootDirs(c, tmpDir, true)()

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

	infoFile := filepath.Join(targetSnapdRoot, dirs.CoreLibExecDir, "info")
	c.Assert(ioutil.WriteFile(infoFile, []byte("VERSION=2.43.0"), 0644), IsNil)

	// simulate snapd version from the deb
	infoFile = filepath.Join(filepath.Join(tmpDir, dirs.CoreLibExecDir, "info"))
	c.Assert(ioutil.WriteFile(infoFile, []byte("VERSION=2.41.0"), 0644), IsNil)

	parser := testParser(c)
	c.Check(main.Run(parser, []string{tmpDir}), ErrorMatches,
		`snapd 2.43.0 from the target system does not support preseeding, the minimum required version is 2.43.3\+`)
}

func (s *startPreseedSuite) TestChooseTargetSnapdVersion(c *C) {
	tmpDir := c.MkDir()
	dirs.SetRootDir(tmpDir)
	c.Assert(os.MkdirAll(filepath.Join(tmpDir, "usr/lib/snapd/"), 0755), IsNil)

	targetSnapdRoot := filepath.Join(tmpDir, "target-core-mounted-here")
	c.Assert(os.MkdirAll(filepath.Join(targetSnapdRoot, "usr/lib/snapd/"), 0755), IsNil)
	restoreMountPath := main.MockSnapdMountPath(targetSnapdRoot)
	defer restoreMountPath()

	var versions = []struct {
		fromSnap        string
		fromDeb         string
		expectedPath    string
		expectedVersion string
		expectedErr     string
	}{
		{
			fromDeb:  "2.44.0",
			fromSnap: "2.45.3+git123",
			// snap version wins
			expectedVersion: "2.45.3+git123",
			expectedPath:    filepath.Join(tmpDir, "target-core-mounted-here/usr/lib/snapd/snapd"),
		},
		{
			fromDeb:  "2.44.0",
			fromSnap: "2.44.0",
			// snap version wins
			expectedVersion: "2.44.0",
			expectedPath:    filepath.Join(tmpDir, "target-core-mounted-here/usr/lib/snapd/snapd"),
		},
		{
			fromDeb:  "2.45.1+20.04",
			fromSnap: "2.45.1",
			// deb version wins
			expectedVersion: "2.45.1+20.04",
			expectedPath:    filepath.Join(tmpDir, "usr/lib/snapd/snapd"),
		},
		{
			fromDeb:  "2.45.2",
			fromSnap: "2.45.1",
			// deb version wins
			expectedVersion: "2.45.2",
			expectedPath:    filepath.Join(tmpDir, "usr/lib/snapd/snapd"),
		},
		{
			fromSnap:    "2.45.1",
			expectedErr: fmt.Sprintf("cannot open snapd info file %q.*", filepath.Join(tmpDir, "usr/lib/snapd/info")),
		},
		{
			fromDeb:     "2.45.1",
			expectedErr: fmt.Sprintf("cannot open snapd info file %q.*", filepath.Join(tmpDir, "target-core-mounted-here/usr/lib/snapd/info")),
		},
	}

	for _, test := range versions {
		infoFile := filepath.Join(tmpDir, "usr/lib/snapd/info")
		os.Remove(infoFile)
		if test.fromDeb != "" {
			c.Assert(ioutil.WriteFile(infoFile, []byte(fmt.Sprintf("VERSION=%s", test.fromDeb)), 0644), IsNil)
		}
		infoFile = filepath.Join(targetSnapdRoot, "usr/lib/snapd/info")
		os.Remove(infoFile)
		if test.fromSnap != "" {
			c.Assert(ioutil.WriteFile(infoFile, []byte(fmt.Sprintf("VERSION=%s", test.fromSnap)), 0644), IsNil)
		}

		targetSnapd, err := main.ChooseTargetSnapdVersion()
		if test.expectedErr != "" {
			c.Assert(err, ErrorMatches, test.expectedErr)
		} else {
			c.Assert(err, IsNil)
			c.Assert(targetSnapd, NotNil)
			path, version := main.SnapdPathAndVersion(targetSnapd)
			c.Check(path, Equals, test.expectedPath)
			c.Check(version, Equals, test.expectedVersion)
		}
	}
}

func (s *startPreseedSuite) TestRunPreseedAgainstFilesystemRoot(c *C) {
	restore := main.MockOsGetuid(func() int { return 0 })
	defer restore()

	parser := testParser(c)
	c.Assert(main.Run(parser, []string{"/"}), ErrorMatches, `cannot run snap-preseed against /`)
}

func (s *startPreseedSuite) TestReset(c *C) {
	restore := main.MockOsGetuid(func() int { return 0 })
	defer restore()

	startDir, err := os.Getwd()
	c.Assert(err, IsNil)
	defer func() {
		os.Chdir(startDir)
	}()

	for _, isRelative := range []bool{false, true} {
		tmpDir := c.MkDir()
		resetDirArg := tmpDir
		if isRelative {
			var parentDir string
			parentDir, resetDirArg = filepath.Split(tmpDir)
			os.Chdir(parentDir)
		}

		// mock some preseeding artifacts
		artifacts := []struct {
			path string
			// if symlinkTarget is not empty, then a path -> symlinkTarget symlink
			// will be created instead of a regular file.
			symlinkTarget string
		}{
			{dirs.SnapStateFile, ""},
			{dirs.SnapSystemKeyFile, ""},
			{filepath.Join(dirs.SnapDesktopFilesDir, "foo.desktop"), ""},
			{filepath.Join(dirs.SnapDesktopIconsDir, "foo.png"), ""},
			{filepath.Join(dirs.SnapMountPolicyDir, "foo.fstab"), ""},
			{filepath.Join(dirs.SnapBlobDir, "foo.snap"), ""},
			{filepath.Join(dirs.SnapUdevRulesDir, "foo-snap.bar.rules"), ""},
			{filepath.Join(dirs.SnapDBusSystemPolicyDir, "snap.foo.bar.conf"), ""},
			{filepath.Join(dirs.SnapDBusSessionServicesDir, "org.example.Session.service"), ""},
			{filepath.Join(dirs.SnapDBusSystemServicesDir, "org.example.System.service"), ""},
			{filepath.Join(dirs.SnapServicesDir, "snap.foo.service"), ""},
			{filepath.Join(dirs.SnapServicesDir, "snap.foo.timer"), ""},
			{filepath.Join(dirs.SnapServicesDir, "snap.foo.socket"), ""},
			{filepath.Join(dirs.SnapServicesDir, "snap-foo.mount"), ""},
			{filepath.Join(dirs.SnapServicesDir, "multi-user.target.wants", "snap-foo.mount"), ""},
			{filepath.Join(dirs.SnapDataDir, "foo", "bar"), ""},
			{filepath.Join(dirs.SnapCacheDir, "foocache", "bar"), ""},
			{filepath.Join(apparmor_sandbox.CacheDir, "foo", "bar"), ""},
			{filepath.Join(dirs.SnapAppArmorDir, "foo"), ""},
			{filepath.Join(dirs.SnapAssertsDBDir, "foo"), ""},
			{filepath.Join(dirs.FeaturesDir, "foo"), ""},
			{filepath.Join(dirs.SnapDeviceDir, "foo-1", "bar"), ""},
			{filepath.Join(dirs.SnapCookieDir, "foo"), ""},
			{filepath.Join(dirs.SnapSeqDir, "foo.json"), ""},
			{filepath.Join(dirs.SnapMountDir, "foo", "bin"), ""},
			{filepath.Join(dirs.SnapSeccompDir, "foo.bin"), ""},
			{filepath.Join(runinhibit.InhibitDir, "foo.lock"), ""},
			// bash-completion symlinks
			{filepath.Join(dirs.CompletersDir, "foo.bar"), "/a/snapd/complete.sh"},
			{filepath.Join(dirs.CompletersDir, "foo"), "foo.bar"},
		}

		for _, art := range artifacts {
			fullPath := filepath.Join(tmpDir, art.path)
			// create parent dir
			c.Assert(os.MkdirAll(filepath.Dir(fullPath), 0755), IsNil)
			if art.symlinkTarget != "" {
				// note, symlinkTarget is not relative to tmpDir
				c.Assert(os.Symlink(art.symlinkTarget, fullPath), IsNil)
			} else {
				c.Assert(ioutil.WriteFile(fullPath, nil, os.ModePerm), IsNil)
			}
		}

		checkArtifacts := func(exists bool) {
			for _, art := range artifacts {
				fullPath := filepath.Join(tmpDir, art.path)
				if art.symlinkTarget != "" {
					c.Check(osutil.IsSymlink(fullPath), Equals, exists, Commentf("offending symlink: %s", fullPath))
				} else {
					c.Check(osutil.FileExists(fullPath), Equals, exists, Commentf("offending file: %s", fullPath))
				}
			}
		}

		// sanity
		checkArtifacts(true)

		snapdDir := filepath.Dir(dirs.SnapStateFile)
		c.Assert(os.MkdirAll(filepath.Join(tmpDir, snapdDir), 0755), IsNil)
		c.Assert(ioutil.WriteFile(filepath.Join(tmpDir, dirs.SnapStateFile), nil, os.ModePerm), IsNil)

		parser := testParser(c)
		c.Assert(main.Run(parser, []string{"--reset", resetDirArg}), IsNil)

		checkArtifacts(false)

		// running reset again is ok
		parser = testParser(c)
		c.Assert(main.Run(parser, []string{"--reset", resetDirArg}), IsNil)

		// reset complains if target directory doesn't exist
		c.Assert(main.Run(parser, []string{"--reset", "/non/existing/chrootpath"}), ErrorMatches, `cannot reset non-existing directory "/non/existing/chrootpath"`)

		// reset complains if target is not a directory
		dummyFile := filepath.Join(resetDirArg, "foo")
		c.Assert(ioutil.WriteFile(dummyFile, nil, os.ModePerm), IsNil)
		err = main.Run(parser, []string{"--reset", dummyFile})
		// the error message is always with an absolute file, so make the path
		// absolute if we are running the relative test to properly match
		if isRelative {
			var err2 error
			dummyFile, err2 = filepath.Abs(dummyFile)
			c.Assert(err2, IsNil)
		}
		c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot reset %q, it is not a directory`, dummyFile))
	}

}
