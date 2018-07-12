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
	"os/exec"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"

	snapExec "github.com/snapcore/snapd/cmd/snap-exec"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type snapExecSuite struct{}

var _ = Suite(&snapExecSuite{})

func (s *snapExecSuite) SetUpTest(c *C) {
	// clean previous parse runs
	snapExec.SetOptsCommand("")
	snapExec.SetOptsHook("")
}

func (s *snapExecSuite) TearDown(c *C) {
	dirs.SetRootDir("/")
}

var mockYaml = []byte(`name: snapname
version: 1.0
apps:
 app:
  command: run-app cmd-arg1 $SNAP_DATA
  stop-command: stop-app
  post-stop-command: post-stop-app
  environment:
   BASE_PATH: /some/path
   LD_LIBRARY_PATH: ${BASE_PATH}/lib
   MY_PATH: $PATH
 app2:
  command: run-app2
  stop-command: stop-app2
  post-stop-command: post-stop-app2
  command-chain: [chain1, chain2]
 nostop:
  command: nostop
`)

var mockHookYaml = []byte(`name: snapname
version: 1.0
hooks:
 configure:
`)

var binaryTemplate = `#!/bin/sh
echo "$(basename $0)" >> %[1]q
for arg in "$@"; do
echo "$arg" >> %[1]q
done
printf "\n" >> %[1]q`

func (s *snapExecSuite) TestInvalidCombinedParameters(c *C) {
	invalidParameters := []string{"--hook=hook-name", "--command=command-name", "snap-name"}
	_, _, err := snapExec.ParseArgs(invalidParameters)
	c.Check(err, ErrorMatches, ".*cannot use --hook and --command together.*")
}

func (s *snapExecSuite) TestInvalidExtraParameters(c *C) {
	invalidParameters := []string{"--hook=hook-name", "snap-name", "foo", "bar"}
	_, _, err := snapExec.ParseArgs(invalidParameters)
	c.Check(err, ErrorMatches, ".*too many arguments for hook \"hook-name\": snap-name foo bar.*")
}

func (s *snapExecSuite) TestFindCommand(c *C) {
	info, err := snap.InfoFromSnapYaml(mockYaml)
	c.Assert(err, IsNil)

	for _, t := range []struct {
		app      string
		cmd      string
		expected string
	}{
		{app: "app", cmd: "", expected: "/snap/snapname/unset/run-app cmd-arg1 $SNAP_DATA"},
		{app: "app2", cmd: "", expected: "/snap/snapname/unset/chain1 /snap/snapname/unset/chain2 /snap/snapname/unset/run-app2"},
		{app: "app", cmd: "stop", expected: "/snap/snapname/unset/stop-app"},
		{app: "app2", cmd: "stop", expected: "/snap/snapname/unset/chain1 /snap/snapname/unset/chain2 /snap/snapname/unset/stop-app2"},
		{app: "app", cmd: "post-stop", expected: "/snap/snapname/unset/post-stop-app"},
		{app: "app2", cmd: "post-stop", expected: "/snap/snapname/unset/chain1 /snap/snapname/unset/chain2 /snap/snapname/unset/post-stop-app2"},
	} {
		cmd, err := snapExec.FindCommand(info.Apps[t.app], t.cmd)
		c.Check(err, IsNil)
		c.Check(cmd, Equals, t.expected)
	}
}

func (s *snapExecSuite) TestFindCommandInvalidCommand(c *C) {
	info, err := snap.InfoFromSnapYaml(mockYaml)
	c.Assert(err, IsNil)

	_, err = snapExec.FindCommand(info.Apps["app"], "xxx")
	c.Check(err, ErrorMatches, `cannot use "xxx" command`)
}

func (s *snapExecSuite) TestFindCommandNoCommand(c *C) {
	info, err := snap.InfoFromSnapYaml(mockYaml)
	c.Assert(err, IsNil)

	_, err = snapExec.FindCommand(info.Apps["nostop"], "stop")
	c.Check(err, ErrorMatches, `no "stop" command found for "nostop"`)
}

