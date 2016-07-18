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

package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type snapExecSuite struct{}

var _ = Suite(&snapExecSuite{})

func (s *snapExecSuite) SetUpTest(c *C) {
	// clean previous parse runs
	opts.Command = ""
	opts.Hook = ""
}

func (s *snapExecSuite) TearDown(c *C) {
	syscallExec = syscall.Exec
	dirs.SetRootDir("/")
}

var mockYaml = []byte(`name: snapname
version: 1.0
apps:
 app:
  command: run-app
  stop-command: stop-app
  post-stop-command: post-stop-app
  environment:
   LD_LIBRARY_PATH: /some/path
 nostop:
  command: nostop
`)

var mockHookYaml = []byte(`name: snapname
version: 1.0
hooks:
 apply-config:
`)

func (s *snapExecSuite) TestInvalidCombinedParameters(c *C) {
	invalidParameters := []string{"--hook=hook-name", "--command=command-name", "snap-name"}
	_, _, err := parseArgs(invalidParameters)
	c.Check(err, ErrorMatches, ".*cannot use --hook and --command together.*")
}

func (s *snapExecSuite) TestInvalidExtraParameters(c *C) {
	invalidParameters := []string{"--hook=hook-name", "snap-name", "foo", "bar"}
	_, _, err := parseArgs(invalidParameters)
	c.Check(err, ErrorMatches, ".*too many arguments for hook \"hook-name\": snap-name foo bar.*")
}

func (s *snapExecSuite) TestFindCommand(c *C) {
	info, err := snap.InfoFromSnapYaml(mockYaml)
	c.Assert(err, IsNil)

	for _, t := range []struct {
		cmd      string
		expected string
	}{
		{cmd: "", expected: "run-app"},
		{cmd: "stop", expected: "stop-app"},
		{cmd: "post-stop", expected: "post-stop-app"},
	} {
		cmd, err := findCommand(info.Apps["app"], t.cmd)
		c.Check(err, IsNil)
		c.Check(cmd, Equals, t.expected)
	}
}

func (s *snapExecSuite) TestFindCommandInvalidCommand(c *C) {
	info, err := snap.InfoFromSnapYaml(mockYaml)
	c.Assert(err, IsNil)

	_, err = findCommand(info.Apps["app"], "xxx")
	c.Check(err, ErrorMatches, `cannot use "xxx" command`)
}

func (s *snapExecSuite) TestFindCommandNoCommand(c *C) {
	info, err := snap.InfoFromSnapYaml(mockYaml)
	c.Assert(err, IsNil)

	_, err = findCommand(info.Apps["nostop"], "stop")
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
	syscallExec = func(argv0 string, argv []string, env []string) error {
		execArgv0 = argv0
		execArgs = argv
		execEnv = env
		return nil
	}

	// launch and verify its run the right way
	err := snapExecApp("snapname.app", "42", "stop", []string{"arg1", "arg2"})
	c.Assert(err, IsNil)
	c.Check(execArgv0, Equals, fmt.Sprintf("%s/snapname/42/stop-app", dirs.SnapSnapsDir))
	c.Check(execArgs, DeepEquals, []string{execArgv0, "arg1", "arg2"})
	c.Check(execEnv, testutil.Contains, "LD_LIBRARY_PATH=/some/path\n")
}

func (s *snapExecSuite) TestSnapExecHookIntegration(c *C) {
	dirs.SetRootDir(c.MkDir())
	snaptest.MockSnap(c, string(mockHookYaml), &snap.SideInfo{
		Revision: snap.R("42"),
	})

	execArgv0 := ""
	execArgs := []string{}
	syscallExec = func(argv0 string, argv []string, env []string) error {
		execArgv0 = argv0
		execArgs = argv
		return nil
	}

	// launch and verify it ran correctly
	err := snapExecHook("snapname", "42", "apply-config")
	c.Assert(err, IsNil)
	c.Check(execArgv0, Equals, fmt.Sprintf("%s/snapname/42/meta/hooks/apply-config", dirs.SnapSnapsDir))
	c.Check(execArgs, DeepEquals, []string{execArgv0})
}

func (s *snapExecSuite) TestSnapExecHookMissingHookIntegration(c *C) {
	dirs.SetRootDir(c.MkDir())
	snaptest.MockSnap(c, string(mockHookYaml), &snap.SideInfo{
		Revision: snap.R("42"),
	})

	err := snapExecHook("snapname", "42", "missing-hook")
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, "cannot find hook \"missing-hook\" in \"snapname\"")
}

func (s *snapExecSuite) TestSnapExecIgnoresUnknownArgs(c *C) {
	snapApp, rest, err := parseArgs([]string{"--command=shell", "snapname.app", "--arg1", "arg2"})
	c.Assert(err, IsNil)
	c.Assert(opts.Command, Equals, "shell")
	c.Assert(snapApp, DeepEquals, "snapname.app")
	c.Assert(rest, DeepEquals, []string{"--arg1", "arg2"})
}

func (s *snapExecSuite) TestSnapExecErrorsOnUnknown(c *C) {
	_, _, err := parseArgs([]string{"--command=shell", "--unknown", "snapname.app", "--arg1", "arg2"})
	c.Check(err, ErrorMatches, "unknown flag `unknown'")
}

func (s *snapExecSuite) TestSnapExecErrorsOnMissingSnapApp(c *C) {
	_, _, err := parseArgs([]string{"--command=shell"})
	c.Check(err, ErrorMatches, "need the application to run as argument")
}

func (s *snapExecSuite) TestSnapExecRealIntegration(c *C) {
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
	script := filepath.Join(dirs.GlobalRootDir, "/snap/snapname/42/run-app")
	err := ioutil.WriteFile(script, []byte(fmt.Sprintf(""+
		"#!/bin/sh\n"+
		"echo \"$(basename \"$0\")\" >> %[1]q\n"+
		"for arg in \"$@\"; do\n"+
		"    echo \"$arg\" >> %[1]q\n"+
		"done\n"+
		"printf \"\\n\" >> %[1]q\n", canaryFile)), 0755)
	c.Assert(err, IsNil)

	// we can not use the real syscall.execv here because it would
	// replace the entire test :)
	syscallExec = func(argv0 string, argv []string, env []string) error {
		cmd := exec.Command(argv[0], argv[1:]...)
		cmd.Env = env
		output, err := cmd.CombinedOutput()
		c.Assert(output, HasLen, 0)
		return err
	}

	// run it
	os.Args = []string{"snap-exec", "snapname.app", "foo", "--bar=baz", "foobar"}
	err = run()
	c.Assert(err, IsNil)

	output, err := ioutil.ReadFile(canaryFile)
	c.Assert(err, IsNil)
	c.Assert(string(output), Equals, `run-app
foo
--bar=baz
foobar

`)
}
