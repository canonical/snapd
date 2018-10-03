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
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/release"
)

func Test(t *testing.T) { TestingT(t) }

type cmdSuite struct {
	restoreExec   func()
	restoreLogger func()
	execCalled    int
	lastExecArgv0 string
	lastExecArgv  []string
	lastExecEnvv  []string
	fakeroot      string
	snapdPath     string
	corePath      string
}

var _ = Suite(&cmdSuite{})

func (s *cmdSuite) SetUpTest(c *C) {
	s.restoreExec = cmd.MockSyscallExec(s.syscallExec)
	_, s.restoreLogger = logger.MockLogger()
	s.execCalled = 0
	s.lastExecArgv0 = ""
	s.lastExecArgv = nil
	s.lastExecEnvv = nil
	s.fakeroot = c.MkDir()
	dirs.SetRootDir(s.fakeroot)
	s.snapdPath = filepath.Join(dirs.SnapMountDir, "/snapd/42")
	s.corePath = filepath.Join(dirs.SnapMountDir, "/core/21")
	c.Assert(os.MkdirAll(filepath.Join(s.fakeroot, "proc/self"), 0755), IsNil)
}

func (s *cmdSuite) TearDownTest(c *C) {
	s.restoreExec()
	s.restoreLogger()
}

func (s *cmdSuite) syscallExec(argv0 string, argv []string, envv []string) (err error) {
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
	s.fakeCoreVersion(c, coreDir, "42")
	p := filepath.Join(coreDir, "/usr/lib/snapd", toolName)
	c.Assert(ioutil.WriteFile(p, nil, 0755), IsNil)

	return p
}

func (s *cmdSuite) mockReExecingEnv() func() {
	restore := []func(){
		release.MockOnClassic(true),
		release.MockReleaseInfo(&release.OS{ID: "ubuntu"}),
		cmd.MockCoreSnapdPaths(s.corePath, s.snapdPath),
		cmd.MockVersion("2"),
	}

	return func() {
		for i := len(restore) - 1; i >= 0; i-- {
			restore[i]()
		}
	}
}

