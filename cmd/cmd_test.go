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
	p := d + "/usr/lib/snapd"
	c.Assert(os.MkdirAll(p, 0755), IsNil)
	c.Assert(ioutil.WriteFile(p+"/info", []byte("VERSION=0:"), 0644), IsNil)

	c.Check(cmd.CoreSupportsReExec(d), Equals, false)
}

func (s *cmdSuite) TestCoreSupportsReExecOldVersion(c *C) {
	// can't re-exec if core version is too old
	defer cmd.MockVersion("2")()
	d := c.MkDir()
	p := d + "/usr/lib/snapd"
	c.Assert(os.MkdirAll(p, 0755), IsNil)
	c.Assert(ioutil.WriteFile(p+"/info", []byte("VERSION=0"), 0644), IsNil)

	c.Check(cmd.CoreSupportsReExec(d), Equals, false)
}

func (s *cmdSuite) TestCoreSupportsReExec(c *C) {
	defer cmd.MockVersion("2")()
	d := c.MkDir()
	p := d + "/usr/lib/snapd"
	c.Assert(os.MkdirAll(p, 0755), IsNil)
	c.Assert(ioutil.WriteFile(p+"/info", []byte("VERSION=9999"), 0644), IsNil)

	c.Check(cmd.CoreSupportsReExec(d), Equals, true)
}

func (s *cmdSuite) TestInternalToolPathInNewCore(c *C) {
	d := c.MkDir()
	defer release.MockOnClassic(true)()
	defer release.MockReleaseInfo(&release.OS{ID: "ubuntu"})()
	defer cmd.MockCorePaths(c.MkDir(), d)()
	defer cmd.MockVersion("2")()

	p := filepath.Join(d, "/usr/lib/snapd/potato")
	c.Assert(os.MkdirAll(p, 0755), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(d, "/usr/lib/snapd/info"), []byte("VERSION=9999"), 0644), IsNil)

	c.Check(cmd.InternalToolPath("potato"), Equals, p)
}

func (s *cmdSuite) TestInternalToolPathInOldCore(c *C) {
	d := c.MkDir()
	defer release.MockOnClassic(true)()
	defer release.MockReleaseInfo(&release.OS{ID: "ubuntu"})()
	defer cmd.MockCorePaths(d, c.MkDir())()
	defer cmd.MockVersion("2")()

	p := filepath.Join(d, "/usr/lib/snapd/potato")
	c.Assert(os.MkdirAll(p, 0755), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(d, "/usr/lib/snapd/info"), []byte("VERSION=9999"), 0644), IsNil)

	c.Check(cmd.InternalToolPath("potato"), Equals, p)
}

func (s *cmdSuite) TestInternalToolPathInBothPrefersNore(c *C) {
	dNew := c.MkDir()
	dOld := c.MkDir()
	defer release.MockOnClassic(true)()
	defer release.MockReleaseInfo(&release.OS{ID: "ubuntu"})()
	defer cmd.MockCorePaths(dOld, dNew)()
	defer cmd.MockVersion("2")()

	pNew := filepath.Join(dNew, "/usr/lib/snapd/potato")
	c.Assert(os.MkdirAll(pNew, 0755), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(dNew, "/usr/lib/snapd/info"), []byte("VERSION=9999"), 0644), IsNil)

	pOld := filepath.Join(dOld, "/usr/lib/snapd/potato")
	c.Assert(os.MkdirAll(pOld, 0755), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(dOld, "/usr/lib/snapd/info"), []byte("VERSION=9999"), 0644), IsNil)

	c.Check(cmd.InternalToolPath("potato"), Equals, pNew)
}

func (s *cmdSuite) TestInternalToolPathDisabledByEnv(c *C) {
	os.Setenv("SNAP_REEXEC", "0")
	defer os.Unsetenv("SNAP_REEXEC")

	c.Check(cmd.InternalToolPath("potato"), Equals, filepath.Join(dirs.DistroLibExecDir, "potato"))
}

func (s *cmdSuite) TestInternalToolPathDisabledByDistro(c *C) {
	defer release.MockOnClassic(false)()

	c.Check(cmd.InternalToolPath("potato"), Equals, filepath.Join(dirs.DistroLibExecDir, "potato"))
}

