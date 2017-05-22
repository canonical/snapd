// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package cmd_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/cmd"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/testutil"
)

func Test(t *testing.T) { TestingT(t) }

type cmdSuite struct {
	restoreExec   func()
	execCalled    int
	lastExecArgv0 string
	lastExecArgv  []string
	lastExecEnvv  []string
}

var _ = Suite(&cmdSuite{})

func (s *cmdSuite) SetUpTest(c *C) {
	s.restoreExec = cmd.MockExec(s.exec)
	s.execCalled = 0
	s.lastExecArgv0 = ""
	s.lastExecArgv = nil
	s.lastExecEnvv = nil
}

func (s *cmdSuite) TearDownTest(c *C) {
	s.restoreExec()
}

func (s *cmdSuite) exec(argv0 string, argv []string, envv []string) (err error) {
	s.execCalled++
	s.lastExecArgv0 = argv0
	s.lastExecArgv = argv
	s.lastExecEnvv = envv
	return fmt.Errorf(">exec of %q in tests<", argv0)
}

func (s *cmdSuite) fakeCoreVersion(c *C, coreDir, version string) {
	p := filepath.Join(coreDir, "/usr/lib/snapd")
	c.Assert(os.MkdirAll(p, 0755), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(p, "info"), []byte("VERSION="+version), 0644), IsNil)
}

func (s *cmdSuite) fakeInternalTool(c *C, coreDir, toolName string) string {
	s.fakeCoreVersion(c, coreDir, "9999")
	p := filepath.Join(coreDir, "/usr/lib/snapd", toolName)
	c.Assert(ioutil.WriteFile(p, nil, 0755), IsNil)

	return p
}

func (s *cmdSuite) mockReExecingEnv(oldCore, newCore string) func() {
	restore := []func(){
		release.MockOnClassic(true),
		release.MockReleaseInfo(&release.OS{ID: "ubuntu"}),
		cmd.MockCorePaths(oldCore, newCore),
		cmd.MockVersion("2"),
	}

	return func() {
		for i := len(restore) - 1; i >= 0; i-- {
			restore[i]()
		}
	}
}

func (s *cmdSuite) mockReExecFor(c *C, oldCore, newCore, toolName string) func() {
	var coreDir string
	// one and only one core given
	c.Assert(oldCore == "", Not(Equals), newCore == "")
	if oldCore == "" {
		coreDir = newCore
		oldCore = c.MkDir()
	} else {
		coreDir = oldCore
		newCore = c.MkDir()
	}
	selfExe := filepath.Join(coreDir, "self-exe")
	restore := []func(){
		s.mockReExecingEnv(oldCore, newCore),
		cmd.MockSelfExe(selfExe),
	}
	s.fakeInternalTool(c, coreDir, toolName)
	c.Assert(os.Symlink(filepath.Join("/usr/lib/snapd", toolName), selfExe), IsNil)

	return func() {
		for i := len(restore) - 1; i >= 0; i-- {
			restore[i]()
		}
	}
}

func (s *cmdSuite) TestDistroSupportsReExec(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	// Some distributions don't support re-execution yet.
	for _, id := range []string{"fedora", "centos", "rhel", "opensuse", "suse", "poky"} {
		restore = release.MockReleaseInfo(&release.OS{ID: id})
		defer restore()
		c.Check(cmd.DistroSupportsReExec(), Equals, false, Commentf("ID: %q", id))
	}

	// While others do.
	for _, id := range []string{"debian", "ubuntu"} {
		restore = release.MockReleaseInfo(&release.OS{ID: id})
		defer restore()
		c.Check(cmd.DistroSupportsReExec(), Equals, true, Commentf("ID: %q", id))
	}
}

func (s *cmdSuite) TestNonClassicDistroNoSupportsReExec(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	// no distro supports classic when not on classic :-)
	for _, id := range []string{
		"fedora", "centos", "rhel", "opensuse", "suse", "poky",
		"debian", "ubuntu",
	} {
		restore = release.MockReleaseInfo(&release.OS{ID: id})
		defer restore()
		c.Check(cmd.DistroSupportsReExec(), Equals, false, Commentf("ID: %q", id))
	}
}

