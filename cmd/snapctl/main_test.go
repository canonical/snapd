// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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
	"os"
	"testing"

	. "gopkg.in/check.v1"

	snapctlmain "github.com/snapcore/snapd/cmd/snapctl"
)

func Test(t *testing.T) { TestingT(t) }

type dispatchSuite struct{}

var _ = Suite(&dispatchSuite{})

func runDispatch(args []string) (called string, argsAtCall []string) {
	saved := os.Args
	defer func() { os.Args = saved }()
	os.Args = args

	restoreSnapExec := snapctlmain.MockSnapExecMain(func() {
		called = "snap-exec"
		argsAtCall = append([]string(nil), os.Args...)
	})
	defer restoreSnapExec()

	restoreSnapctl := snapctlmain.MockSnapctlMain(func() {
		called = "snapctl"
		argsAtCall = append([]string(nil), os.Args...)
	})
	defer restoreSnapctl()

	snapctlmain.Main()
	return called, argsAtCall
}

func (s *dispatchSuite) TestArgv0SnapExecDispatchesSnapExec(c *C) {
	called, argsAtCall := runDispatch([]string{"snap-exec"})
	c.Check(called, Equals, "snap-exec")
	c.Check(argsAtCall, DeepEquals, []string{"snap-exec"})
}

func (s *dispatchSuite) TestArgv0SnapExecWithArgsPassesArgsThrough(c *C) {
	called, argsAtCall := runDispatch([]string{"snap-exec", "mysnap.app", "--arg"})
	c.Check(called, Equals, "snap-exec")
	c.Check(argsAtCall, DeepEquals, []string{"snap-exec", "mysnap.app", "--arg"})
}

func (s *dispatchSuite) TestArgv0SnapExecFullPathDispatches(c *C) {
	called, argsAtCall := runDispatch([]string{"/usr/lib/snapd/snap-exec", "mysnap.app"})
	c.Check(called, Equals, "snap-exec")
	c.Check(argsAtCall, DeepEquals, []string{"/usr/lib/snapd/snap-exec", "mysnap.app"})
}

func (s *dispatchSuite) TestArgv0SnapExecInSnapDispatches(c *C) {
	// snap-exec invoked from within a snapd snap (re-exec scenario).
	args := []string{"/snap/snapd/123/usr/lib/snapd/snap-exec", "mysnap.app"}
	called, argsAtCall := runDispatch(args)
	c.Check(called, Equals, "snap-exec")
	c.Check(argsAtCall, DeepEquals, args)
}

func (s *dispatchSuite) TestArgv0SnapctlDispatchesSnapctl(c *C) {
	called, argsAtCall := runDispatch([]string{"snapctl"})
	c.Check(called, Equals, "snapctl")
	c.Check(argsAtCall, DeepEquals, []string{"snapctl"})
}

func (s *dispatchSuite) TestArgv0SnapctlWithArgsPassesArgsThrough(c *C) {
	called, argsAtCall := runDispatch([]string{"snapctl", "set", "key=value"})
	c.Check(called, Equals, "snapctl")
	c.Check(argsAtCall, DeepEquals, []string{"snapctl", "set", "key=value"})
}

func (s *dispatchSuite) TestArgv0UnknownDispatchesSnapctl(c *C) {
	// Any argv[0] other than "snap-exec" falls through to snapctl.
	called, argsAtCall := runDispatch([]string{"something-else", "an-arg"})
	c.Check(called, Equals, "snapctl")
	c.Check(argsAtCall, DeepEquals, []string{"something-else", "an-arg"})
}
