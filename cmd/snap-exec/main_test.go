// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !integrationcoverage

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

type snapExecSuite struct {
}

var _ = Suite(&snapExecSuite{})

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

func (s *snapExecSuite) TestInvalidParameters(c *C) {
	invalidParameters := []string{"snap-name", "--hook=hook-name", "--command=command-name"}
	err := run(invalidParameters)
	c.Check(err, ErrorMatches, ".*cannot use --hook and --command together.*")

	invalidParameters = []string{"snap-name", "--hook=hook-name", "foo", "bar"}
	err = run(invalidParameters)
	c.Check(err, ErrorMatches, ".*too many arguments for hook \"hook-name\": foo bar.*")
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
