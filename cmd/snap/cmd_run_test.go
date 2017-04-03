// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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
	"os/user"
	"path/filepath"

	"gopkg.in/check.v1"

	snaprun "github.com/snapcore/snapd/cmd/snap"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/x11"
)

var mockYaml = []byte(`name: snapname
version: 1.0
apps:
 app:
  command: run-app
hooks:
 configure:
`)
var mockContents = "SNAP"

func (s *SnapSuite) TestInvalidParameters(c *check.C) {
	invalidParameters := []string{"run", "--hook=configure", "--command=command-name", "snap-name"}
	_, err := snaprun.Parser().ParseArgs(invalidParameters)
	c.Check(err, check.ErrorMatches, ".*cannot use --hook and --command together.*")

	invalidParameters = []string{"run", "-r=1", "--command=command-name", "snap-name"}
	_, err = snaprun.Parser().ParseArgs(invalidParameters)
	c.Check(err, check.ErrorMatches, ".*-r can only be used with --hook.*")

	invalidParameters = []string{"run", "-r=1", "snap-name"}
	_, err = snaprun.Parser().ParseArgs(invalidParameters)
	c.Check(err, check.ErrorMatches, ".*-r can only be used with --hook.*")

	invalidParameters = []string{"run", "--hook=configure", "foo", "bar", "snap-name"}
	_, err = snaprun.Parser().ParseArgs(invalidParameters)
	c.Check(err, check.ErrorMatches, ".*too many arguments for hook \"configure\": bar.*")
}

func (s *SnapSuite) TestSnapRunWhenMissingConfine(c *check.C) {
	dirs.SetRootDir(c.MkDir())

	// mock installed snap
	si := snaptest.MockSnap(c, string(mockYaml), string(mockContents), &snap.SideInfo{
		Revision: snap.R("x2"),
	})
	err := os.Symlink(si.MountDir(), filepath.Join(si.MountDir(), "../current"))
	c.Assert(err, check.IsNil)

	// redirect exec
	var execs [][]string
	restorer := snaprun.MockSyscallExec(func(arg0 string, args []string, envv []string) error {
		execs = append(execs, args)
		return nil
	})
	defer restorer()

	// and run it!
	// a regular run will fail
	_, err = snaprun.Parser().ParseArgs([]string{"run", "snapname.app", "--arg1", "arg2"})
	c.Assert(err, check.ErrorMatches, `.* your snapd package`)
	// a hook run will not fail
	_, err = snaprun.Parser().ParseArgs([]string{"run", "--hook=configure", "snapname"})
	c.Assert(err, check.IsNil)

	// but nothing is run ever
	c.Check(execs, check.IsNil)
}

