// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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

package cli_test

import (
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	snap "github.com/snapcore/snapd/cmd/snapd/cli"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/testutil"
)

func (s *SnapSuite) TestDebugMountNamespaceShellFailsIfMntFileNotExist(c *C) {
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("/")

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"debug", "mount-namespace", "test-snap"})
	c.Assert(err, ErrorMatches, `cannot enter mount namespace of snap "test-snap": the mount namespace is not bound to a file \(.*/run/snapd/ns/test-snap.mnt does not exist\)`)
}

func (s *SnapSuite) TestDebugMountNamespaceShellDefaultBash(c *C) {
	rootDir := c.MkDir()
	dirs.SetRootDir(rootDir)
	defer dirs.SetRootDir("/")

	// Create the .mnt file so the existence check passes
	nsDir := filepath.Join(rootDir, "/run/snapd/ns")
	err := os.MkdirAll(nsDir, 0755)
	c.Assert(err, IsNil)
	mntFile := filepath.Join(nsDir, "test-snap.mnt")
	err = os.WriteFile(mntFile, nil, 0600)
	c.Assert(err, IsNil)

	// Mock nsenter so exec.LookPath resolves without host dependency
	nsenterCmd := testutil.MockCommand(c, "nsenter", "")
	defer nsenterCmd.Restore()

	var execCalled bool
	var execPath string
	var execArgv []string
	var execEnv []string
	restoreExec := snap.MockSyscallExec(func(path string, argv []string, env []string) error {
		execCalled = true
		execPath = path
		execArgv = argv
		execEnv = env
		return nil
	})
	defer restoreExec()

	_, err = snap.Parser(snap.Client()).ParseArgs([]string{"debug", "mount-namespace", "test-snap"})
	c.Assert(err, IsNil)
	c.Assert(execCalled, Equals, true)
	c.Assert(execPath, Equals, nsenterCmd.Exe())
	c.Assert(execArgv, DeepEquals, []string{"nsenter", "-m" + mntFile, "/bin/bash"})
	c.Assert(execEnv, DeepEquals, []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin"})
}

func (s *SnapSuite) TestDebugMountNamespaceShellWithCommand(c *C) {
	rootDir := c.MkDir()
	dirs.SetRootDir(rootDir)
	defer dirs.SetRootDir("/")

	// Create the .mnt file so the existence check passes
	nsDir := filepath.Join(rootDir, "/run/snapd/ns")
	err := os.MkdirAll(nsDir, 0755)
	c.Assert(err, IsNil)
	mntFile := filepath.Join(nsDir, "test-snap.mnt")
	err = os.WriteFile(mntFile, nil, 0600)
	c.Assert(err, IsNil)

	// Mock nsenter so exec.LookPath resolves without host dependency
	nsenterCmd := testutil.MockCommand(c, "nsenter", "")
	defer nsenterCmd.Restore()

	var execArgv []string
	var execEnv []string
	restoreExec := snap.MockSyscallExec(func(path string, argv []string, env []string) error {
		execArgv = argv
		execEnv = env
		return nil
	})
	defer restoreExec()

	_, err = snap.Parser(snap.Client()).ParseArgs([]string{"debug", "mount-namespace", "--shell", "test-snap", "--", "/usr/bin/findmnt", "-l"})
	c.Assert(err, IsNil)
	c.Assert(execArgv, DeepEquals, []string{"nsenter", "-m" + mntFile, "/usr/bin/findmnt", "-l"})
	c.Assert(execEnv, DeepEquals, []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin"})
}

func (s *SnapSuite) TestDebugMountNamespaceShellWithCommandNoDash(c *C) {
	rootDir := c.MkDir()
	dirs.SetRootDir(rootDir)
	defer dirs.SetRootDir("/")

	// Create the .mnt file so the existence check passes
	nsDir := filepath.Join(rootDir, "/run/snapd/ns")
	err := os.MkdirAll(nsDir, 0755)
	c.Assert(err, IsNil)
	mntFile := filepath.Join(nsDir, "test-snap.mnt")
	err = os.WriteFile(mntFile, nil, 0600)
	c.Assert(err, IsNil)

	// Mock nsenter so exec.LookPath resolves without host dependency
	nsenterCmd := testutil.MockCommand(c, "nsenter", "")
	defer nsenterCmd.Restore()

	var execArgv []string
	restoreExec := snap.MockSyscallExec(func(path string, argv []string, env []string) error {
		execArgv = argv
		return nil
	})
	defer restoreExec()

	// Without --, positional arguments that don't look like flags work fine
	_, err = snap.Parser(snap.Client()).ParseArgs([]string{"debug", "mount-namespace", "--shell", "test-snap", "/usr/bin/findmnt"})
	c.Assert(err, IsNil)
	c.Assert(execArgv, DeepEquals, []string{"nsenter", "-m" + mntFile, "/usr/bin/findmnt"})
}

func (s *SnapSuite) TestDebugMountNamespaceDiscardRunsTool(c *C) {
	cmd := testutil.MockCommand(c, "snap-discard-ns", "")
	dirs.DistroLibExecDir = cmd.BinDir()
	defer cmd.Restore()

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"debug", "mount-namespace", "--discard", "test-snap"})
	c.Assert(err, IsNil)
	c.Assert(cmd.Calls(), DeepEquals, [][]string{
		{"snap-discard-ns", "test-snap"},
	})
}

func (s *SnapSuite) TestDebugMountNamespaceDiscardReportsError(c *C) {
	cmd := testutil.MockCommand(c, "snap-discard-ns", "echo 'namespace error'; exit 1")
	dirs.DistroLibExecDir = cmd.BinDir()
	defer cmd.Restore()

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"debug", "mount-namespace", "--discard", "test-snap"})
	c.Assert(err, ErrorMatches, `cannot discard mount namespace of snap "test-snap": .*`)
}

func (s *SnapSuite) TestDebugMountNamespaceShellAndDiscardMutuallyExclusive(c *C) {
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"debug", "mount-namespace", "--shell", "--discard", "test-snap"})
	c.Assert(err, ErrorMatches, `--shell and --discard cannot be used together`)
}
