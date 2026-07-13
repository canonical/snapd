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
	"fmt"
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	snapdmain "github.com/snapcore/snapd/cmd/snapd"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snapdtool"
	"github.com/snapcore/snapd/testutil"
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
	// Tool name is preserved; tool sees ["snap-preseed", "/some/path"].
	c.Check(argsAtCall, DeepEquals, []string{"snap-preseed", "/some/path"})
}

func (s *dispatchSuite) TestArgv0SnapdArgv1SnapdApparmorDispatches(c *C) {
	called, argsAtCall := runDispatch([]string{"snapd", "snapd-apparmor", "start"})
	c.Check(called, Equals, "snapd-apparmor")
	c.Check(argsAtCall, DeepEquals, []string{"snapd-apparmor", "start"})
}

func (s *dispatchSuite) TestArgv0SnapdArgv1SnapGpioHelperDispatches(c *C) {
	called, argsAtCall := runDispatch([]string{"snapd", "snap-gpio-helper", "export-chardev"})
	c.Check(called, Equals, "snap-gpio-helper")
	c.Check(argsAtCall, DeepEquals, []string{"snap-gpio-helper", "export-chardev"})
}

// --- arg stripping ---

// When a tool is dispatched, argv[1] (the tool name) must be stripped so that
// the tool sees its own arguments starting at argv[1], not at argv[2].
func (s *dispatchSuite) TestToolDispatchStripsToolNameFromArgs(c *C) {
	called, argsAtCall := runDispatch([]string{"snapd", "snap-preseed", "--reset", "/path"})
	c.Check(called, Equals, "snap-preseed")
	// The tool should see ["snap-preseed", "--reset", "/path"]
	c.Check(argsAtCall, DeepEquals, []string{"snap-preseed", "--reset", "/path"})
}

func (s *dispatchSuite) TestToolDispatchNoUserArgsStripsToolName(c *C) {
	called, argsAtCall := runDispatch([]string{"snapd", "snap-gpio-helper"})
	c.Check(called, Equals, "snap-gpio-helper")
	c.Check(argsAtCall, DeepEquals, []string{"snap-gpio-helper"})
}

// --- re-exec integration ---

type reexecSuite struct {
	testutil.BaseTest

	fakeroot  string
	snapdPath string
}

var _ = Suite(&reexecSuite{})

func (s *reexecSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	_, restoreLogger := logger.MockLogger()
	s.AddCleanup(restoreLogger)

	s.AddCleanup(release.MockReleaseInfo(&release.OS{ID: "ubuntu"}))
	s.AddCleanup(release.MockOnClassic(true))

	s.fakeroot = c.MkDir()
	dirs.SetRootDir(s.fakeroot)
	s.AddCleanup(func() { dirs.SetRootDir("") })

	s.snapdPath = filepath.Join(dirs.SnapMountDir, "snapd", "42")

	c.Assert(os.MkdirAll(filepath.Join(s.fakeroot, "proc", "self"), 0755), IsNil)

	// Default: syscallExec is not expected to fire. Individual tests override.
	s.AddCleanup(snapdtool.MockSyscallExec(func(argv0 string, argv []string, envv []string) error {
		c.Fatalf("unexpected syscallExec: argv0=%q argv=%v", argv0, argv)
		return fmt.Errorf("syscallExec not expected")
	}))
}

// mockReExecEnv sets up the environment so that ExecInSnapdOrCoreSnap() will
// re-exec from the system snapd into the snapd snap at s.snapdPath.
func (s *reexecSuite) mockReExecEnv(c *C) {
	s.AddCleanup(snapdtool.MockCoreSnapdPaths(filepath.Join(dirs.SnapMountDir, "core", "21"), s.snapdPath))
	s.AddCleanup(snapdtool.MockVersion("2"))

	// Create a fake snapd snap with a newer version.
	infoDir := filepath.Join(s.snapdPath, "usr", "lib", "snapd")
	c.Assert(os.MkdirAll(infoDir, 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(infoDir, "info"), []byte("VERSION=42"), 0644), IsNil)

	// Create the fake snapd binary inside the snapd snap.
	makeFakeExe(c, filepath.Join(infoDir, "snapd"))

	// Mock /proc/self/exe to point to the system snapd binary.
	selfExe := filepath.Join(s.fakeroot, "proc", "self", "exe")
	systemSnapd := filepath.Join(dirs.DistroLibExecDir, "snapd")
	c.Assert(os.MkdirAll(filepath.Dir(systemSnapd), 0755), IsNil)
	makeFakeExe(c, systemSnapd)
	c.Assert(os.Symlink(systemSnapd, selfExe), IsNil)
	s.AddCleanup(snapdtool.MockSelfExe(selfExe))
}

func makeFakeExe(c *C, path string) {
	// TODO move to testutil
	c.Assert(os.MkdirAll(filepath.Dir(path), 0755), IsNil)
	c.Assert(os.WriteFile(path, nil, 0755), IsNil)
}

// TestToolDispatchReexecPreservesToolName verifies that when a tool is
// dispatched and the tool calls ExecInSnapdOrCoreSnap(), the args passed
// to syscall.Exec contain the tool name. Without the tool name in argv,
// the re-exec'd snapd process would fall through to the daemon instead of
// dispatching to the tool.
func (s *reexecSuite) TestToolDispatchReexecPreservesToolName(c *C) {
	s.mockReExecEnv(c)

	savedArgs := os.Args
	defer func() { os.Args = savedArgs }()
	os.Args = []string{"snapd", "snapd-apparmor", "start"}

	var execArgv []string
	s.AddCleanup(snapdtool.MockSyscallExec(func(argv0 string, argv []string, envv []string) error {
		execArgv = append([]string(nil), argv...)
		// Return an error to prevent actual exec; ExecInSnapdOrCoreSnap
		// panics on error from syscallExec.
		return fmt.Errorf("stopped exec in test")
	}))

	restore := snapdmain.MockToolMains(map[string]func(){
		"snapd-apparmor": func() {},
	})
	defer restore()

	restore = snapdmain.MockReexecTools([]string{
		"snapd-apparmor",
	})
	defer restore()

	// ExecInSnapdOrCoreSnap panics when syscallExec returns an error.
	c.Check(func() { snapdmain.Main() }, PanicMatches, "stopped exec in test")

	// The args passed to syscall.Exec must contain "snapd-apparmor" at
	// argv[1] so the re-exec'd process dispatches to the tool, not the daemon.
	c.Assert(execArgv, NotNil)
	c.Check(execArgv, DeepEquals, []string{"snapd", "snapd-apparmor", "start"})
}

// TestToolDispatchReexecNotNeeded executes a scenario in which the invoked tool
// does not expect reexec to be invoked before its entrypoint is called.
func (s *reexecSuite) TestToolDispatchReexecNotNeeded(c *C) {
	s.mockReExecEnv(c)

	savedArgs := os.Args
	defer func() { os.Args = savedArgs }()
	os.Args = []string{"snapd", "not-reexecd-tool", "start"}

	s.AddCleanup(snapdtool.MockSyscallExec(func(argv0 string, argv []string, envv []string) error {
		panic("unexpected call")
	}))

	toolCalled := false
	restore := snapdmain.MockToolMains(map[string]func(){
		"not-reexecd-tool": func() {
			toolCalled = true
		},
	})
	defer restore()

	restore = snapdmain.MockReexecTools([]string{})
	defer restore()

	snapdmain.Main()
	c.Check(toolCalled, Equals, true)
}