func (s *SnapSuite) TestSnapRunAppIntegration(c *check.C) {
	// mock installed snap
	dirs.SetRootDir(c.MkDir())
	defer func() { dirs.SetRootDir("/") }()
	defer mockSnapConfine()()

	si := snaptest.MockSnap(c, string(mockYaml), string(mockContents), &snap.SideInfo{
		Revision: snap.R("x2"),
	})
	err := os.Symlink(si.MountDir(), filepath.Join(si.MountDir(), "../current"))
	c.Assert(err, check.IsNil)

	// redirect exec
	execArg0 := ""
	execArgs := []string{}
	execEnv := []string{}
	restorer := snaprun.MockSyscallExec(func(arg0 string, args []string, envv []string) error {
		execArg0 = arg0
		execArgs = args
		execEnv = envv
		return nil
	})
	defer restorer()

	// and run it!
	rest, err := snaprun.Parser().ParseArgs([]string{"run", "snapname.app", "--arg1", "arg2"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{"snapname.app", "--arg1", "arg2"})
	c.Check(execArg0, check.Equals, filepath.Join(dirs.DistroLibExecDir, "snap-confine"))
	c.Check(execArgs, check.DeepEquals, []string{
		filepath.Join(dirs.DistroLibExecDir, "snap-confine"),
		"snap.snapname.app",
		filepath.Join(dirs.DistroLibExecDir, "snap-exec"),
		"snapname.app", "--arg1", "arg2"})
	c.Check(execEnv, testutil.Contains, "SNAP_REVISION=x2")
}

func (s *SnapSuite) TestSnapRunClassicAppIntegration(c *check.C) {
	// mock installed snap
	dirs.SetRootDir(c.MkDir())
	defer func() { dirs.SetRootDir("/") }()
	defer mockSnapConfine()()

	si := snaptest.MockSnap(c, string(mockYaml)+"confinement: classic\n", string(mockContents), &snap.SideInfo{
		Revision: snap.R("x2"),
	})
	err := os.Symlink(si.MountDir(), filepath.Join(si.MountDir(), "../current"))
	c.Assert(err, check.IsNil)

	// redirect exec
	execArg0 := ""
	execArgs := []string{}
	execEnv := []string{}
	restorer := snaprun.MockSyscallExec(func(arg0 string, args []string, envv []string) error {
		execArg0 = arg0
		execArgs = args
		execEnv = envv
		return nil
	})
	defer restorer()

	// and run it!
	rest, err := snaprun.Parser().ParseArgs([]string{"run", "snapname.app", "--arg1", "arg2"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{"snapname.app", "--arg1", "arg2"})
	c.Check(execArg0, check.Equals, filepath.Join(dirs.DistroLibExecDir, "snap-confine"))
	c.Check(execArgs, check.DeepEquals, []string{
		filepath.Join(dirs.DistroLibExecDir, "snap-confine"), "--classic",
		"snap.snapname.app",
		filepath.Join(dirs.DistroLibExecDir, "snap-exec"),
		"snapname.app", "--arg1", "arg2"})
	c.Check(execEnv, testutil.Contains, "SNAP_REVISION=x2")
}

func (s *SnapSuite) TestSnapRunAppWithCommandIntegration(c *check.C) {
	// mock installed snap
	dirs.SetRootDir(c.MkDir())
	defer func() { dirs.SetRootDir("/") }()
	defer mockSnapConfine()()

	si := snaptest.MockSnap(c, string(mockYaml), string(mockContents), &snap.SideInfo{
		Revision: snap.R(42),
	})
	err := os.Symlink(si.MountDir(), filepath.Join(si.MountDir(), "../current"))
	c.Assert(err, check.IsNil)

	// redirect exec
	execArg0 := ""
	execArgs := []string{}
	execEnv := []string{}
	restorer := snaprun.MockSyscallExec(func(arg0 string, args []string, envv []string) error {
		execArg0 = arg0
		execArgs = args
		execEnv = envv
		return nil
	})
	defer restorer()

	// and run it!
	err = snaprun.SnapRunApp("snapname.app", "my-command", []string{"arg1", "arg2"})
	c.Assert(err, check.IsNil)
	c.Check(execArg0, check.Equals, filepath.Join(dirs.DistroLibExecDir, "snap-confine"))
	c.Check(execArgs, check.DeepEquals, []string{
		filepath.Join(dirs.DistroLibExecDir, "snap-confine"),
		"snap.snapname.app",
		filepath.Join(dirs.DistroLibExecDir, "snap-exec"),
		"--command=my-command", "snapname.app", "arg1", "arg2"})
	c.Check(execEnv, testutil.Contains, "SNAP_REVISION=42")
}

func (s *SnapSuite) TestSnapRunCreateDataDirs(c *check.C) {
	info, err := snap.InfoFromSnapYaml(mockYaml)
	c.Assert(err, check.IsNil)
	info.SideInfo.Revision = snap.R(42)

	fakeHome := c.MkDir()
	restorer := snaprun.MockUserCurrent(func() (*user.User, error) {
		return &user.User{HomeDir: fakeHome}, nil
	})
	defer restorer()

	err = snaprun.CreateUserDataDirs(info)
	c.Assert(err, check.IsNil)
	c.Check(osutil.FileExists(filepath.Join(fakeHome, "/snap/snapname/42")), check.Equals, true)
	c.Check(osutil.FileExists(filepath.Join(fakeHome, "/snap/snapname/common")), check.Equals, true)
}

func (s *SnapSuite) TestSnapRunHookIntegration(c *check.C) {
	// mock installed snap
	dirs.SetRootDir(c.MkDir())
	defer func() { dirs.SetRootDir("/") }()
	defer mockSnapConfine()()

	si := snaptest.MockSnap(c, string(mockYaml), string(mockContents), &snap.SideInfo{
		Revision: snap.R(42),
	})
	err := os.Symlink(si.MountDir(), filepath.Join(si.MountDir(), "../current"))
	c.Assert(err, check.IsNil)

	// redirect exec
	execArg0 := ""
	execArgs := []string{}
	execEnv := []string{}
	restorer := snaprun.MockSyscallExec(func(arg0 string, args []string, envv []string) error {
		execArg0 = arg0
		execArgs = args
		execEnv = envv
		return nil
	})
	defer restorer()

	// Run a hook from the active revision
	_, err = snaprun.Parser().ParseArgs([]string{"run", "--hook=configure", "snapname"})
	c.Assert(err, check.IsNil)
	c.Check(execArg0, check.Equals, filepath.Join(dirs.DistroLibExecDir, "snap-confine"))
	c.Check(execArgs, check.DeepEquals, []string{
		filepath.Join(dirs.DistroLibExecDir, "snap-confine"),
		"snap.snapname.hook.configure",
		filepath.Join(dirs.DistroLibExecDir, "snap-exec"),
		"--hook=configure", "snapname"})
	c.Check(execEnv, testutil.Contains, "SNAP_REVISION=42")
}

func (s *SnapSuite) TestSnapRunHookUnsetRevisionIntegration(c *check.C) {
	// mock installed snap
	dirs.SetRootDir(c.MkDir())
	defer func() { dirs.SetRootDir("/") }()
	defer mockSnapConfine()()

	si := snaptest.MockSnap(c, string(mockYaml), string(mockContents), &snap.SideInfo{
		Revision: snap.R(42),
	})
	err := os.Symlink(si.MountDir(), filepath.Join(si.MountDir(), "../current"))
	c.Assert(err, check.IsNil)

	// redirect exec
	execArg0 := ""
	execArgs := []string{}
	execEnv := []string{}
	restorer := snaprun.MockSyscallExec(func(arg0 string, args []string, envv []string) error {
		execArg0 = arg0
		execArgs = args
		execEnv = envv
		return nil
	})
	defer restorer()

	// Specifically pass "unset" which would use the active version.
	_, err = snaprun.Parser().ParseArgs([]string{"run", "--hook=configure", "-r=unset", "snapname"})
	c.Assert(err, check.IsNil)
	c.Check(execArg0, check.Equals, filepath.Join(dirs.DistroLibExecDir, "snap-confine"))
	c.Check(execArgs, check.DeepEquals, []string{
		filepath.Join(dirs.DistroLibExecDir, "snap-confine"),
		"snap.snapname.hook.configure",
		filepath.Join(dirs.DistroLibExecDir, "snap-exec"),
		"--hook=configure", "snapname"})
	c.Check(execEnv, testutil.Contains, "SNAP_REVISION=42")
}

func (s *SnapSuite) TestSnapRunHookSpecificRevisionIntegration(c *check.C) {
	// mock installed snap
	dirs.SetRootDir(c.MkDir())
	defer func() { dirs.SetRootDir("/") }()
	defer mockSnapConfine()()

	// Create both revisions 41 and 42
	snaptest.MockSnap(c, string(mockYaml), string(mockContents), &snap.SideInfo{
		Revision: snap.R(41),
	})
	snaptest.MockSnap(c, string(mockYaml), string(mockContents), &snap.SideInfo{
		Revision: snap.R(42),
	})

	// redirect exec
	execArg0 := ""
	execArgs := []string{}
	execEnv := []string{}
	restorer := snaprun.MockSyscallExec(func(arg0 string, args []string, envv []string) error {
		execArg0 = arg0
		execArgs = args
		execEnv = envv
		return nil
	})
	defer restorer()

	// Run a hook on revision 41
	_, err := snaprun.Parser().ParseArgs([]string{"run", "--hook=configure", "-r=41", "snapname"})
	c.Assert(err, check.IsNil)
	c.Check(execArg0, check.Equals, filepath.Join(dirs.DistroLibExecDir, "snap-confine"))
	c.Check(execArgs, check.DeepEquals, []string{
		filepath.Join(dirs.DistroLibExecDir, "snap-confine"),
		"snap.snapname.hook.configure",
		filepath.Join(dirs.DistroLibExecDir, "snap-exec"),
		"--hook=configure", "snapname"})
	c.Check(execEnv, testutil.Contains, "SNAP_REVISION=41")
}

func (s *SnapSuite) TestSnapRunHookMissingRevisionIntegration(c *check.C) {
	// mock installed snap
	dirs.SetRootDir(c.MkDir())
	defer func() { dirs.SetRootDir("/") }()

	// Only create revision 42
	si := snaptest.MockSnap(c, string(mockYaml), string(mockContents), &snap.SideInfo{
		Revision: snap.R(42),
	})
	err := os.Symlink(si.MountDir(), filepath.Join(si.MountDir(), "../current"))
	c.Assert(err, check.IsNil)

	// redirect exec
	restorer := snaprun.MockSyscallExec(func(arg0 string, args []string, envv []string) error {
		return nil
	})
	defer restorer()

	// Attempt to run a hook on revision 41, which doesn't exist
	_, err = snaprun.Parser().ParseArgs([]string{"run", "--hook=configure", "-r=41", "snapname"})
	c.Assert(err, check.NotNil)
	c.Check(err, check.ErrorMatches, "cannot find .*")
}

func (s *SnapSuite) TestSnapRunHookInvalidRevisionIntegration(c *check.C) {
	_, err := snaprun.Parser().ParseArgs([]string{"run", "--hook=configure", "-r=invalid", "snapname"})
	c.Assert(err, check.NotNil)
	c.Check(err, check.ErrorMatches, "invalid snap revision: \"invalid\"")
}

func (s *SnapSuite) TestSnapRunHookMissingHookIntegration(c *check.C) {
	// mock installed snap
	dirs.SetRootDir(c.MkDir())
	defer func() { dirs.SetRootDir("/") }()

	// Only create revision 42
	si := snaptest.MockSnap(c, string(mockYaml), string(mockContents), &snap.SideInfo{
		Revision: snap.R(42),
	})
	err := os.Symlink(si.MountDir(), filepath.Join(si.MountDir(), "../current"))
	c.Assert(err, check.IsNil)

	// redirect exec
	called := false
	restorer := snaprun.MockSyscallExec(func(arg0 string, args []string, envv []string) error {
		called = true
		return nil
	})
	defer restorer()

	err = snaprun.SnapRunHook("snapname", "unset", "missing-hook")
	c.Assert(err, check.ErrorMatches, `cannot find hook "missing-hook" in "snapname"`)
	c.Check(called, check.Equals, false)
}

func (s *SnapSuite) TestSnapRunErorsForUnknownRunArg(c *check.C) {
	_, err := snaprun.Parser().ParseArgs([]string{"run", "--unknown", "snapname.app", "--arg1", "arg2"})
	c.Assert(err, check.ErrorMatches, "unknown flag `unknown'")
}

func (s *SnapSuite) TestSnapRunErorsForMissingApp(c *check.C) {
	_, err := snaprun.Parser().ParseArgs([]string{"run", "--command=shell"})
	c.Assert(err, check.ErrorMatches, "need the application to run as argument")
}

func (s *SnapSuite) TestSnapRunErorrForUnavailableApp(c *check.C) {
	_, err := snaprun.Parser().ParseArgs([]string{"run", "not-there"})
	c.Assert(err, check.ErrorMatches, "cannot find current revision for snap not-there: readlink /snap/not-there/current: no such file or directory")
}

func (s *SnapSuite) TestSnapRunSaneEnvironmentHandling(c *check.C) {
	// mock installed snap
	dirs.SetRootDir(c.MkDir())
	defer func() { dirs.SetRootDir("/") }()
	defer mockSnapConfine()()

	si := snaptest.MockSnap(c, string(mockYaml), string(mockContents), &snap.SideInfo{
		Revision: snap.R(42),
	})
	err := os.Symlink(si.MountDir(), filepath.Join(si.MountDir(), "../current"))
	c.Assert(err, check.IsNil)

	// redirect exec
	execEnv := []string{}
	restorer := snaprun.MockSyscallExec(func(arg0 string, args []string, envv []string) error {
		execEnv = envv
		return nil
	})
	defer restorer()

	// set a SNAP{,_*} variable in the environment
	os.Setenv("SNAP_NAME", "something-else")
	os.Setenv("SNAP_ARCH", "PDP-7")
	defer os.Unsetenv("SNAP_NAME")
	defer os.Unsetenv("SNAP_ARCH")
	// but unrelated stuff is ok
	os.Setenv("SNAP_THE_WORLD", "YES")
	defer os.Unsetenv("SNAP_THE_WORLD")

	// and ensure those SNAP_ vars get overridden
	rest, err := snaprun.Parser().ParseArgs([]string{"run", "snapname.app", "--arg1", "arg2"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{"snapname.app", "--arg1", "arg2"})
	c.Check(execEnv, testutil.Contains, "SNAP_REVISION=42")
	c.Check(execEnv, check.Not(testutil.Contains), "SNAP_NAME=something-else")
	c.Check(execEnv, check.Not(testutil.Contains), "SNAP_ARCH=PDP-7")
	c.Check(execEnv, testutil.Contains, "SNAP_THE_WORLD=YES")
}

func (s *SnapSuite) TestSnapRunIsReexeced(c *check.C) {
	var osReadlinkResult string
	restore := snaprun.MockOsReadlink(func(name string) (string, error) {
		return osReadlinkResult, nil
	})
	defer restore()

	for _, t := range []struct {
		readlink string
		expected bool
	}{
		{filepath.Join(dirs.SnapMountDir, "/usr/lib/snapd/snapd"), true},
		{"/usr/lib/snapd/snapd", false},
	} {
		osReadlinkResult = t.readlink
		c.Check(snaprun.IsReexeced(), check.Equals, t.expected)
	}
}

func (s *SnapSuite) TestSnapRunAppIntegrationFromCore(c *check.C) {
	// mock installed snap
	dirs.SetRootDir(c.MkDir())
	defer func() { dirs.SetRootDir("/") }()
	defer mockSnapConfine()()

	si := snaptest.MockSnap(c, string(mockYaml), string(mockContents), &snap.SideInfo{
		Revision: snap.R("x2"),
	})
	err := os.Symlink(si.MountDir(), filepath.Join(si.MountDir(), "../current"))
	c.Assert(err, check.IsNil)

	// pretend to be running from core
	restorer := snaprun.MockOsReadlink(func(string) (string, error) {
		return filepath.Join(dirs.SnapMountDir, "/core/111//usr/bin/snap"), nil
	})
	defer restorer()

	// redirect exec
	execArg0 := ""
	execArgs := []string{}
	execEnv := []string{}
	restorer = snaprun.MockSyscallExec(func(arg0 string, args []string, envv []string) error {
		execArg0 = arg0
		execArgs = args
		execEnv = envv
		return nil
	})
	defer restorer()

	// and run it!
	rest, err := snaprun.Parser().ParseArgs([]string{"run", "snapname.app", "--arg1", "arg2"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{"snapname.app", "--arg1", "arg2"})
	c.Check(execArg0, check.Equals, filepath.Join(dirs.SnapMountDir, "/core/current", dirs.DistroLibExecDir, "snap-confine"))
	c.Check(execArgs, check.DeepEquals, []string{
		filepath.Join(dirs.SnapMountDir, "/core/current", dirs.DistroLibExecDir, "snap-confine"),
		"snap.snapname.app",
		filepath.Join(dirs.DistroLibExecDir, "snap-exec"),
		"snapname.app", "--arg1", "arg2"})
	c.Check(execEnv, testutil.Contains, "SNAP_REVISION=x2")
}

func (s *SnapSuite) TestSnapRunXauthorityMigration(c *check.C) {
	// mock installed snap
	dirs.SetRootDir(c.MkDir())
	defer func() { dirs.SetRootDir("/") }()
	defer mockSnapConfine()()

	si := snaptest.MockSnap(c, string(mockYaml), string(mockContents), &snap.SideInfo{
		Revision: snap.R("x2"),
	})
	err := os.Symlink(si.MountDir(), filepath.Join(si.MountDir(), "../current"))
	c.Assert(err, check.IsNil)

	// redirect exec
	execArg0 := ""
	execArgs := []string{}
	execEnv := []string{}
	restorer := snaprun.MockSyscallExec(func(arg0 string, args []string, envv []string) error {
		execArg0 = arg0
		execArgs = args
		execEnv = envv
		return nil
	})
	defer restorer()

	defer snaprun.MockUserCurrent(func() (*user.User, error) {
		return &user.User{Uid: "1000"}, nil
	})()

	xauthPath, err := x11.MockXauthority(2)
	c.Assert(err, check.IsNil)
	defer os.Remove(xauthPath)

	defer snaprun.MockGetEnv(func(name string) string {
		if name == "XAUTHORITY" {
			return xauthPath
		}
		return ""
	})()

	// and run it!
	rest, err := snaprun.Parser().ParseArgs([]string{"run", "snapname.app"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{"snapname.app"})
	c.Check(execArg0, check.Equals, filepath.Join(dirs.DistroLibExecDir, "snap-confine"))
	c.Check(execArgs, check.DeepEquals, []string{
		filepath.Join(dirs.DistroLibExecDir, "snap-confine"),
		"snap.snapname.app",
		filepath.Join(dirs.DistroLibExecDir, "snap-exec"),
		"snapname.app"})

	expectedXauthPath := filepath.Join(dirs.XdgRuntimeDirBase, "1000", "Xauthority")
	c.Check(execEnv, testutil.Contains, fmt.Sprintf("XAUTHORITY=%s", expectedXauthPath))

	info, err := os.Stat(expectedXauthPath)
	c.Assert(err, check.IsNil)
	c.Assert(info.Mode().Perm(), check.Equals, os.FileMode(0600))

	err = x11.ValidateXauthority(expectedXauthPath)
	c.Assert(err, check.IsNil)
}