func (s *snapExecSuite) TestSnapExecAppIntegration(c *C) {
	dirs.SetRootDir(c.MkDir())
	snaptest.MockSnap(c, string(mockYaml), &snap.SideInfo{
		Revision: snap.R("42"),
	})

	execArgv0 := ""
	execArgs := []string{}
	execEnv := []string{}
	restore := snapExec.MockSyscallExec(func(argv0 string, argv []string, env []string) error {
		execArgv0 = argv0
		execArgs = argv
		execEnv = env
		return nil
	})
	defer restore()

	// launch and verify its run the right way
	err := snapExec.ExecApp("snapname.app", "42", "stop", []string{"arg1", "arg2"})
	c.Assert(err, IsNil)
	c.Check(execArgv0, Equals, fmt.Sprintf("%s/snapname/42/stop-app", dirs.SnapMountDir))
	c.Check(execArgs, DeepEquals, []string{execArgv0, "arg1", "arg2"})
	c.Check(execEnv, testutil.Contains, "BASE_PATH=/some/path")
	c.Check(execEnv, testutil.Contains, "LD_LIBRARY_PATH=/some/path/lib")
	c.Check(execEnv, testutil.Contains, fmt.Sprintf("MY_PATH=%s", os.Getenv("PATH")))
}

func (s *snapExecSuite) TestSnapExecAppCommandChainIntegration(c *C) {
	dirs.SetRootDir(c.MkDir())
	snaptest.MockSnap(c, string(mockYaml), &snap.SideInfo{
		Revision: snap.R("42"),
	})

	execArgv0 := ""
	execArgs := []string{}
	restore := snapExec.MockSyscallExec(func(argv0 string, argv []string, env []string) error {
		execArgv0 = argv0
		execArgs = argv
		return nil
	})
	defer restore()

	// launch and verify its run the right way
	err := snapExec.ExecApp("snapname.app2", "42", "", []string{"arg1", "arg2"})
	c.Assert(err, IsNil)
	c.Check(execArgv0, Equals, fmt.Sprintf("%s/snapname/42/chain1", dirs.SnapMountDir))
	c.Check(execArgs, DeepEquals, []string{
		execArgv0,
		fmt.Sprintf("%s/snapname/42/chain2", dirs.SnapMountDir),
		fmt.Sprintf("%s/snapname/42/run-app2", dirs.SnapMountDir),
		"arg1", "arg2",
	})
}

func (s *snapExecSuite) TestSnapExecHookIntegration(c *C) {
	dirs.SetRootDir(c.MkDir())
	snaptest.MockSnap(c, string(mockHookYaml), &snap.SideInfo{
		Revision: snap.R("42"),
	})

	execArgv0 := ""
	execArgs := []string{}
	restore := snapExec.MockSyscallExec(func(argv0 string, argv []string, env []string) error {
		execArgv0 = argv0
		execArgs = argv
		return nil
	})
	defer restore()

	// launch and verify it ran correctly
	err := snapExec.ExecHook("snapname", "42", "configure")
	c.Assert(err, IsNil)
	c.Check(execArgv0, Equals, fmt.Sprintf("%s/snapname/42/meta/hooks/configure", dirs.SnapMountDir))
	c.Check(execArgs, DeepEquals, []string{execArgv0})
}

func (s *snapExecSuite) TestSnapExecHookMissingHookIntegration(c *C) {
	dirs.SetRootDir(c.MkDir())
	snaptest.MockSnap(c, string(mockHookYaml), &snap.SideInfo{
		Revision: snap.R("42"),
	})

	err := snapExec.ExecHook("snapname", "42", "missing-hook")
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, "cannot find hook \"missing-hook\" in \"snapname\"")
}