func (s *cmdSuite) TestInternalToolPathInNewCoreDisabledByNoReExecSupportInCore(c *C) {
	d := c.MkDir()
	defer release.MockOnClassic(true)()
	defer release.MockReleaseInfo(&release.OS{ID: "ubuntu"})()
	defer cmd.MockCorePaths(c.MkDir(), d)()

	p := filepath.Join(d, "/usr/lib/snapd/potato")
	c.Assert(os.MkdirAll(p, 0755), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(d, "/usr/lib/snapd/info"), []byte("VERSION=0"), 0644), IsNil)

	c.Check(cmd.InternalToolPath("potato"), Equals, filepath.Join(dirs.DistroLibExecDir, "potato"))
}

func (s *cmdSuite) TestExecInCoreSnap(c *C) {
	d := c.MkDir()
	selfExe := filepath.Join(d, "self-exe")
	defer cmd.MockVersion("2")()
	defer cmd.MockSelfExe(selfExe)()
	defer release.MockOnClassic(true)()
	defer release.MockReleaseInfo(&release.OS{ID: "ubuntu"})()
	defer cmd.MockCorePaths(c.MkDir(), d)()
	c.Assert(os.Symlink("/bin/true", selfExe), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(d, "/bin/true"), 0755), IsNil)
	p := d + "/usr/lib/snapd"
	c.Assert(os.MkdirAll(p, 0755), IsNil)
	c.Assert(ioutil.WriteFile(p+"/info", []byte("VERSION=9999"), 0644), IsNil)

	c.Check(cmd.ExecInCoreSnap, PanicMatches, `>exec of "[^"]+/bin/true" in tests<`)
	c.Check(s.execCalled, Equals, 1)
	c.Check(s.lastExecArgv0, Equals, filepath.Join(d, "/bin/true"))
	c.Check(s.lastExecArgv, DeepEquals, os.Args)
	c.Check(s.lastExecEnvv, testutil.Contains, "SNAP_DID_REEXEC=1")
}

func (s *cmdSuite) TestExecInOldCoreSnap(c *C) {
	d := c.MkDir()
	selfExe := filepath.Join(d, "self-exe")
	defer cmd.MockVersion("2")()
	defer cmd.MockSelfExe(selfExe)()
	defer release.MockOnClassic(true)()
	defer release.MockReleaseInfo(&release.OS{ID: "ubuntu"})()
	defer cmd.MockCorePaths(d, c.MkDir())()
	c.Assert(os.Symlink("/bin/true", selfExe), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(d, "/bin/true"), 0755), IsNil)
	p := d + "/usr/lib/snapd"
	c.Assert(os.MkdirAll(p, 0755), IsNil)
	c.Assert(ioutil.WriteFile(p+"/info", []byte("VERSION=9999"), 0644), IsNil)

	c.Check(cmd.ExecInCoreSnap, PanicMatches, `>exec of "[^"]+/bin/true" in tests<`)
	c.Check(s.execCalled, Equals, 1)
	c.Check(s.lastExecArgv0, Equals, filepath.Join(d, "/bin/true"))
	c.Check(s.lastExecArgv, DeepEquals, os.Args)
	c.Check(s.lastExecEnvv, testutil.Contains, "SNAP_DID_REEXEC=1")
}

func (s *cmdSuite) TestExecInCoreSnapBailsNoCoreSupport(c *C) {
	d := c.MkDir()
	selfExe := filepath.Join(d, "self-exe")
	defer cmd.MockVersion("2")()
	defer cmd.MockSelfExe(selfExe)()
	defer release.MockOnClassic(true)()
	defer release.MockReleaseInfo(&release.OS{ID: "ubuntu"})()
	defer cmd.MockCorePaths(c.MkDir(), d)()
	c.Assert(os.Symlink("/bin/true", selfExe), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(d, "/bin/true"), 0755), IsNil)
	// no "info" -> no core support:
	// p := d + "/usr/lib/snapd"
	// c.Assert(os.MkdirAll(p, 0755), IsNil)
	// c.Assert(ioutil.WriteFile(p+"/info", []byte("VERSION=9999"), 0644), IsNil)

	cmd.ExecInCoreSnap()
	c.Check(s.execCalled, Equals, 0)
}