func (s *cmdSuite) mockReExecFor(c *C, coreDir, toolName string) func() {
	selfExe := filepath.Join(s.fakeroot, "proc/self/exe")
	restore := []func(){
		s.mockReExecingEnv(),
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

	// no distro supports re-exec when not on classic :-)
	for _, id := range []string{
		"fedora", "centos", "rhel", "opensuse", "suse", "poky",
		"debian", "ubuntu", "arch",
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
	p := s.snapdPath + "/usr/lib/snapd/info"
	c.Assert(os.MkdirAll(p, 0755), IsNil)

	c.Check(cmd.CoreSupportsReExec(s.snapdPath), Equals, false)
}

func (s *cmdSuite) TestCoreSupportsReExecBadInfoContent(c *C) {
	// can't understand snapd/info if all it holds are potatoes
	p := s.snapdPath + "/usr/lib/snapd"
	c.Assert(os.MkdirAll(p, 0755), IsNil)
	c.Assert(ioutil.WriteFile(p+"/info", []byte("potatoes"), 0644), IsNil)

	c.Check(cmd.CoreSupportsReExec(s.snapdPath), Equals, false)
}

func (s *cmdSuite) TestCoreSupportsReExecBadVersion(c *C) {
	// can't understand snapd/info if all its version is gibberish
	s.fakeCoreVersion(c, s.snapdPath, "0:")

	c.Check(cmd.CoreSupportsReExec(s.snapdPath), Equals, false)
}

func (s *cmdSuite) TestCoreSupportsReExecOldVersion(c *C) {
	// can't re-exec if core version is too old
	defer cmd.MockVersion("2")()
	s.fakeCoreVersion(c, s.snapdPath, "0")

	c.Check(cmd.CoreSupportsReExec(s.snapdPath), Equals, false)
}

func (s *cmdSuite) TestCoreSupportsReExec(c *C) {
	defer cmd.MockVersion("2")()
	s.fakeCoreVersion(c, s.snapdPath, "9999")

	c.Check(cmd.CoreSupportsReExec(s.snapdPath), Equals, true)
}

func (s *cmdSuite) TestInternalToolPathNoReexec(c *C) {
	restore := cmd.MockOsReadlink(func(string) (string, error) {
		return filepath.Join(dirs.DistroLibExecDir, "snapd"), nil
	})
	defer restore()

	c.Check(cmd.InternalToolPath("potato"), Equals, filepath.Join(dirs.DistroLibExecDir, "potato"))
}

func (s *cmdSuite) TestInternalToolPathWithReexec(c *C) {
	s.fakeInternalTool(c, s.snapdPath, "potato")
	restore := cmd.MockOsReadlink(func(string) (string, error) {
		return filepath.Join(s.snapdPath, "/usr/lib/snapd/snapd"), nil
	})
	defer restore()

	c.Check(cmd.InternalToolPath("potato"), Equals, filepath.Join(dirs.SnapMountDir, "snapd/42/usr/lib/snapd/potato"))
}

func (s *cmdSuite) TestInternalToolPathFromIncorrectHelper(c *C) {
	restore := cmd.MockOsReadlink(func(string) (string, error) {
		return "/usr/bin/potato", nil
	})
	defer restore()

	c.Check(func() { cmd.InternalToolPath("potato") }, PanicMatches, "InternalToolPath can only be used from snapd, got: /usr/bin/potato")
}

func (s *cmdSuite) TestExecInSnapdOrCoreSnap(c *C) {
	defer s.mockReExecFor(c, s.snapdPath, "potato")()

	c.Check(cmd.ExecInSnapdOrCoreSnap, PanicMatches, `>exec of "[^"]+/potato" in tests<`)
	c.Check(s.execCalled, Equals, 1)
	c.Check(s.lastExecArgv0, Equals, filepath.Join(s.snapdPath, "/usr/lib/snapd/potato"))
	c.Check(s.lastExecArgv, DeepEquals, os.Args)
}

func (s *cmdSuite) TestExecInOldCoreSnap(c *C) {
	defer s.mockReExecFor(c, s.corePath, "potato")()

	c.Check(cmd.ExecInSnapdOrCoreSnap, PanicMatches, `>exec of "[^"]+/potato" in tests<`)
	c.Check(s.execCalled, Equals, 1)
	c.Check(s.lastExecArgv0, Equals, filepath.Join(s.corePath, "/usr/lib/snapd/potato"))
	c.Check(s.lastExecArgv, DeepEquals, os.Args)
}

func (s *cmdSuite) TestExecInSnapdOrCoreSnapBailsNoCoreSupport(c *C) {
	defer s.mockReExecFor(c, s.snapdPath, "potato")()

	// no "info" -> no core support:
	c.Assert(os.Remove(filepath.Join(s.snapdPath, "/usr/lib/snapd/info")), IsNil)

	cmd.ExecInSnapdOrCoreSnap()
	c.Check(s.execCalled, Equals, 0)
}

func (s *cmdSuite) TestExecInSnapdOrCoreSnapMissingExe(c *C) {
	defer s.mockReExecFor(c, s.snapdPath, "potato")()

	// missing exe:
	c.Assert(os.Remove(filepath.Join(s.snapdPath, "/usr/lib/snapd/potato")), IsNil)

	cmd.ExecInSnapdOrCoreSnap()
	c.Check(s.execCalled, Equals, 0)
}

func (s *cmdSuite) TestExecInSnapdOrCoreSnapBadSelfExe(c *C) {
	defer s.mockReExecFor(c, s.snapdPath, "potato")()

	// missing self/exe:
	c.Assert(os.Remove(filepath.Join(s.fakeroot, "proc/self/exe")), IsNil)

	cmd.ExecInSnapdOrCoreSnap()
	c.Check(s.execCalled, Equals, 0)
}

func (s *cmdSuite) TestExecInSnapdOrCoreSnapBailsNoDistroSupport(c *C) {
	defer s.mockReExecFor(c, s.snapdPath, "potato")()

	// no distro support:
	defer release.MockOnClassic(false)()

	cmd.ExecInSnapdOrCoreSnap()
	c.Check(s.execCalled, Equals, 0)
}

func (s *cmdSuite) TestExecInSnapdOrCoreSnapNoDouble(c *C) {
	selfExe := filepath.Join(s.fakeroot, "proc/self/exe")
	err := os.Symlink(filepath.Join(s.fakeroot, "/snap/core/42/usr/lib/snapd"), selfExe)
	c.Assert(err, IsNil)
	cmd.MockSelfExe(selfExe)

	cmd.ExecInSnapdOrCoreSnap()
	c.Check(s.execCalled, Equals, 0)
}

func (s *cmdSuite) TestExecInSnapdOrCoreSnapDisabled(c *C) {
	defer s.mockReExecFor(c, s.snapdPath, "potato")()

	os.Setenv("SNAP_REEXEC", "0")
	defer os.Unsetenv("SNAP_REEXEC")

	cmd.ExecInSnapdOrCoreSnap()
	c.Check(s.execCalled, Equals, 0)
}
