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
	"io/ioutil"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/check.v1"

	snaprun "github.com/snapcore/snapd/cmd/snap"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
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

func (s *SnapSuite) TestInvalidParameters(c *check.C) {
	invalidParameters := []string{"run", "--hook=configure", "--command=command-name", "--", "snap-name"}
	_, err := snaprun.Parser(snaprun.Client()).ParseArgs(invalidParameters)
	c.Check(err, check.ErrorMatches, ".*you can only use one of --hook, --command, and --timer.*")

	invalidParameters = []string{"run", "--hook=configure", "--timer=10:00-12:00", "--", "snap-name"}
	_, err = snaprun.Parser(snaprun.Client()).ParseArgs(invalidParameters)
	c.Check(err, check.ErrorMatches, ".*you can only use one of --hook, --command, and --timer.*")

	invalidParameters = []string{"run", "--command=command-name", "--timer=10:00-12:00", "--", "snap-name"}
	_, err = snaprun.Parser(snaprun.Client()).ParseArgs(invalidParameters)
	c.Check(err, check.ErrorMatches, ".*you can only use one of --hook, --command, and --timer.*")

	invalidParameters = []string{"run", "-r=1", "--command=command-name", "--", "snap-name"}
	_, err = snaprun.Parser(snaprun.Client()).ParseArgs(invalidParameters)
	c.Check(err, check.ErrorMatches, ".*-r can only be used with --hook.*")

	invalidParameters = []string{"run", "-r=1", "--", "snap-name"}
	_, err = snaprun.Parser(snaprun.Client()).ParseArgs(invalidParameters)
	c.Check(err, check.ErrorMatches, ".*-r can only be used with --hook.*")

	invalidParameters = []string{"run", "--hook=configure", "--", "foo", "bar", "snap-name"}
	_, err = snaprun.Parser(snaprun.Client()).ParseArgs(invalidParameters)
	c.Check(err, check.ErrorMatches, ".*too many arguments for hook \"configure\": bar.*")
}

func (s *SnapSuite) TestSnapRunWhenMissingConfine(c *check.C) {
	_, r := logger.MockLogger()
	defer r()

	// mock installed snap
	snaptest.MockSnapCurrent(c, string(mockYaml), &snap.SideInfo{
		Revision: snap.R("x2"),
	})

	// redirect exec
	var execs [][]string
	restorer := snaprun.MockSyscallExec(func(arg0 string, args []string, envv []string) error {
		execs = append(execs, args)
		return nil
	})
	defer restorer()

	// and run it!
	// a regular run will fail
	_, err := snaprun.Parser(snaprun.Client()).ParseArgs([]string{"run", "--", "snapname.app", "--arg1", "arg2"})
	c.Assert(err, check.ErrorMatches, `.* your core/snapd package`)
	// a hook run will not fail
	_, err = snaprun.Parser(snaprun.Client()).ParseArgs([]string{"run", "--hook=configure", "--", "snapname"})
	c.Assert(err, check.IsNil)

	// but nothing is run ever
	c.Check(execs, check.IsNil)
}