func (s *cmdSuite) TestCoreSupportsReExecNoInfo(c *C) {
	// there's no snapd/info in a just-created tmpdir :-p
	c.Check(cmd.CoreSupportsReExec(c.MkDir()), Equals, false)
}

func (s *cmdSuite) TestCoreSupportsReExecBadInfo(c *C) {
	// can't read snapd/info if it's a directory
	d := c.MkDir()
	p := d + "/usr/lib/snapd/info"
	c.Assert(os.MkdirAll(p, 0755), IsNil)

	c.Check(cmd.CoreSupportsReExec(d), Equals, false)
}

func (s *cmdSuite) TestCoreSupportsReExecBadInfoContent(c *C) {
	// can't understand snapd/info if all it holds are potatoes
	d := c.MkDir()
	p := d + "/usr/lib/snapd"
	c.Assert(os.MkdirAll(p, 0755), IsNil)
	c.Assert(ioutil.WriteFile(p+"/info", []byte("potatoes"), 0644), IsNil)

	c.Check(cmd.CoreSupportsReExec(d), Equals, false)
}

func (s *cmdSuite) TestCoreSupportsReExecBadVersion(c *C) {
	// can't understand snapd/info if all its version is gibberish
	d := c.MkDir()
	s.fakeCoreVersion(c, d, "0:")

	c.Check(cmd.CoreSupportsReExec(d), Equals, false)
}

func (s *cmdSuite) TestCoreSupportsReExecOldVersion(c *C) {
	// can't re-exec if core version is too old
	defer cmd.MockVersion("2")()
	d := c.MkDir()
	s.fakeCoreVersion(c, d, "0")

	c.Check(cmd.CoreSupportsReExec(d), Equals, false)
}

func (s *cmdSuite) TestCoreSupportsReExec(c *C) {
	defer cmd.MockVersion("2")()
	d := c.MkDir()
	s.fakeCoreVersion(c, d, "9999")

	c.Check(cmd.CoreSupportsReExec(d), Equals, true)
}

func (s *cmdSuite) testInternalToolPathInCore(c *C, coreDir string) {
	defer release.MockOnClassic(true)()
	defer release.MockReleaseInfo(&release.OS{ID: "ubuntu"})()
	defer cmd.MockVersion("2")()

	p := s.fakeInternalTool(c, coreDir, "potato")

	c.Check(cmd.InternalToolPath("potato"), Equals, p)
}

func (s *cmdSuite) TestInternalToolPathInNewCore(c *C) {
	newCore := c.MkDir()
	defer cmd.MockCorePaths(c.MkDir(), newCore)()
	s.testInternalToolPathInCore(c, newCore)
}

func (s *cmdSuite) TestInternalToolPathInOldCore(c *C) {
	oldCore := c.MkDir()
	defer cmd.MockCorePaths(oldCore, c.MkDir())()
	s.testInternalToolPathInCore(c, oldCore)
}

func (s *cmdSuite) TestInternalToolPathInBothPrefersNew(c *C) {
	dNew := c.MkDir()
	dOld := c.MkDir()
	defer s.mockReExecingEnv(dOld, dNew)()

	pNew := s.fakeInternalTool(c, dNew, "potato")
	s.fakeInternalTool(c, dOld, "potato")

	c.Check(cmd.InternalToolPath("potato"), Equals, pNew)
}

func (s *cmdSuite) TestInternalToolPathDisabledByEnv(c *C) {
	// everything ready to get internal
	d := c.MkDir()
	defer s.mockReExecingEnv(c.MkDir(), d)()
	s.fakeInternalTool(c, d, "potato")

	// but, disabled by env
	os.Setenv("SNAP_REEXEC", "0")
	defer os.Unsetenv("SNAP_REEXEC")

	c.Check(cmd.InternalToolPath("potato"), Equals, filepath.Join(dirs.DistroLibExecDir, "potato"))
}

func (s *cmdSuite) TestInternalToolPathDisabledByDistro(c *C) {
	// everything ready to get internal
	d := c.MkDir()
	defer s.mockReExecingEnv(c.MkDir(), d)()
	s.fakeInternalTool(c, d, "potato")

	// but, not on classic
	defer release.MockOnClassic(false)()

	c.Check(cmd.InternalToolPath("potato"), Equals, filepath.Join(dirs.DistroLibExecDir, "potato"))
}

