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
	"errors"
	"fmt"
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
	"github.com/snapcore/snapd/sandbox/selinux"
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

var mockYamlBaseNone1 = []byte(`name: snapname1
version: 1.0
base: none
apps:
 app:
  command: run-app
`)

var mockYamlBaseNone2 = []byte(`name: snapname2
version: 1.0
base: none
hooks:
 configure:
`)

type RunSuite struct {
	fakeHome string
	BaseSnapSuite
}

var _ = check.Suite(&RunSuite{})

func (s *RunSuite) SetUpTest(c *check.C) {
	s.BaseSnapSuite.SetUpTest(c)
	s.fakeHome = c.MkDir()

	u, err := user.Current()
	c.Assert(err, check.IsNil)
	s.AddCleanup(snaprun.MockUserCurrent(func() (*user.User, error) {
		return &user.User{Uid: u.Uid, HomeDir: s.fakeHome}, nil
	}))
}

func (s *RunSuite) TestInvalidParameters(c *check.C) {
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

func (s *RunSuite) TestRunCmdWithBaseNone(c *check.C) {
	defer mockSnapConfine(dirs.DistroLibExecDir)()

	// mock installed snap
	snaptest.MockSnapCurrent(c, string(mockYamlBaseNone1), &snap.SideInfo{
		Revision: snap.R("1"),
	})
	snaptest.MockSnapCurrent(c, string(mockYamlBaseNone2), &snap.SideInfo{
		Revision: snap.R("1"),
	})

	_, err := snaprun.Parser(snaprun.Client()).ParseArgs([]string{"run", "--", "snapname1.app", "--arg1", "arg2"})
	c.Assert(err, check.ErrorMatches, `cannot run hooks / applications with base \"none\"`)

	_, err = snaprun.Parser(snaprun.Client()).ParseArgs([]string{"run", "--hook=configure", "--", "snapname2"})
	c.Assert(err, check.ErrorMatches, `cannot run hooks / applications with base \"none\"`)
}

func (s *RunSuite) TestSnapRunWhenMissingConfine(c *check.C) {
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

func (s *RunSuite) TestSnapRunAppIntegration(c *check.C) {
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

func (s *RunSuite) TestSnapRunClassicAppIntegration(c *check.C) {
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

func (s *RunSuite) TestSnapRunClassicAppIntegrationReexecedFromCore(c *check.C) {
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

func (s *RunSuite) TestSnapRunClassicAppIntegrationReexecedFromSnapd(c *check.C) {
	mountedSnapdPath := filepath.Join(dirs.SnapMountDir, "snapd/current")
	mountedSnapdLibExecPath := filepath.Join(mountedSnapdPath, dirs.CoreLibExecDir)

	defer mockSnapConfine(mountedSnapdLibExecPath)()

	// mock installed snap
	snaptest.MockSnapCurrent(c, string(mockYaml)+"confinement: classic\n", &snap.SideInfo{
		Revision: snap.R("x2"),
	})

	restore := snaprun.MockOsReadlink(func(name string) (string, error) {
		// pretend 'snap' is reexeced from 'core'
		return filepath.Join(mountedSnapdPath, "usr/bin/snap"), nil
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
		filepath.Join(mountedSnapdLibExecPath, "snap-confine"), "--classic",
		"snap.snapname.app",
		filepath.Join(mountedSnapdLibExecPath, "snap-exec"),
		"snapname.app", "--arg1", "arg2"})
}

func (s *RunSuite) TestSnapRunAppWithCommandIntegration(c *check.C) {
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

func (s *RunSuite) TestSnapRunCreateDataDirs(c *check.C) {
	info, err := snap.InfoFromSnapYaml(mockYaml)
	c.Assert(err, check.IsNil)
	info.SideInfo.Revision = snap.R(42)

	err = snaprun.CreateUserDataDirs(info)
	c.Assert(err, check.IsNil)
	c.Check(osutil.FileExists(filepath.Join(s.fakeHome, "/snap/snapname/42")), check.Equals, true)
	c.Check(osutil.FileExists(filepath.Join(s.fakeHome, "/snap/snapname/common")), check.Equals, true)
}

func (s *RunSuite) TestParallelInstanceSnapRunCreateDataDirs(c *check.C) {
	info, err := snap.InfoFromSnapYaml(mockYaml)
	c.Assert(err, check.IsNil)
	info.SideInfo.Revision = snap.R(42)
	info.InstanceKey = "foo"

	err = snaprun.CreateUserDataDirs(info)
	c.Assert(err, check.IsNil)
	c.Check(osutil.FileExists(filepath.Join(s.fakeHome, "/snap/snapname_foo/42")), check.Equals, true)
	c.Check(osutil.FileExists(filepath.Join(s.fakeHome, "/snap/snapname_foo/common")), check.Equals, true)
	// mount point for snap instance mapping has been created
	c.Check(osutil.FileExists(filepath.Join(s.fakeHome, "/snap/snapname")), check.Equals, true)
	// and it's empty inside
	m, err := filepath.Glob(filepath.Join(s.fakeHome, "/snap/snapname/*"))
	c.Assert(err, check.IsNil)
	c.Assert(m, check.HasLen, 0)
}

func (s *RunSuite) TestSnapRunHookIntegration(c *check.C) {
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

func (s *RunSuite) TestSnapRunHookUnsetRevisionIntegration(c *check.C) {
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

func (s *RunSuite) TestSnapRunHookSpecificRevisionIntegration(c *check.C) {
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

func (s *RunSuite) TestSnapRunHookMissingRevisionIntegration(c *check.C) {
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

func (s *RunSuite) TestSnapRunHookInvalidRevisionIntegration(c *check.C) {
	_, err := snaprun.Parser(snaprun.Client()).ParseArgs([]string{"run", "--hook=configure", "-r=invalid", "--", "snapname"})
	c.Assert(err, check.NotNil)
	c.Check(err, check.ErrorMatches, "invalid snap revision: \"invalid\"")
}

func (s *RunSuite) TestSnapRunHookMissingHookIntegration(c *check.C) {
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

func (s *RunSuite) TestSnapRunErorsForUnknownRunArg(c *check.C) {
	_, err := snaprun.Parser(snaprun.Client()).ParseArgs([]string{"run", "--unknown", "--", "snapname.app", "--arg1", "arg2"})
	c.Assert(err, check.ErrorMatches, "unknown flag `unknown'")
}

func (s *RunSuite) TestSnapRunErorsForMissingApp(c *check.C) {
	_, err := snaprun.Parser(snaprun.Client()).ParseArgs([]string{"run", "--command=shell"})
	c.Assert(err, check.ErrorMatches, "need the application to run as argument")
}

func (s *RunSuite) TestSnapRunErorrForUnavailableApp(c *check.C) {
	_, err := snaprun.Parser(snaprun.Client()).ParseArgs([]string{"run", "--", "not-there"})
	c.Assert(err, check.ErrorMatches, fmt.Sprintf("cannot find current revision for snap not-there: readlink %s/not-there/current: no such file or directory", dirs.SnapMountDir))
}

func (s *RunSuite) TestSnapRunSaneEnvironmentHandling(c *check.C) {
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

func (s *RunSuite) TestSnapRunSnapdHelperPath(c *check.C) {
	var osReadlinkResult string
	restore := snaprun.MockOsReadlink(func(name string) (string, error) {
		return osReadlinkResult, nil
	})
	defer restore()

	tool := "snap-confine"
	for _, t := range []struct {
		readlink string
		expected string
	}{
		{
			filepath.Join(dirs.SnapMountDir, "core/current/usr/bin/snap"),
			filepath.Join(dirs.SnapMountDir, "core/current", dirs.CoreLibExecDir, tool),
		},
		{
			filepath.Join(dirs.SnapMountDir, "snapd/current/usr/bin/snap"),
			filepath.Join(dirs.SnapMountDir, "snapd/current", dirs.CoreLibExecDir, tool),
		},
		{
			filepath.Join("/usr/bin/snap"),
			filepath.Join(dirs.DistroLibExecDir, tool),
		},
	} {
		osReadlinkResult = t.readlink
		toolPath, err := snaprun.SnapdHelperPath(tool)
		c.Assert(err, check.IsNil)
		c.Check(toolPath, check.Equals, t.expected)
	}
}

func (s *RunSuite) TestSnapRunAppIntegrationFromCore(c *check.C) {
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

func (s *RunSuite) TestSnapRunAppIntegrationFromSnapd(c *check.C) {
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

func (s *RunSuite) TestSnapRunXauthorityMigration(c *check.C) {
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

func (s *RunSuite) TestAntialiasHappy(c *check.C) {
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

func (s *RunSuite) TestAntialiasBailsIfUnhappy(c *check.C) {
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

func (s *RunSuite) TestSnapRunAppWithStraceIntegration(c *check.C) {
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

func (s *RunSuite) TestSnapRunAppWithStraceOptions(c *check.C) {
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

func (s *RunSuite) TestSnapRunShellIntegration(c *check.C) {
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

func (s *RunSuite) TestSnapRunAppTimer(c *check.C) {
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

func (s *RunSuite) TestRunCmdWithTraceExecUnhappy(c *check.C) {
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

func (s *RunSuite) TestSnapRunRestoreSecurityContextHappy(c *check.C) {
	logbuf, restorer := logger.MockLogger()
	defer restorer()

	defer mockSnapConfine(dirs.DistroLibExecDir)()

	// mock installed snap
	snaptest.MockSnapCurrent(c, string(mockYaml), &snap.SideInfo{
		Revision: snap.R("x2"),
	})

	// redirect exec
	execCalled := 0
	restorer = snaprun.MockSyscallExec(func(_ string, args []string, envv []string) error {
		execCalled++
		return nil
	})
	defer restorer()

	verifyCalls := 0
	restoreCalls := 0
	isEnabledCalls := 0
	enabled := false
	verify := true

	snapUserDir := filepath.Join(s.fakeHome, dirs.UserHomeSnapDir)

	restorer = snaprun.MockSELinuxVerifyPathContext(func(what string) (bool, error) {
		c.Check(what, check.Equals, snapUserDir)
		verifyCalls++
		return verify, nil
	})
	defer restorer()

	restorer = snaprun.MockSELinuxRestoreContext(func(what string, mode selinux.RestoreMode) error {
		c.Check(mode, check.Equals, selinux.RestoreMode{Recursive: true})
		c.Check(what, check.Equals, snapUserDir)
		restoreCalls++
		return nil
	})
	defer restorer()

	restorer = snaprun.MockSELinuxIsEnabled(func() (bool, error) {
		isEnabledCalls++
		return enabled, nil
	})
	defer restorer()

	_, err := snaprun.Parser(snaprun.Client()).ParseArgs([]string{"run", "--", "snapname.app"})
	c.Assert(err, check.IsNil)
	c.Check(execCalled, check.Equals, 1)
	c.Check(isEnabledCalls, check.Equals, 1)
	c.Check(verifyCalls, check.Equals, 0)
	c.Check(restoreCalls, check.Equals, 0)

	// pretend SELinux is on
	enabled = true

	_, err = snaprun.Parser(snaprun.Client()).ParseArgs([]string{"run", "--", "snapname.app"})
	c.Assert(err, check.IsNil)
	c.Check(execCalled, check.Equals, 2)
	c.Check(isEnabledCalls, check.Equals, 2)
	c.Check(verifyCalls, check.Equals, 1)
	c.Check(restoreCalls, check.Equals, 0)

	// pretend the context does not match
	verify = false

	logbuf.Reset()

	_, err = snaprun.Parser(snaprun.Client()).ParseArgs([]string{"run", "--", "snapname.app"})
	c.Assert(err, check.IsNil)
	c.Check(execCalled, check.Equals, 3)
	c.Check(isEnabledCalls, check.Equals, 3)
	c.Check(verifyCalls, check.Equals, 2)
	c.Check(restoreCalls, check.Equals, 1)

	// and we let the user know what we're doing
	c.Check(logbuf.String(), testutil.Contains, fmt.Sprintf("restoring default SELinux context of %s", snapUserDir))
}

func (s *RunSuite) TestSnapRunRestoreSecurityContextFail(c *check.C) {
	logbuf, restorer := logger.MockLogger()
	defer restorer()

	defer mockSnapConfine(dirs.DistroLibExecDir)()

	// mock installed snap
	snaptest.MockSnapCurrent(c, string(mockYaml), &snap.SideInfo{
		Revision: snap.R("x2"),
	})

	// redirect exec
	execCalled := 0
	restorer = snaprun.MockSyscallExec(func(_ string, args []string, envv []string) error {
		execCalled++
		return nil
	})
	defer restorer()

	verifyCalls := 0
	restoreCalls := 0
	isEnabledCalls := 0
	enabledErr := errors.New("enabled failed")
	verifyErr := errors.New("verify failed")
	restoreErr := errors.New("restore failed")

	snapUserDir := filepath.Join(s.fakeHome, dirs.UserHomeSnapDir)

	restorer = snaprun.MockSELinuxVerifyPathContext(func(what string) (bool, error) {
		c.Check(what, check.Equals, snapUserDir)
		verifyCalls++
		return false, verifyErr
	})
	defer restorer()

	restorer = snaprun.MockSELinuxRestoreContext(func(what string, mode selinux.RestoreMode) error {
		c.Check(mode, check.Equals, selinux.RestoreMode{Recursive: true})
		c.Check(what, check.Equals, snapUserDir)
		restoreCalls++
		return restoreErr
	})
	defer restorer()

	restorer = snaprun.MockSELinuxIsEnabled(func() (bool, error) {
		isEnabledCalls++
		return enabledErr == nil, enabledErr
	})
	defer restorer()

	_, err := snaprun.Parser(snaprun.Client()).ParseArgs([]string{"run", "--", "snapname.app"})
	// these errors are only logged, but we still run the snap
	c.Assert(err, check.IsNil)
	c.Check(execCalled, check.Equals, 1)
	c.Check(logbuf.String(), testutil.Contains, "cannot determine SELinux status: enabled failed")
	c.Check(isEnabledCalls, check.Equals, 1)
	c.Check(verifyCalls, check.Equals, 0)
	c.Check(restoreCalls, check.Equals, 0)
	// pretend selinux is on
	enabledErr = nil

	logbuf.Reset()

	_, err = snaprun.Parser(snaprun.Client()).ParseArgs([]string{"run", "--", "snapname.app"})
	c.Assert(err, check.IsNil)
	c.Check(execCalled, check.Equals, 2)
	c.Check(logbuf.String(), testutil.Contains, fmt.Sprintf("failed to verify SELinux context of %s: verify failed", snapUserDir))
	c.Check(isEnabledCalls, check.Equals, 2)
	c.Check(verifyCalls, check.Equals, 1)
	c.Check(restoreCalls, check.Equals, 0)

	// pretend the context does not match
	verifyErr = nil

	logbuf.Reset()

	_, err = snaprun.Parser(snaprun.Client()).ParseArgs([]string{"run", "--", "snapname.app"})
	c.Assert(err, check.IsNil)
	c.Check(execCalled, check.Equals, 3)
	c.Check(logbuf.String(), testutil.Contains, fmt.Sprintf("cannot restore SELinux context of %s: restore failed", snapUserDir))
	c.Check(isEnabledCalls, check.Equals, 3)
	c.Check(verifyCalls, check.Equals, 2)
	c.Check(restoreCalls, check.Equals, 1)
}