func (s *SnapSuite) TestSnapRunAppIntegration(c *check.C) {
	defer mockSnapConfine(dirs.DistroLibExecDir)()

	// mock installed snap
	snaptest.MockSnapCurrent(c, string(mockYaml), &snap.SideInfo{
		Revision: snap.R("x2"),
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

	// and run it!
	rest, err := snaprun.Parser(snaprun.Client()).ParseArgs([]string{"run", "--", "snapname.app", "--arg1", "arg2"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{"snapname.app", "--arg1", "arg2"})
	c.Check(execArg0, check.Equals, filepath.Join(dirs.DistroLibExecDir, "snap-confine"))
	c.Check(execArgs, check.DeepEquals, []string{
		filepath.Join(dirs.DistroLibExecDir, "snap-confine"),
		"snap.snapname.app",
		filepath.Join(dirs.CoreLibExecDir, "snap-exec"),
		"snapname.app", "--arg1", "arg2"})
	c.Check(execEnv, testutil.Contains, "SNAP_REVISION=x2")
}

func (s *SnapSuite) TestSnapRunClassicAppIntegration(c *check.C) {
	defer mockSnapConfine(dirs.DistroLibExecDir)()

	// mock installed snap
	snaptest.MockSnapCurrent(c, string(mockYaml)+"confinement: classic\n", &snap.SideInfo{
		Revision: snap.R("x2"),
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

	// and run it!
	rest, err := snaprun.Parser(snaprun.Client()).ParseArgs([]string{"run", "--", "snapname.app", "--arg1", "arg2"})
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

func (s *SnapSuite) TestSnapRunClassicAppIntegrationReexeced(c *check.C) {
	mountedCorePath := filepath.Join(dirs.SnapMountDir, "core/current")
	mountedCoreLibExecPath := filepath.Join(mountedCorePath, dirs.CoreLibExecDir)

	defer mockSnapConfine(mountedCoreLibExecPath)()

	// mock installed snap
	snaptest.MockSnapCurrent(c, string(mockYaml)+"confinement: classic\n", &snap.SideInfo{
		Revision: snap.R("x2"),
	})

	restore := snaprun.MockOsReadlink(func(name string) (string, error) {
		// pretend 'snap' is reexeced from 'core'
		return filepath.Join(mountedCorePath, "usr/bin/snap"), nil
	})
	defer restore()

	execArgs := []string{}
	restorer := snaprun.MockSyscallExec(func(arg0 string, args []string, envv []string) error {
		execArgs = args
		return nil
	})
	defer restorer()
	rest, err := snaprun.Parser(snaprun.Client()).ParseArgs([]string{"run", "--", "snapname.app", "--arg1", "arg2"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{"snapname.app", "--arg1", "arg2"})
	c.Check(execArgs, check.DeepEquals, []string{
		filepath.Join(mountedCoreLibExecPath, "snap-confine"), "--classic",
		"snap.snapname.app",
		filepath.Join(mountedCoreLibExecPath, "snap-exec"),
		"snapname.app", "--arg1", "arg2"})
}

func (s *SnapSuite) TestSnapRunAppWithCommandIntegration(c *check.C) {
	defer mockSnapConfine(dirs.DistroLibExecDir)()

	// mock installed snap
	snaptest.MockSnapCurrent(c, string(mockYaml), &snap.SideInfo{
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

	// and run it!
	_, err := snaprun.Parser(snaprun.Client()).ParseArgs([]string{"run", "--command=my-command", "--", "snapname.app", "arg1", "arg2"})
	c.Assert(err, check.IsNil)
	c.Check(execArg0, check.Equals, filepath.Join(dirs.DistroLibExecDir, "snap-confine"))
	c.Check(execArgs, check.DeepEquals, []string{
		filepath.Join(dirs.DistroLibExecDir, "snap-confine"),
		"snap.snapname.app",
		filepath.Join(dirs.CoreLibExecDir, "snap-exec"),
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

func (s *SnapSuite) TestParallelInstanceSnapRunCreateDataDirs(c *check.C) {
	info, err := snap.InfoFromSnapYaml(mockYaml)
	c.Assert(err, check.IsNil)
	info.SideInfo.Revision = snap.R(42)
	info.InstanceKey = "foo"

	fakeHome := c.MkDir()
	restorer := snaprun.MockUserCurrent(func() (*user.User, error) {
		return &user.User{HomeDir: fakeHome}, nil
	})
	defer restorer()

	err = snaprun.CreateUserDataDirs(info)
	c.Assert(err, check.IsNil)
	c.Check(osutil.FileExists(filepath.Join(fakeHome, "/snap/snapname_foo/42")), check.Equals, true)
	c.Check(osutil.FileExists(filepath.Join(fakeHome, "/snap/snapname_foo/common")), check.Equals, true)
	// mount point for snap instance mapping has been created
	c.Check(osutil.FileExists(filepath.Join(fakeHome, "/snap/snapname")), check.Equals, true)
	// and it's empty inside
	m, err := filepath.Glob(filepath.Join(fakeHome, "/snap/snapname/*"))
	c.Assert(err, check.IsNil)
	c.Assert(m, check.HasLen, 0)
}

func (s *SnapSuite) TestSnapRunHookIntegration(c *check.C) {
	defer mockSnapConfine(dirs.DistroLibExecDir)()

	// mock installed snap
	snaptest.MockSnapCurrent(c, string(mockYaml), &snap.SideInfo{
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

	// Run a hook from the active revision
	_, err := snaprun.Parser(snaprun.Client()).ParseArgs([]string{"run", "--hook=configure", "--", "snapname"})
	c.Assert(err, check.IsNil)
	c.Check(execArg0, check.Equals, filepath.Join(dirs.DistroLibExecDir, "snap-confine"))
	c.Check(execArgs, check.DeepEquals, []string{
		filepath.Join(dirs.DistroLibExecDir, "snap-confine"),
		"snap.snapname.hook.configure",
		filepath.Join(dirs.CoreLibExecDir, "snap-exec"),
		"--hook=configure", "snapname"})
	c.Check(execEnv, testutil.Contains, "SNAP_REVISION=42")
}

func (s *SnapSuite) TestSnapRunHookUnsetRevisionIntegration(c *check.C) {
	defer mockSnapConfine(dirs.DistroLibExecDir)()

	// mock installed snap
	snaptest.MockSnapCurrent(c, string(mockYaml), &snap.SideInfo{
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

	// Specifically pass "unset" which would use the active version.
	_, err := snaprun.Parser(snaprun.Client()).ParseArgs([]string{"run", "--hook=configure", "-r=unset", "--", "snapname"})
	c.Assert(err, check.IsNil)
	c.Check(execArg0, check.Equals, filepath.Join(dirs.DistroLibExecDir, "snap-confine"))
	c.Check(execArgs, check.DeepEquals, []string{
		filepath.Join(dirs.DistroLibExecDir, "snap-confine"),
		"snap.snapname.hook.configure",
		filepath.Join(dirs.CoreLibExecDir, "snap-exec"),
		"--hook=configure", "snapname"})
	c.Check(execEnv, testutil.Contains, "SNAP_REVISION=42")
}

func (s *SnapSuite) TestSnapRunHookSpecificRevisionIntegration(c *check.C) {
	defer mockSnapConfine(dirs.DistroLibExecDir)()

	// mock installed snap
	// Create both revisions 41 and 42
	snaptest.MockSnap(c, string(mockYaml), &snap.SideInfo{
		Revision: snap.R(41),
	})
	snaptest.MockSnap(c, string(mockYaml), &snap.SideInfo{
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
	_, err := snaprun.Parser(snaprun.Client()).ParseArgs([]string{"run", "--hook=configure", "-r=41", "--", "snapname"})
	c.Assert(err, check.IsNil)
	c.Check(execArg0, check.Equals, filepath.Join(dirs.DistroLibExecDir, "snap-confine"))
	c.Check(execArgs, check.DeepEquals, []string{
		filepath.Join(dirs.DistroLibExecDir, "snap-confine"),
		"snap.snapname.hook.configure",
		filepath.Join(dirs.CoreLibExecDir, "snap-exec"),
		"--hook=configure", "snapname"})
	c.Check(execEnv, testutil.Contains, "SNAP_REVISION=41")
}

func (s *SnapSuite) TestSnapRunHookMissingRevisionIntegration(c *check.C) {
	// Only create revision 42
	snaptest.MockSnapCurrent(c, string(mockYaml), &snap.SideInfo{
		Revision: snap.R(42),
	})

	// redirect exec
	restorer := snaprun.MockSyscallExec(func(arg0 string, args []string, envv []string) error {
		return nil
	})
	defer restorer()

	// Attempt to run a hook on revision 41, which doesn't exist
	_, err := snaprun.Parser(snaprun.Client()).ParseArgs([]string{"run", "--hook=configure", "-r=41", "--", "snapname"})
	c.Assert(err, check.NotNil)
	c.Check(err, check.ErrorMatches, "cannot find .*")
}

func (s *SnapSuite) TestSnapRunHookInvalidRevisionIntegration(c *check.C) {
	_, err := snaprun.Parser(snaprun.Client()).ParseArgs([]string{"run", "--hook=configure", "-r=invalid", "--", "snapname"})
	c.Assert(err, check.NotNil)
	c.Check(err, check.ErrorMatches, "invalid snap revision: \"invalid\"")
}

func (s *SnapSuite) TestSnapRunHookMissingHookIntegration(c *check.C) {
	// Only create revision 42
	snaptest.MockSnapCurrent(c, string(mockYaml), &snap.SideInfo{
		Revision: snap.R(42),
	})

	// redirect exec
	called := false
	restorer := snaprun.MockSyscallExec(func(arg0 string, args []string, envv []string) error {
		called = true
		return nil
	})
	defer restorer()

	_, err := snaprun.Parser(snaprun.Client()).ParseArgs([]string{"run", "--hook=missing-hook", "--", "snapname"})
	c.Assert(err, check.ErrorMatches, `cannot find hook "missing-hook" in "snapname"`)
	c.Check(called, check.Equals, false)
}

func (s *SnapSuite) TestSnapRunErorsForUnknownRunArg(c *check.C) {
	_, err := snaprun.Parser(snaprun.Client()).ParseArgs([]string{"run", "--unknown", "--", "snapname.app", "--arg1", "arg2"})
	c.Assert(err, check.ErrorMatches, "unknown flag `unknown'")
}

func (s *SnapSuite) TestSnapRunErorsForMissingApp(c *check.C) {
	_, err := snaprun.Parser(snaprun.Client()).ParseArgs([]string{"run", "--command=shell"})
	c.Assert(err, check.ErrorMatches, "need the application to run as argument")
}

func (s *SnapSuite) TestSnapRunErorrForUnavailableApp(c *check.C) {
	_, err := snaprun.Parser(snaprun.Client()).ParseArgs([]string{"run", "--", "not-there"})
	c.Assert(err, check.ErrorMatches, fmt.Sprintf("cannot find current revision for snap not-there: readlink %s/not-there/current: no such file or directory", dirs.SnapMountDir))
}

func (s *SnapSuite) TestSnapRunSaneEnvironmentHandling(c *check.C) {
	defer mockSnapConfine(dirs.DistroLibExecDir)()

	// mock installed snap
	snaptest.MockSnapCurrent(c, string(mockYaml), &snap.SideInfo{
		Revision: snap.R(42),
	})

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
	rest, err := snaprun.Parser(snaprun.Client()).ParseArgs([]string{"run", "--", "snapname.app", "--arg1", "arg2"})
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
		{filepath.Join(dirs.SnapMountDir, dirs.CoreLibExecDir, "snapd"), true},
		{filepath.Join(dirs.DistroLibExecDir, "snapd"), false},
	} {
		osReadlinkResult = t.readlink
		c.Check(snaprun.IsReexeced(), check.Equals, t.expected)
	}
}

func (s *SnapSuite) TestSnapRunAppIntegrationFromCore(c *check.C) {
	defer mockSnapConfine(filepath.Join(dirs.SnapMountDir, "core", "111", dirs.CoreLibExecDir))()

	// mock installed snap
	snaptest.MockSnapCurrent(c, string(mockYaml), &snap.SideInfo{
		Revision: snap.R("x2"),
	})

	// pretend to be running from core
	restorer := snaprun.MockOsReadlink(func(string) (string, error) {
		return filepath.Join(dirs.SnapMountDir, "core/111/usr/bin/snap"), nil
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
	rest, err := snaprun.Parser(snaprun.Client()).ParseArgs([]string{"run", "--", "snapname.app", "--arg1", "arg2"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{"snapname.app", "--arg1", "arg2"})
	c.Check(execArg0, check.Equals, filepath.Join(dirs.SnapMountDir, "/core/111", dirs.CoreLibExecDir, "snap-confine"))
	c.Check(execArgs, check.DeepEquals, []string{
		filepath.Join(dirs.SnapMountDir, "/core/111", dirs.CoreLibExecDir, "snap-confine"),
		"snap.snapname.app",
		filepath.Join(dirs.CoreLibExecDir, "snap-exec"),
		"snapname.app", "--arg1", "arg2"})
	c.Check(execEnv, testutil.Contains, "SNAP_REVISION=x2")
}

func (s *SnapSuite) TestSnapRunAppIntegrationFromSnapd(c *check.C) {
	defer mockSnapConfine(filepath.Join(dirs.SnapMountDir, "snapd", "222", dirs.CoreLibExecDir))()

	// mock installed snap
	snaptest.MockSnapCurrent(c, string(mockYaml), &snap.SideInfo{
		Revision: snap.R("x2"),
	})

	// pretend to be running from snapd
	restorer := snaprun.MockOsReadlink(func(string) (string, error) {
		return filepath.Join(dirs.SnapMountDir, "snapd/222/usr/bin/snap"), nil
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
	rest, err := snaprun.Parser(snaprun.Client()).ParseArgs([]string{"run", "--", "snapname.app", "--arg1", "arg2"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{"snapname.app", "--arg1", "arg2"})
	c.Check(execArg0, check.Equals, filepath.Join(dirs.SnapMountDir, "/snapd/222", dirs.CoreLibExecDir, "snap-confine"))
	c.Check(execArgs, check.DeepEquals, []string{
		filepath.Join(dirs.SnapMountDir, "/snapd/222", dirs.CoreLibExecDir, "snap-confine"),
		"snap.snapname.app",
		filepath.Join(dirs.CoreLibExecDir, "snap-exec"),
		"snapname.app", "--arg1", "arg2"})
	c.Check(execEnv, testutil.Contains, "SNAP_REVISION=x2")
}

func (s *SnapSuite) TestSnapRunXauthorityMigration(c *check.C) {
	defer mockSnapConfine(dirs.DistroLibExecDir)()

	u, err := user.Current()
	c.Assert(err, check.IsNil)

	// Ensure XDG_RUNTIME_DIR exists for the user we're testing with
	err = os.MkdirAll(filepath.Join(dirs.XdgRuntimeDirBase, u.Uid), 0700)
	c.Assert(err, check.IsNil)

	// mock installed snap; happily this also gives us a directory
	// below /tmp which the Xauthority migration expects.
	snaptest.MockSnapCurrent(c, string(mockYaml), &snap.SideInfo{
		Revision: snap.R("x2"),
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
	rest, err := snaprun.Parser(snaprun.Client()).ParseArgs([]string{"run", "--", "snapname.app"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{"snapname.app"})
	c.Check(execArg0, check.Equals, filepath.Join(dirs.DistroLibExecDir, "snap-confine"))
	c.Check(execArgs, check.DeepEquals, []string{
		filepath.Join(dirs.DistroLibExecDir, "snap-confine"),
		"snap.snapname.app",
		filepath.Join(dirs.CoreLibExecDir, "snap-exec"),
		"snapname.app"})

	expectedXauthPath := filepath.Join(dirs.XdgRuntimeDirBase, u.Uid, ".Xauthority")
	c.Check(execEnv, testutil.Contains, fmt.Sprintf("XAUTHORITY=%s", expectedXauthPath))

	info, err := os.Stat(expectedXauthPath)
	c.Assert(err, check.IsNil)
	c.Assert(info.Mode().Perm(), check.Equals, os.FileMode(0600))

	err = x11.ValidateXauthorityFile(expectedXauthPath)
	c.Assert(err, check.IsNil)
}

// build the args for a hypothetical completer
func mkCompArgs(compPoint string, argv ...string) []string {
	out := []string{
		"99", // COMP_TYPE
		"99", // COMP_KEY
		"",   // COMP_POINT
		"2",  // COMP_CWORD
		" ",  // COMP_WORDBREAKS
	}
	out[2] = compPoint
	out = append(out, strings.Join(argv, " "))
	out = append(out, argv...)
	return out
}

func (s *SnapSuite) TestAntialiasHappy(c *check.C) {
	c.Assert(os.MkdirAll(dirs.SnapBinariesDir, 0755), check.IsNil)

	inArgs := mkCompArgs("10", "alias", "alias", "bo-alias")

	// first not so happy because no alias symlink
	app, outArgs := snaprun.Antialias("alias", inArgs)
	c.Check(app, check.Equals, "alias")
	c.Check(outArgs, check.DeepEquals, inArgs)

	c.Assert(os.Symlink("an-app", filepath.Join(dirs.SnapBinariesDir, "alias")), check.IsNil)

	// now really happy
	app, outArgs = snaprun.Antialias("alias", inArgs)
	c.Check(app, check.Equals, "an-app")
	c.Check(outArgs, check.DeepEquals, []string{
		"99", // COMP_TYPE (no change)
		"99", // COMP_KEY (no change)
		"11", // COMP_POINT (+1 because "an-app" is one longer than "alias")
		"2",  // COMP_CWORD (no change)
		" ",  // COMP_WORDBREAKS (no change)
		"an-app alias bo-alias", // COMP_LINE (argv[0] changed)
		"an-app",                // argv (arv[0] changed)
		"alias",
		"bo-alias",
	})
}

func (s *SnapSuite) TestAntialiasBailsIfUnhappy(c *check.C) {
	// alias exists but args are somehow wonky
	c.Assert(os.MkdirAll(dirs.SnapBinariesDir, 0755), check.IsNil)
	c.Assert(os.Symlink("an-app", filepath.Join(dirs.SnapBinariesDir, "alias")), check.IsNil)

	// weird1 has COMP_LINE not start with COMP_WORDS[0], argv[0] equal to COMP_WORDS[0]
	weird1 := mkCompArgs("6", "alias", "")
	weird1[5] = "xxxxx "
	// weird2 has COMP_LINE not start with COMP_WORDS[0], argv[0] equal to the first word in COMP_LINE
	weird2 := mkCompArgs("6", "xxxxx", "")
	weird2[5] = "alias "

	for desc, inArgs := range map[string][]string{
		"nil args":                                               nil,
		"too-short args":                                         {"alias"},
		"COMP_POINT not a number":                                mkCompArgs("hello", "alias"),
		"COMP_POINT is inside argv[0]":                           mkCompArgs("2", "alias", ""),
		"COMP_POINT is outside argv":                             mkCompArgs("99", "alias", ""),
		"COMP_WORDS[0] is not argv[0]":                           mkCompArgs("10", "not-alias", ""),
		"mismatch between argv[0], COMP_LINE and COMP_WORDS, #1": weird1,
		"mismatch between argv[0], COMP_LINE and COMP_WORDS, #2": weird2,
	} {
		// antialias leaves args alone if it's too short
		app, outArgs := snaprun.Antialias("alias", inArgs)
		c.Check(app, check.Equals, "alias", check.Commentf(desc))
		c.Check(outArgs, check.DeepEquals, inArgs, check.Commentf(desc))
	}
}

func (s *SnapSuite) TestSnapRunAppWithStraceIntegration(c *check.C) {
	defer mockSnapConfine(dirs.DistroLibExecDir)()

	// mock installed snap
	snaptest.MockSnapCurrent(c, string(mockYaml), &snap.SideInfo{
		Revision: snap.R("x2"),
	})

	// pretend we have sudo and simulate some useful output that would
	// normally come from strace
	sudoCmd := testutil.MockCommand(c, "sudo", fmt.Sprintf(`
echo "stdout output 1"
>&2 echo 'execve("/path/to/snap-confine")'
>&2 echo "snap-confine/snap-exec strace stuff"
>&2 echo "getuid() = 1000"
>&2 echo 'execve("%s/snapName/x2/bin/foo")'
>&2 echo "interessting strace output"
>&2 echo "and more"
echo "stdout output 2"
`, dirs.SnapMountDir))
	defer sudoCmd.Restore()

	// pretend we have strace
	straceCmd := testutil.MockCommand(c, "strace", "")
	defer straceCmd.Restore()

	user, err := user.Current()
	c.Assert(err, check.IsNil)

	// and run it under strace
	rest, err := snaprun.Parser(snaprun.Client()).ParseArgs([]string{"run", "--strace", "--", "snapname.app", "--arg1", "arg2"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{"snapname.app", "--arg1", "arg2"})
	c.Check(sudoCmd.Calls(), check.DeepEquals, [][]string{
		{
			"sudo", "-E",
			filepath.Join(straceCmd.BinDir(), "strace"),
			"-u", user.Username,
			"-f",
			"-e", "!select,pselect6,_newselect,clock_gettime,sigaltstack,gettid,gettimeofday,nanosleep",
			filepath.Join(dirs.DistroLibExecDir, "snap-confine"),
			"snap.snapname.app",
			filepath.Join(dirs.CoreLibExecDir, "snap-exec"),
			"snapname.app", "--arg1", "arg2",
		},
	})
	c.Check(s.Stdout(), check.Equals, "stdout output 1\nstdout output 2\n")
	c.Check(s.Stderr(), check.Equals, fmt.Sprintf("execve(%q)\ninteressting strace output\nand more\n", filepath.Join(dirs.SnapMountDir, "snapName/x2/bin/foo")))

	s.ResetStdStreams()
	sudoCmd.ForgetCalls()

	// try again without filtering
	rest, err = snaprun.Parser(snaprun.Client()).ParseArgs([]string{"run", "--strace=--raw", "--", "snapname.app", "--arg1", "arg2"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{"snapname.app", "--arg1", "arg2"})
	c.Check(sudoCmd.Calls(), check.DeepEquals, [][]string{
		{
			"sudo", "-E",
			filepath.Join(straceCmd.BinDir(), "strace"),
			"-u", user.Username,
			"-f",
			"-e", "!select,pselect6,_newselect,clock_gettime,sigaltstack,gettid,gettimeofday,nanosleep",
			filepath.Join(dirs.DistroLibExecDir, "snap-confine"),
			"snap.snapname.app",
			filepath.Join(dirs.CoreLibExecDir, "snap-exec"),
			"snapname.app", "--arg1", "arg2",
		},
	})
	c.Check(s.Stdout(), check.Equals, "stdout output 1\nstdout output 2\n")
	expectedFullFmt := `execve("/path/to/snap-confine")
snap-confine/snap-exec strace stuff
getuid() = 1000
execve("%s/snapName/x2/bin/foo")
interessting strace output
and more
`
	c.Check(s.Stderr(), check.Equals, fmt.Sprintf(expectedFullFmt, dirs.SnapMountDir))
}

func (s *SnapSuite) TestSnapRunAppWithStraceOptions(c *check.C) {
	defer mockSnapConfine(dirs.DistroLibExecDir)()

	// mock installed snap
	snaptest.MockSnapCurrent(c, string(mockYaml), &snap.SideInfo{
		Revision: snap.R("x2"),
	})

	// pretend we have sudo
	sudoCmd := testutil.MockCommand(c, "sudo", "")
	defer sudoCmd.Restore()

	// pretend we have strace
	straceCmd := testutil.MockCommand(c, "strace", "")
	defer straceCmd.Restore()

	user, err := user.Current()
	c.Assert(err, check.IsNil)

	// and run it under strace
	rest, err := snaprun.Parser(snaprun.Client()).ParseArgs([]string{"run", `--strace=-tt --raw -o "file with spaces"`, "--", "snapname.app", "--arg1", "arg2"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{"snapname.app", "--arg1", "arg2"})
	c.Check(sudoCmd.Calls(), check.DeepEquals, [][]string{
		{
			"sudo", "-E",
			filepath.Join(straceCmd.BinDir(), "strace"),
			"-u", user.Username,
			"-f",
			"-e", "!select,pselect6,_newselect,clock_gettime,sigaltstack,gettid,gettimeofday,nanosleep",
			"-tt",
			"-o",
			"file with spaces",
			filepath.Join(dirs.DistroLibExecDir, "snap-confine"),
			"snap.snapname.app",
			filepath.Join(dirs.CoreLibExecDir, "snap-exec"),
			"snapname.app", "--arg1", "arg2",
		},
	})
}

func (s *SnapSuite) TestSnapRunShellIntegration(c *check.C) {
	defer mockSnapConfine(dirs.DistroLibExecDir)()

	// mock installed snap
	snaptest.MockSnapCurrent(c, string(mockYaml), &snap.SideInfo{
		Revision: snap.R("x2"),
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

	// and run it!
	rest, err := snaprun.Parser(snaprun.Client()).ParseArgs([]string{"run", "--shell", "--", "snapname.app", "--arg1", "arg2"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{"snapname.app", "--arg1", "arg2"})
	c.Check(execArg0, check.Equals, filepath.Join(dirs.DistroLibExecDir, "snap-confine"))
	c.Check(execArgs, check.DeepEquals, []string{
		filepath.Join(dirs.DistroLibExecDir, "snap-confine"),
		"snap.snapname.app",
		filepath.Join(dirs.CoreLibExecDir, "snap-exec"),
		"--command=shell", "snapname.app", "--arg1", "arg2"})
	c.Check(execEnv, testutil.Contains, "SNAP_REVISION=x2")
}

func (s *SnapSuite) TestSnapRunAppTimer(c *check.C) {
	defer mockSnapConfine(dirs.DistroLibExecDir)()

	// mock installed snap
	snaptest.MockSnapCurrent(c, string(mockYaml), &snap.SideInfo{
		Revision: snap.R("x2"),
	})

	// redirect exec
	execArg0 := ""
	execArgs := []string{}
	execCalled := false
	restorer := snaprun.MockSyscallExec(func(arg0 string, args []string, envv []string) error {
		execArg0 = arg0
		execArgs = args
		execCalled = true
		return nil
	})
	defer restorer()

	fakeNow := time.Date(2018, 02, 12, 9, 55, 0, 0, time.Local)
	restorer = snaprun.MockTimeNow(func() time.Time {
		// Monday Feb 12, 9:55
		return fakeNow
	})
	defer restorer()

	// pretend we are outside of timer range
	rest, err := snaprun.Parser(snaprun.Client()).ParseArgs([]string{"run", `--timer="mon,10:00~12:00,,fri,13:00"`, "--", "snapname.app", "--arg1", "arg2"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{"snapname.app", "--arg1", "arg2"})
	c.Assert(execCalled, check.Equals, false)

	c.Check(s.Stderr(), check.Equals, fmt.Sprintf(`%s: attempted to run "snapname.app" timer outside of scheduled time "mon,10:00~12:00,,fri,13:00"
`, fakeNow.Format(time.RFC3339)))
	s.ResetStdStreams()

	restorer = snaprun.MockTimeNow(func() time.Time {
		// Monday Feb 12, 10:20
		return time.Date(2018, 02, 12, 10, 20, 0, 0, time.Local)
	})
	defer restorer()

	// and run it under strace
	_, err = snaprun.Parser(snaprun.Client()).ParseArgs([]string{"run", `--timer="mon,10:00~12:00,,fri,13:00"`, "--", "snapname.app", "--arg1", "arg2"})
	c.Assert(err, check.IsNil)
	c.Assert(execCalled, check.Equals, true)
	c.Check(execArg0, check.Equals, filepath.Join(dirs.DistroLibExecDir, "snap-confine"))
	c.Check(execArgs, check.DeepEquals, []string{
		filepath.Join(dirs.DistroLibExecDir, "snap-confine"),
		"snap.snapname.app",
		filepath.Join(dirs.CoreLibExecDir, "snap-exec"),
		"snapname.app", "--arg1", "arg2"})
}

// generated with:
//  sudo /usr/lib/snapd/snap-discard-ns test-snapd-tools && sudo strace -u $USER -o strace.log -f -e trace=execve,execveat -ttt test-snapd-tools.echo foo && cat strace.log
var sampleStraceSimple = []byte(`
21616 1542882400.198907 execve("/snap/bin/test-snapd-tools.echo", ["test-snapd-tools.echo", "foo"], 0x7fff7f275f48 /* 27 vars */) = 0
21616 1542882400.204710 execve("/snap/core/current/usr/bin/snap", ["test-snapd-tools.echo", "foo"], 0xc42011c8c0 /* 27 vars */ <unfinished ...>
21621 1542882400.204845 +++ exited with 0 +++
21620 1542882400.204853 +++ exited with 0 +++
21619 1542882400.204857 +++ exited with 0 +++
21618 1542882400.204861 +++ exited with 0 +++
21617 1542882400.204875 +++ exited with 0 +++
21616 1542882400.205199 <... execve resumed> ) = 0
21616 1542882400.220845 execve("/snap/core/5976/usr/lib/snapd/snap-confine", ["/snap/core/5976/usr/lib/snapd/sn"..., "snap.test-snapd-tools.echo", "/usr/lib/snapd/snap-exec", "test-snapd-tools.echo", "foo"], 0xc8200a3600 /* 41 vars */ <unfinished ...>
21625 1542882400.220994 +++ exited with 0 +++
21624 1542882400.220999 +++ exited with 0 +++
21623 1542882400.221002 +++ exited with 0 +++
21622 1542882400.221005 +++ exited with 0 +++
21616 1542882400.221634 <... execve resumed> ) = 0
21629 1542882400.356625 execveat(3, "", ["snap-update-ns", "--from-snap-confine", "test-snapd-tools"], 0x7ffeaf4faa40 /* 0 vars */, AT_EMPTY_PATH) = 0
21631 1542882400.360708 +++ exited with 0 +++
21632 1542882400.360723 +++ exited with 0 +++
21630 1542882400.360727 +++ exited with 0 +++
21633 1542882400.360842 +++ exited with 0 +++
21629 1542882400.360848 +++ exited with 0 +++
21616 1542882400.360869 --- SIGCHLD {si_signo=SIGCHLD, si_code=CLD_EXITED, si_pid=21629, si_uid=1000, si_status=0, si_utime=0, si_stime=0} ---
21626 1542882400.375793 +++ exited with 0 +++
21616 1542882400.375836 --- SIGCHLD {si_signo=SIGCHLD, si_code=CLD_EXITED, si_pid=21626, si_uid=1000, si_status=0, si_utime=0, si_stime=0} ---
21616 1542882400.377349 execve("/usr/lib/snapd/snap-exec", ["/usr/lib/snapd/snap-exec", "test-snapd-tools.echo", "foo"], 0x23ebc80 /* 45 vars */) = 0
21616 1542882400.383698 execve("/snap/test-snapd-tools/6/bin/echo", ["/snap/test-snapd-tools/6/bin/ech"..., "foo"], 0xc420072f00 /* 47 vars */ <unfinished ...>
21638 1542882400.383855 +++ exited with 0 +++
21637 1542882400.383862 +++ exited with 0 +++
21636 1542882400.383877 +++ exited with 0 +++
21634 1542882400.383884 +++ exited with 0 +++
21635 1542882400.383890 +++ exited with 0 +++
21616 1542882400.384105 <... execve resumed> ) = 0
21616 1542882400.384974 +++ exited with 0 +++
`)

func (s *SnapSuite) TestStraceExtractExecRuntime(c *check.C) {
	f, err := ioutil.TempFile("", "strace-extract-test-")
	c.Assert(err, check.IsNil)
	defer os.Remove(f.Name())
	_, err = f.Write(sampleStraceSimple)
	c.Assert(err, check.IsNil)
	f.Sync()

	st, err := snaprun.StraceExtractExecRuntime(f.Name())
	c.Assert(err, check.IsNil)
	c.Assert(st.ExecRuntimes(), check.DeepEquals, []snaprun.ExecRuntime{
		{Exe: "/snap/bin/test-snapd-tools.echo", TotalSec: 0.005803108215332031},
		{Exe: "/snap/core/current/usr/bin/snap", TotalSec: 0.016134977340698242},
		{Exe: "snap-update-ns", TotalSec: 0.0042438507080078125},
		{Exe: "/snap/core/5976/usr/lib/snapd/snap-confine", TotalSec: 0.15650391578674316},
		{Exe: "/usr/lib/snapd/snap-exec", TotalSec: 0.006349086761474609},
	})
}

func (s *SnapSuite) TestDisplayExecRuntimes(c *check.C) {
	// setup mock traces
	stt := &snaprun.SnapTrace{TotalTime: 2.71828}
	stt.SetNrSamples(3)
	stt.AddExecRuntime("slow", 1.0001)
	stt.AddExecRuntime("fast", 0.1002)
	stt.AddExecRuntime("really-fast", 0.0301)
	stt.AddExecRuntime("medium", 0.5003)

	// display works and shows the slowest 3 calls in order
	stt.Display(snaprun.Stderr)
	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, `Slowest 3 exec calls during snap run:
  1.000s slow
  0.100s fast
  0.500s medium
Total time: 2.718s
`)
}

func (s *SnapSuite) TestExecRuntimePrune(c *check.C) {
	stt := &snaprun.SnapTrace{}
	stt.SetNrSamples(3)

	// simple cases
	stt.AddExecRuntime("t0", 2)
	c.Check(stt.ExecRuntimes(), check.HasLen, 1)
	stt.AddExecRuntime("t1", 1)
	c.Check(stt.ExecRuntimes(), check.HasLen, 2)
	stt.AddExecRuntime("t2", 5)
	c.Check(stt.ExecRuntimes(), check.HasLen, 3)

	// starts pruing the fastest call, keeps order otherwise
	stt.AddExecRuntime("t3", 4)
	c.Check(stt.ExecRuntimes(), check.HasLen, 3)
	c.Check(stt.ExecRuntimes(), check.DeepEquals, []snaprun.ExecRuntime{
		{Exe: "t0", TotalSec: 2},
		{Exe: "t2", TotalSec: 5},
		{Exe: "t3", TotalSec: 4},
	})

}

func (s *SnapSuite) TestRunCmdWithTraceExecUnhappy(c *check.C) {
	defer mockSnapConfine(dirs.DistroLibExecDir)()

	// mock installed snap
	snaptest.MockSnapCurrent(c, string(mockYaml), &snap.SideInfo{
		Revision: snap.R("1"),
	})

	// pretend we have sudo
	sudoCmd := testutil.MockCommand(c, "sudo", "echo unhappy; exit 12")
	defer sudoCmd.Restore()

	// pretend we have strace
	straceCmd := testutil.MockCommand(c, "strace", "")
	defer straceCmd.Restore()

	rest, err := snaprun.Parser(snaprun.Client()).ParseArgs([]string{"run", "--trace-exec", "--", "snapname.app", "--arg1", "arg2"})
	c.Assert(err, check.ErrorMatches, "exit status 12")
	c.Assert(rest, check.DeepEquals, []string{"--", "snapname.app", "--arg1", "arg2"})
	c.Check(s.Stdout(), check.Equals, "unhappy\n")
	c.Check(s.Stderr(), check.Equals, "")
}