func (s *cmdSuite) TestInternalToolPathInNewCoreDisabledByNoReExecSupportInCore(c *C) {
	// everything ready to get internal
	d := c.MkDir()
	defer s.mockReExecingEnv(c.MkDir(), d)()
	s.fakeInternalTool(c, d, "potato")

	// but, older version
	s.fakeCoreVersion(c, d, "0")

	c.Check(cmd.InternalToolPath("potato"), Equals, filepath.Join(dirs.DistroLibExecDir, "potato"))
}

func (s *cmdSuite) TestExecInCoreSnap(c *C) {
	d := c.MkDir()
	defer s.mockReExecFor(c, "", d, "potato")()

	c.Check(cmd.ExecInCoreSnap, PanicMatches, `>exec of "[^"]+/potato" in tests<`)
	c.Check(s.execCalled, Equals, 1)
	c.Check(s.lastExecArgv0, Equals, filepath.Join(d, "/usr/lib/snapd/potato"))
	c.Check(s.lastExecArgv, DeepEquals, os.Args)
	c.Check(s.lastExecEnvv, testutil.Contains, "SNAP_DID_REEXEC=1")
}

func (s *cmdSuite) TestExecInOldCoreSnap(c *C) {
	d := c.MkDir()
	defer s.mockReExecFor(c, d, "", "potato")()

	c.Check(cmd.ExecInCoreSnap, PanicMatches, `>exec of "[^"]+/potato" in tests<`)
	c.Check(s.execCalled, Equals, 1)
	c.Check(s.lastExecArgv0, Equals, filepath.Join(d, "/usr/lib/snapd/potato"))
	c.Check(s.lastExecArgv, DeepEquals, os.Args)
	c.Check(s.lastExecEnvv, testutil.Contains, "SNAP_DID_REEXEC=1")
}

func (s *cmdSuite) TestExecInCoreSnapBailsNoCoreSupport(c *C) {
	d := c.MkDir()
	defer s.mockReExecFor(c, "", d, "potato")()

	// no "info" -> no core support:
	c.Assert(os.Remove(filepath.Join(d, "/usr/lib/snapd/info")), IsNil)

	cmd.ExecInCoreSnap()
	c.Check(s.execCalled, Equals, 0)
}

func (s *cmdSuite) TestExecInCoreSnapMissingExe(c *C) {
	d := c.MkDir()
	defer s.mockReExecFor(c, "", d, "potato")()

	// missing exe:
	c.Assert(os.Remove(filepath.Join(d, "/usr/lib/snapd/potato")), IsNil)

	cmd.ExecInCoreSnap()
	c.Check(s.execCalled, Equals, 0)
}

func (s *cmdSuite) TestExecInCoreSnapBadSelfExe(c *C) {
	d := c.MkDir()
	defer s.mockReExecFor(c, "", d, "potato")()

	// missing self/exe:
	c.Assert(os.Remove(filepath.Join(d, "self-exe")), IsNil)

	cmd.ExecInCoreSnap()
	c.Check(s.execCalled, Equals, 0)
}

func (s *cmdSuite) TestExecInCoreSnapBailsNoDistroSupport(c *C) {
	d := c.MkDir()
	defer s.mockReExecFor(c, "", d, "potato")()

	// no distro support:
	defer release.MockOnClassic(false)()

	cmd.ExecInCoreSnap()
	c.Check(s.execCalled, Equals, 0)
}

func (s *cmdSuite) TestExecInCoreSnapNoDouble(c *C) {
	d := c.MkDir()
	defer s.mockReExecFor(c, "", d, "potato")()

	os.Setenv("SNAP_DID_REEXEC", "1")
	defer os.Unsetenv("SNAP_DID_REEXEC")

	cmd.ExecInCoreSnap()
	c.Check(s.execCalled, Equals, 0)
}

func (s *cmdSuite) TestExecInCoreSnapDisabled(c *C) {
	d := c.MkDir()
	defer s.mockReExecFor(c, "", d, "potato")()

	os.Setenv("SNAP_REEXEC", "0")
	defer os.Unsetenv("SNAP_REEXEC")

	cmd.ExecInCoreSnap()
	c.Check(s.execCalled, Equals, 0)
}