func (s *snapExecSuite) TestSnapExecIgnoresUnknownArgs(c *C) {
	snapApp, rest, err := snapExec.ParseArgs([]string{"--command=shell", "snapname.app", "--arg1", "arg2"})
	c.Assert(err, IsNil)
	c.Assert(snapExec.GetOptsCommand(), Equals, "shell")
	c.Assert(snapApp, DeepEquals, "snapname.app")
	c.Assert(rest, DeepEquals, []string{"--arg1", "arg2"})
}

func (s *snapExecSuite) TestSnapExecErrorsOnUnknown(c *C) {
	_, _, err := snapExec.ParseArgs([]string{"--command=shell", "--unknown", "snapname.app", "--arg1", "arg2"})
	c.Check(err, ErrorMatches, "unknown flag `unknown'")
}

func (s *snapExecSuite) TestSnapExecErrorsOnMissingSnapApp(c *C) {
	_, _, err := snapExec.ParseArgs([]string{"--command=shell"})
	c.Check(err, ErrorMatches, "need the application to run as argument")
}

func (s *snapExecSuite) TestSnapExecAppRealIntegration(c *C) {
	// we need a lot of mocks
	dirs.SetRootDir(c.MkDir())

	oldOsArgs := os.Args
	defer func() { os.Args = oldOsArgs }()

	os.Setenv("SNAP_REVISION", "42")
	defer os.Unsetenv("SNAP_REVISION")

	snaptest.MockSnap(c, string(mockYaml), &snap.SideInfo{
		Revision: snap.R("42"),
	})

	canaryFile := filepath.Join(c.MkDir(), "canary.txt")
	script := fmt.Sprintf("%s/snapname/42/run-app", dirs.SnapMountDir)
	err := ioutil.WriteFile(script, []byte(fmt.Sprintf(binaryTemplate, canaryFile)), 0755)
	c.Assert(err, IsNil)

	// we can not use the real syscall.execv here because it would
	// replace the entire test :)
	restore := snapExec.MockSyscallExec(actuallyExec)
	defer restore()

	// run it
	os.Args = []string{"snap-exec", "snapname.app", "foo", "--bar=baz", "foobar"}
	err = snapExec.Run()
	c.Assert(err, IsNil)

	c.Assert(canaryFile, testutil.FileEquals, `run-app
cmd-arg1
foo
--bar=baz
foobar

`)
}

func (s *snapExecSuite) TestSnapExecHookRealIntegration(c *C) {
	// we need a lot of mocks
	dirs.SetRootDir(c.MkDir())

	oldOsArgs := os.Args
	defer func() { os.Args = oldOsArgs }()

	os.Setenv("SNAP_REVISION", "42")
	defer os.Unsetenv("SNAP_REVISION")

	canaryFile := filepath.Join(c.MkDir(), "canary.txt")

	testSnap := snaptest.MockSnap(c, string(mockHookYaml), &snap.SideInfo{
		Revision: snap.R("42"),
	})
	hookPath := filepath.Join("meta", "hooks", "configure")
	hookPathAndContents := []string{hookPath, fmt.Sprintf(binaryTemplate, canaryFile)}
	snaptest.PopulateDir(testSnap.MountDir(), [][]string{hookPathAndContents})
	hookPath = filepath.Join(testSnap.MountDir(), hookPath)
	c.Assert(os.Chmod(hookPath, 0755), IsNil)

	// we can not use the real syscall.execv here because it would
	// replace the entire test :)
	restore := snapExec.MockSyscallExec(actuallyExec)
	defer restore()

	// run it
	os.Args = []string{"snap-exec", "--hook=configure", "snapname"}
	err := snapExec.Run()
	c.Assert(err, IsNil)

	c.Assert(canaryFile, testutil.FileEquals, "configure\n\n")
}

func actuallyExec(argv0 string, argv []string, env []string) error {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = env
	output, err := cmd.CombinedOutput()
	if len(output) > 0 {
		return fmt.Errorf("Expected output length to be 0, it was %d", len(output))
	}
	return err
}

