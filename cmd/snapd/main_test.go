// -*- Mode: Go; indent-tabs-mode: t -*-

//go:build linux

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

	snapdmain "github.com/snapcore/snapd/cmd/snapd"
)

func Test(t *testing.T) { TestingT(t) }

type dispatchSuite struct{}

var _ = Suite(&dispatchSuite{})

// runDispatch saves/restores os.Args around a Main() call, returning which
// entry point was invoked and the os.Args it saw at call time.
func runDispatch(args []string) (called string, argsAtCall []string) {
	saved := os.Args
	defer func() { os.Args = saved }()
	os.Args = args

	restoreDaemon := snapdmain.MockDaemonMain(func() {
		called = "daemon"
		argsAtCall = append([]string(nil), os.Args...)
	})
	defer restoreDaemon()

	restoreCLI := snapdmain.MockCLIMain(func() {
		called = "cli"
		argsAtCall = append([]string(nil), os.Args...)
	})
	defer restoreCLI()

	restoreTools := snapdmain.MockToolMains(map[string]func(){
		"snap-preseed": func() {
			called = "snap-preseed"
			argsAtCall = append([]string(nil), os.Args...)
		},
		"snapd-apparmor": func() {
			called = "snapd-apparmor"
			argsAtCall = append([]string(nil), os.Args...)
		},
		"snap-gpio-helper": func() {
			called = "snap-gpio-helper"
			argsAtCall = append([]string(nil), os.Args...)
		},
	})
	defer restoreTools()

	snapdmain.Main()
	return called, argsAtCall
}

// --- daemon dispatch ---

func (s *dispatchSuite) TestArgv0SnapdNoArgv1DispatchesDaemon(c *C) {
	called, argsAtCall := runDispatch([]string{"snapd"})
	c.Check(called, Equals, "daemon")
	c.Check(argsAtCall, DeepEquals, []string{"snapd"})
}

func (s *dispatchSuite) TestArgv0SnapdUnknownToolNameDispatchesDaemon(c *C) {
	called, argsAtCall := runDispatch([]string{"snapd", "not-a-tool"})
	c.Check(called, Equals, "daemon")
	// Unknown argv[1] is NOT stripped — the daemon sees it as-is.
	c.Check(argsAtCall, DeepEquals, []string{"snapd", "not-a-tool"})
}

func (s *dispatchSuite) TestArgv0FullPathSnapdDispatchesDaemon(c *C) {
	// When snapd is started by systemd the full path is used as argv[0].
	called, argsAtCall := runDispatch([]string{"/usr/lib/snapd/snapd"})
	c.Check(called, Equals, "daemon")
	c.Check(argsAtCall, DeepEquals, []string{"/usr/lib/snapd/snapd"})
}

// --- CLI dispatch ---

func (s *dispatchSuite) TestArgv0SnapDispatchesCLI(c *C) {
	called, argsAtCall := runDispatch([]string{"snap"})
	c.Check(called, Equals, "cli")
	c.Check(argsAtCall, DeepEquals, []string{"snap"})
}

func (s *dispatchSuite) TestArgv0SnapWithArgsDispatchesCLI(c *C) {
	called, argsAtCall := runDispatch([]string{"snap", "list"})
	c.Check(called, Equals, "cli")
	c.Check(argsAtCall, DeepEquals, []string{"snap", "list"})
}

func (s *dispatchSuite) TestArgv0AppNameDispatchesCLI(c *C) {
	// /snap/bin/<app-name> symlinks set argv[0] to the app name.
	called, argsAtCall := runDispatch([]string{"ohmygiraffe"})
	c.Check(called, Equals, "cli")
	c.Check(argsAtCall, DeepEquals, []string{"ohmygiraffe"})
}

func (s *dispatchSuite) TestArgv0AppNameWithArgsDispatchesCLI(c *C) {
	// App invoked with arguments — all args must be passed through unchanged.
	called, argsAtCall := runDispatch([]string{"ohmygiraffe", "some-arg"})
	c.Check(called, Equals, "cli")
	c.Check(argsAtCall, DeepEquals, []string{"ohmygiraffe", "some-arg"})
}

func (s *dispatchSuite) TestArgv0FullPathSnapDispatchesCLI(c *C) {
	called, argsAtCall := runDispatch([]string{"/usr/bin/snap", "run", "myapp"})
	c.Check(called, Equals, "cli")
	c.Check(argsAtCall, DeepEquals, []string{"/usr/bin/snap", "run", "myapp"})
}

func (s *dispatchSuite) TestArgv0SnapInSnapDispatchesCLI(c *C) {
	// Re-exec path: snapd hook runner invokes snap from the snap mount.
	args := []string{"/snap/snapd/123/usr/bin/snap", "run", "--hook", "configure", "mysnap"}
	called, argsAtCall := runDispatch(args)
	c.Check(called, Equals, "cli")
	c.Check(argsAtCall, DeepEquals, args)
}

// --- tool dispatch ---

func (s *dispatchSuite) TestArgv0SnapdArgv1SnapPreseedDispatchesPreseed(c *C) {
	called, argsAtCall := runDispatch([]string{"snapd", "snap-preseed", "/some/path"})
	c.Check(called, Equals, "snap-preseed")
	// Tool name is stripped; tool sees ["snapd", "/some/path"].
	c.Check(argsAtCall, DeepEquals, []string{"snapd", "/some/path"})
}

func (s *dispatchSuite) TestArgv0SnapdArgv1SnapdApparmorDispatches(c *C) {
	called, argsAtCall := runDispatch([]string{"snapd", "snapd-apparmor", "start"})
	c.Check(called, Equals, "snapd-apparmor")
	c.Check(argsAtCall, DeepEquals, []string{"snapd", "start"})
}

func (s *dispatchSuite) TestArgv0SnapdArgv1SnapGpioHelperDispatches(c *C) {
	called, argsAtCall := runDispatch([]string{"snapd", "snap-gpio-helper", "export-chardev"})
	c.Check(called, Equals, "snap-gpio-helper")
	c.Check(argsAtCall, DeepEquals, []string{"snapd", "export-chardev"})
}

// --- arg stripping ---

// When a tool is dispatched, argv[1] (the tool name) must be stripped so that
// the tool sees its own arguments starting at argv[1], not at argv[2].
func (s *dispatchSuite) TestToolDispatchStripsToolNameFromArgs(c *C) {
	called, argsAtCall := runDispatch([]string{"snapd", "snap-preseed", "--reset", "/path"})
	c.Check(called, Equals, "snap-preseed")
	// The tool should see ["snapd", "--reset", "/path"], not
	// ["snapd", "snap-preseed", "--reset", "/path"].
	c.Check(argsAtCall, DeepEquals, []string{"snapd", "--reset", "/path"})
}

func (s *dispatchSuite) TestToolDispatchNoUserArgsStripsToolName(c *C) {
	called, argsAtCall := runDispatch([]string{"snapd", "snap-gpio-helper"})
	c.Check(called, Equals, "snap-gpio-helper")
	c.Check(argsAtCall, DeepEquals, []string{"snapd"})
}
