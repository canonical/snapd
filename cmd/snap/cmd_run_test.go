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

package main_test

import (
	"sort"
	"syscall"

	"gopkg.in/check.v1"

	snaprun "github.com/snapcore/snapd/cmd/snap"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

var mockYaml = []byte(`name: snapname
version: 1.0
apps:
 app:
  command: run-app
`)

func (s *SnapSuite) TestSnapRunGetPhase1AppEnv(c *check.C) {
	info, err := snap.InfoFromSnapYaml(mockYaml)
	c.Assert(err, check.IsNil)
	info.SideInfo.Revision = snap.R(42)

	env := snaprun.GetPhase1AppEnv(info.Apps["app"])
	sort.Strings(env)
	c.Check(env, check.DeepEquals, []string{
		"SNAP=/snap/snapname/42",
		"SNAP_ARCH=amd64",
		"SNAP_DATA=/var/snap/snapname/42",
		"SNAP_LIBRARY_PATH=/var/lib/snapd/lib/gl:",
		"SNAP_NAME=snapname",
		"SNAP_REVISION=42",
		"SNAP_USER_DATA=/snap/snapname/42",
		"SNAP_VERSION=1.0",
	})
}

func (s *SnapSuite) TestSnapRun(c *check.C) {
	snaprun.GetSnapInfo = func(snapName string) (*snap.Info, error) {
		info, err := snap.InfoFromSnapYaml(mockYaml)
		info.SideInfo.Revision = snap.R(42)
		return info, err
	}
	defer func() { snaprun.GetSnapInfo = snaprun.GetSnapInfoImpl }()

	execArg0 := ""
	execArgs := []string{}
	execEnv := []string{}
	snaprun.SyscallExec = func(arg0 string, args []string, envv []string) error {
		execArg0 = arg0
		execArgs = args
		execEnv = envv
		return nil
	}
	defer func() { snaprun.SyscallExec = syscall.Exec }()

	err := snaprun.SnapRun("snapname.app", "", []string{"arg1", "arg2"})
	c.Assert(err, check.IsNil)
	c.Check(execArg0, check.Equals, "/usr/bin/ubuntu-core-launcher")
	c.Check(execArgs, check.DeepEquals, []string{"/usr/bin/ubuntu-core-launcher", "snap.snapname.app", "snap.snapname.app", "/usr/lib/snapd/snap-exec", "snapname.app", "arg1", "arg2"})
	c.Check(execEnv, testutil.Contains, "SNAP_REVISION=42")
}