func (s *snapExecSuite) TestSnapExecShellIntegration(c *C) {
	dirs.SetRootDir(c.MkDir())
	snaptest.MockSnap(c, string(mockYaml), &snap.SideInfo{
		Revision: snap.R("42"),
	})

	execArgv0 := ""
	execArgs := []string{}
	execEnv := []string{}
	restore := snapExec.MockSyscallExec(func(argv0 string, argv []string, env []string) error {
		execArgv0 = argv0
		execArgs = argv
		execEnv = env
		return nil
	})
	defer restore()

	// launch and verify its run the right way
	err := snapExec.ExecApp("snapname.app", "42", "shell", []string{"-c", "echo foo"})
	c.Assert(err, IsNil)
	c.Check(execArgv0, Equals, "/bin/bash")
	c.Check(execArgs, DeepEquals, []string{execArgv0, "-c", "echo foo"})
	c.Check(execEnv, testutil.Contains, "LD_LIBRARY_PATH=/some/path/lib")

	// launch and verify shell still runs the command chain
	err = snapExec.ExecApp("snapname.app2", "42", "shell", []string{"-c", "echo foo"})
	c.Assert(err, IsNil)
	chain1 := fmt.Sprintf("%s/snapname/42/chain1", dirs.SnapMountDir)
	chain2 := fmt.Sprintf("%s/snapname/42/chain2", dirs.SnapMountDir)
	c.Check(execArgv0, Equals, chain1)
	c.Check(execArgs, DeepEquals, []string{chain1, chain2, "/bin/bash", "-c", "echo foo"})
}

func (s *snapExecSuite) TestSnapExecAppIntegrationWithVars(c *C) {
	dirs.SetRootDir(c.MkDir())
	snaptest.MockSnap(c, string(mockYaml), &snap.SideInfo{
		Revision: snap.R("42"),
	})

	execArgv0 := ""
	execArgs := []string{}
	execEnv := []string{}
	restore := snapExec.MockSyscallExec(func(argv0 string, argv []string, env []string) error {
		execArgv0 = argv0
		execArgs = argv
		execEnv = env
		return nil
	})
	defer restore()

	// setup env
	os.Setenv("SNAP_DATA", "/var/snap/snapname/42")
	defer os.Unsetenv("SNAP_DATA")

	// launch and verify its run the right way
	err := snapExec.ExecApp("snapname.app", "42", "", []string{"user-arg1"})
	c.Assert(err, IsNil)
	c.Check(execArgv0, Equals, fmt.Sprintf("%s/snapname/42/run-app", dirs.SnapMountDir))
	c.Check(execArgs, DeepEquals, []string{execArgv0, "cmd-arg1", "/var/snap/snapname/42", "user-arg1"})
	c.Check(execEnv, testutil.Contains, "BASE_PATH=/some/path")
	c.Check(execEnv, testutil.Contains, "LD_LIBRARY_PATH=/some/path/lib")
	c.Check(execEnv, testutil.Contains, fmt.Sprintf("MY_PATH=%s", os.Getenv("PATH")))
}

func (s *snapExecSuite) TestSnapExecExpandEnvCmdArgs(c *C) {
	for _, t := range []struct {
		args     []string
		env      map[string]string
		expected []string
	}{
		{
			args:     []string{"foo"},
			env:      nil,
			expected: []string{"foo"},
		},
		{
			args:     []string{"$var"},
			env:      map[string]string{"var": "value"},
			expected: []string{"value"},
		},
		{
			args:     []string{"foo", "$not_existing"},
			env:      nil,
			expected: []string{"foo"},
		},
		{
			args:     []string{"foo", "$var", "baz"},
			env:      map[string]string{"var": "bar", "unrelated": "env"},
			expected: []string{"foo", "bar", "baz"},
		},
	} {
		c.Check(snapExec.ExpandEnvCmdArgs(t.args, t.env), DeepEquals, t.expected)

	}
}
