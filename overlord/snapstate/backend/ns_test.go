// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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

package backend_test

import (
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/snapstate/backend"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/testutil"
)

type nsSuite struct {
	be            backend.Backend
	nullProgress  progress.NullProgress
	oldLibExecDir string
}

var _ = Suite(&nsSuite{})

func (s *nsSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
	// Mock enough bits so that we can observe calls to snap-discard-ns
	s.oldLibExecDir = dirs.LibExecDir
}

func (s *nsSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
	dirs.LibExecDir = s.oldLibExecDir
}

func (s *nsSuite) TestDiscardNamespaceMntFilePresent(c *C) {
	// Mock the snap-discard-ns command
	cmd := testutil.MockCommand(c, "snap-discard-ns", "")
	dirs.LibExecDir = cmd.BinDir()
	defer cmd.Restore()

	// the presence of the .mnt file is the trigger so create it now
	c.Assert(os.MkdirAll(dirs.SnapRunNsDir, 0755), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(dirs.SnapRunNsDir, "snap-name.mnt"), nil, 0644), IsNil)

	err := s.be.DiscardSnapNamespace("snap-name")
	c.Assert(err, IsNil)
	c.Check(cmd.Calls(), DeepEquals, [][]string{{"snap-discard-ns", "snap-name"}})
}

func (s *nsSuite) TestDiscardNamespaceMntFileAbsent(c *C) {
	// Mock the snap-discard-ns command
	cmd := testutil.MockCommand(c, "snap-discard-ns", "")
	dirs.LibExecDir = cmd.BinDir()
	defer cmd.Restore()

	// don't create the .mnt file that triggers the discard operation

	// ask the backend to discard the namespace
	err := s.be.DiscardSnapNamespace("snap-name")
	c.Assert(err, IsNil)
	c.Check(cmd.Calls(), IsNil)
}

func (s *nsSuite) TestDiscardNamespaceFailure(c *C) {
	// Mock the snap-discard-ns command, make it fail
	cmd := testutil.MockCommand(c, "snap-discard-ns", "echo failure; exit 1;")
	dirs.LibExecDir = cmd.BinDir()
	defer cmd.Restore()

	// the presence of the .mnt file is the trigger so create it now
	c.Assert(os.MkdirAll(dirs.SnapRunNsDir, 0755), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(dirs.SnapRunNsDir, "snap-name.mnt"), nil, 0644), IsNil)

	// ask the backend to discard the namespace
	err := s.be.DiscardSnapNamespace("snap-name")
	c.Assert(err, ErrorMatches, `cannot discard preserved namespaces of snap "snap-name": failure`)
	c.Check(cmd.Calls(), DeepEquals, [][]string{{"snap-discard-ns", "snap-name"}})
}

func (s *nsSuite) TestDiscardNamespaceSilentFailure(c *C) {
	// Mock the snap-discard-ns command, make it fail
	cmd := testutil.MockCommand(c, "snap-discard-ns", "exit 1")
	dirs.LibExecDir = cmd.BinDir()
	defer cmd.Restore()

	// the presence of the .mnt file is the trigger so create it now
	c.Assert(os.MkdirAll(dirs.SnapRunNsDir, 0755), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(dirs.SnapRunNsDir, "snap-name.mnt"), nil, 0644), IsNil)

	// ask the backend to discard the namespace
	err := s.be.DiscardSnapNamespace("snap-name")
	c.Assert(err, ErrorMatches, `cannot discard preserved namespaces of snap "snap-name": exit status 1`)
	c.Check(cmd.Calls(), DeepEquals, [][]string{{"snap-discard-ns", "snap-name"}})
}

func (s *nsSuite) TestUpdateNamespaceMnt(c *C) {
	for _, t := range []struct {
		cmd    string
		mnt    bool
		errStr string
		res    [][]string
	}{
		// The mnt file present so we use snap-update-ns;
		// The command doesn't fail and there's no error.
		{cmd: "", mnt: true, errStr: "", res: [][]string{{"snap-update-ns", "snap-name"}}},
		// The mnt file is not present so we don't do anything.
		{cmd: "", mnt: false, errStr: "", res: nil},
		// The mnt file is present so we use snap-update-ns;
		// The command fails and we forward the error along with the output.
		{
			cmd:    "echo failure; exit 1;",
			mnt:    true,
			errStr: `cannot update preserved namespaces of snap "snap-name": failure`,
			res:    [][]string{{"snap-update-ns", "snap-name"}}},
		// The mnt file is present so we use snap-update-ns;
		// The command fails silently and we forward this fact using a generic message.
		{
			cmd:    "exit 1;",
			mnt:    true,
			errStr: `cannot update preserved namespaces of snap "snap-name": exit status 1`,
			res:    [][]string{{"snap-update-ns", "snap-name"}}},
	} {
		cmd := testutil.MockCommand(c, "snap-update-ns", t.cmd)
		dirs.LibExecDir = cmd.BinDir()
		defer cmd.Restore()

		if t.mnt {
			c.Assert(os.MkdirAll(dirs.SnapRunNsDir, 0755), IsNil)
			c.Assert(ioutil.WriteFile(filepath.Join(dirs.SnapRunNsDir, "snap-name.mnt"), nil, 0644), IsNil)
		} else {
			c.Assert(os.RemoveAll(dirs.SnapRunNsDir), IsNil)
		}

		err := s.be.UpdateSnapNamespace("snap-name")
		if t.errStr != "" {
			c.Check(err, ErrorMatches, t.errStr)
		} else {
			c.Check(err, IsNil)
			c.Check(cmd.Calls(), DeepEquals, t.res)
		}
	}
}