func (s *cmdSuite) TestExecInCoreSnapMissingExe(c *C) {
	d := c.MkDir()
	selfExe := filepath.Join(d, "self-exe")
	defer cmd.MockVersion("2")()
	defer cmd.MockSelfExe(selfExe)()
	defer release.MockOnClassic(true)()
	defer release.MockReleaseInfo(&release.OS{ID: "ubuntu"})()
	defer cmd.MockCorePaths(c.MkDir(), d)()
	c.Assert(os.Symlink("/bin/true", selfExe), IsNil)
	// missing exe:
	// c.Assert(os.MkdirAll(filepath.Join(d, "/bin/true"), 0755), IsNil)
	p := d + "/usr/lib/snapd"
	c.Assert(os.MkdirAll(p, 0755), IsNil)
	c.Assert(ioutil.WriteFile(p+"/info", []byte("VERSION=9999"), 0644), IsNil)

	cmd.ExecInCoreSnap()
	c.Check(s.execCalled, Equals, 0)
}

func (s *cmdSuite) TestExecInCoreSnapBadSelfExe(c *C) {
	d := c.MkDir()
	selfExe := filepath.Join(d, "self-exe")
	defer cmd.MockVersion("2")()
	defer cmd.MockSelfExe(selfExe)()
	defer release.MockOnClassic(true)()
	defer release.MockReleaseInfo(&release.OS{ID: "ubuntu"})()
	defer cmd.MockCorePaths(c.MkDir(), d)()
	// no self/exe -> no re-exec
	// c.Assert(os.Symlink("/bin/true", selfExe), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(d, "/bin/true"), 0755), IsNil)
	p := d + "/usr/lib/snapd"
	c.Assert(os.MkdirAll(p, 0755), IsNil)
	c.Assert(ioutil.WriteFile(p+"/info", []byte("VERSION=9999"), 0644), IsNil)

	cmd.ExecInCoreSnap()
	c.Check(s.execCalled, Equals, 0)
}

func (s *cmdSuite) TestExecInCoreSnapBailsNoDistroSupport(c *C) {
	d := c.MkDir()
	selfExe := filepath.Join(d, "self-exe")
	defer cmd.MockVersion("2")()
	defer cmd.MockSelfExe(selfExe)()
	// no distro support:
	defer release.MockOnClassic(false)()
	defer cmd.MockCorePaths(c.MkDir(), d)()
	c.Assert(os.Symlink("/bin/true", selfExe), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(d, "/bin/true"), 0755), IsNil)
	p := d + "/usr/lib/snapd"
	c.Assert(os.MkdirAll(p, 0755), IsNil)
	c.Assert(ioutil.WriteFile(p+"/info", []byte("VERSION=9999"), 0644), IsNil)

	cmd.ExecInCoreSnap()
	c.Check(s.execCalled, Equals, 0)
}

func (s *cmdSuite) TestExecInCoreSnapNoDouble(c *C) {
	d := c.MkDir()
	selfExe := filepath.Join(d, "self-exe")
	defer cmd.MockVersion("2")()
	defer cmd.MockSelfExe(selfExe)()
	defer release.MockOnClassic(true)()
	defer release.MockReleaseInfo(&release.OS{ID: "ubuntu"})()
	defer cmd.MockCorePaths(c.MkDir(), d)()
	c.Assert(os.Symlink("/bin/true", selfExe), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(d, "/bin/true"), 0755), IsNil)
	p := d + "/usr/lib/snapd"
	c.Assert(os.MkdirAll(p, 0755), IsNil)
	c.Assert(ioutil.WriteFile(p+"/info", []byte("VERSION=9999"), 0644), IsNil)

	os.Setenv("SNAP_DID_REEXEC", "1")
	defer os.Unsetenv("SNAP_DID_REEXEC")

	cmd.ExecInCoreSnap()
	c.Check(s.execCalled, Equals, 0)
}

func (s *cmdSuite) TestExecInCoreSnapDisabled(c *C) {
	d := c.MkDir()
	selfExe := filepath.Join(d, "self-exe")
	defer cmd.MockVersion("2")()
	defer cmd.MockSelfExe(selfExe)()
	defer release.MockOnClassic(true)()
	defer release.MockReleaseInfo(&release.OS{ID: "ubuntu"})()
	defer cmd.MockCorePaths(c.MkDir(), d)()
	c.Assert(os.Symlink("/bin/true", selfExe), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(d, "/bin/true"), 0755), IsNil)
	p := d + "/usr/lib/snapd"
	c.Assert(os.MkdirAll(p, 0755), IsNil)
	c.Assert(ioutil.WriteFile(p+"/info", []byte("VERSION=9999"), 0644), IsNil)

	os.Setenv("SNAP_REEXEC", "0")
	defer os.Unsetenv("SNAP_REEXEC")

	cmd.ExecInCoreSnap()
	c.Check(s.execCalled, Equals, 0)
}
