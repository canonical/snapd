// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) Canonical Ltd
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

package blockplan_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/snap/squashfs/blockplan"
	"github.com/snapcore/snapd/testutil"
)

type toolSuite struct {
	testutil.BaseTest
}

var _ = Suite(&toolSuite{})

func (s *toolSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })
}

// systemSnapWith puts tool in the snapd snap of the mocked system, which is
// where a device gets the tools snapd was tested with.
func (s *toolSuite) systemSnapWith(c *C, tool string) string {
	inSnap := filepath.Join(dirs.SnapMountDir, "snapd", "current", "usr", "bin", tool)
	c.Assert(os.MkdirAll(filepath.Dir(inSnap), 0755), IsNil)
	c.Assert(os.WriteFile(inSnap, []byte("#!/bin/sh\n"), 0755), IsNil)
	return inSnap
}

// refuseToBuild is a seam that fails the test if it is reached, for the cases
// where the snap copy must not even be considered.
func (s *toolSuite) refuseToBuild(c *C) func(ctx context.Context, name string, args ...string) (*exec.Cmd, error) {
	return func(ctx context.Context, name string, args ...string) (*exec.Cmd, error) {
		c.Fatalf("the system snap was asked for %s, which it does not carry", name)
		return nil, nil
	}
}

func (s *toolSuite) TestToolCommandFromPath(c *C) {
	mock := testutil.MockCommand(c, "xz", "")
	s.AddCleanup(mock.Restore)
	s.AddCleanup(blockplan.MockCommandFromSystemSnapWithContext(s.refuseToBuild(c)))

	cmd, err := blockplan.ToolCommand(context.Background(), "xz", "-dc", "-T1")
	c.Assert(err, IsNil)
	c.Check(cmd.Path, Equals, mock.Exe())
	c.Check(cmd.Args, DeepEquals, []string{mock.Exe(), "-dc", "-T1"})

	// And the command really runs, which is the whole point of resolving it
	// rather than reporting a path.
	c.Assert(cmd.Run(), IsNil)
	c.Check(mock.Calls(), DeepEquals, [][]string{{"xz", "-dc", "-T1"}})
}

func (s *toolSuite) TestToolCommandPrefersTheSystemSnap(c *C) {
	s.systemSnapWith(c, "xz")
	mock := testutil.MockCommand(c, "xz", "")
	s.AddCleanup(mock.Restore)

	var gotName string
	var gotArgs []string
	s.AddCleanup(blockplan.MockCommandFromSystemSnapWithContext(
		func(ctx context.Context, name string, args ...string) (*exec.Cmd, error) {
			gotName, gotArgs = name, args
			return exec.CommandContext(ctx, "/from/the/snap", args...), nil
		}))

	cmd, err := blockplan.ToolCommand(context.Background(), "xz", "-dc")
	c.Assert(err, IsNil)
	// The path handed to snapdtool is snap-relative, and the tool from the
	// snap wins over the one on $PATH: on a device the snap copy is the one
	// snapd was tested with.
	c.Check(gotName, Equals, "/usr/bin/xz")
	c.Check(gotArgs, DeepEquals, []string{"-dc"})
	c.Check(cmd.Path, Equals, "/from/the/snap")
}

func (s *toolSuite) TestToolCommandSkipsTheSystemSnapWithoutTheTool(c *C) {
	// The snapd snap is mounted but carries no xz, which is what every snapd
	// snap before the one that started shipping it looks like. Asking
	// snapdtool for it anyway would yield a command that fails at exec time,
	// turning a clean fallback into a failed refresh.
	c.Assert(os.MkdirAll(filepath.Join(dirs.SnapMountDir, "snapd", "current", "usr", "bin"), 0755), IsNil)
	mock := testutil.MockCommand(c, "xz", "")
	s.AddCleanup(mock.Restore)
	s.AddCleanup(blockplan.MockCommandFromSystemSnapWithContext(s.refuseToBuild(c)))

	cmd, err := blockplan.ToolCommand(context.Background(), "xz")
	c.Assert(err, IsNil)
	c.Check(cmd.Path, Equals, mock.Exe())
}

func (s *toolSuite) TestToolCommandFallsBackWhenTheSnapCommandCannotBeBuilt(c *C) {
	s.systemSnapWith(c, "xz")
	mock := testutil.MockCommand(c, "xz", "")
	s.AddCleanup(mock.Restore)
	s.AddCleanup(blockplan.MockCommandFromSystemSnapWithContext(
		func(ctx context.Context, name string, args ...string) (*exec.Cmd, error) {
			// What a snap whose ELF interpreter cannot be worked out
			// reports. A tool that runs beats a refusal.
			return nil, errors.New("cannot read ELF interpreter")
		}))

	cmd, err := blockplan.ToolCommand(context.Background(), "xz")
	c.Assert(err, IsNil)
	c.Check(cmd.Path, Equals, mock.Exe())
}

func (s *toolSuite) TestToolCommandMissingTool(c *C) {
	s.AddCleanup(blockplan.MockCommandFromSystemSnapWithContext(s.refuseToBuild(c)))

	_, err := blockplan.ToolCommand(context.Background(), "blockplan-no-such-tool")
	c.Assert(err, ErrorMatches, `cannot find blockplan-no-such-tool: .*`)
}

func (s *toolSuite) TestHaveTool(c *C) {
	s.AddCleanup(blockplan.MockCommandFromSystemSnapWithContext(s.refuseToBuild(c)))
	c.Check(blockplan.HaveTool("blockplan-no-such-tool"), Equals, false)

	mock := testutil.MockCommand(c, "hdiffz", "")
	s.AddCleanup(mock.Restore)
	c.Check(blockplan.HaveTool("hdiffz"), Equals, true)
}
