// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2017 Canonical Ltd
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

package mount_test

import (
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces/mount"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/testutil"
)

type nsSuite struct {
	testutil.BaseTest
}

var _ = Suite(&nsSuite{})

func (s *nsSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })
	s.AddCleanup(release.MockOnClassic(true))
	// Anything that just gives us no-reexec.
	s.AddCleanup(release.MockReleaseInfo(&release.OS{ID: "fedora"}))
}

func (s *nsSuite) TestDiscardNamespaceMnt(c *C) {
	for _, t := range []struct {
		cmd    string
		mnt    bool
		errStr string
		res    [][]string
	}{
		// The mnt file present so we use snap-discard-ns;
		// The command doesn't fail and there's no error.
		{cmd: "", mnt: true, errStr: "", res: [][]string{{"snap-discard-ns", "snap-name"}}},
		// The mnt file is not present so we don't do anything.
		{cmd: "", mnt: false, errStr: "", res: nil},
		// The mnt file is present so we use snap-discard-ns;
		// The command fails and we forward the error along with the output.
		{
			cmd:    "echo failure; exit 1;",
			mnt:    true,
			errStr: `cannot discard preserved namespace of snap "snap-name": failure`,
			res:    [][]string{{"snap-discard-ns", "snap-name"}}},
		// The mnt file is present so we use snap-discard-ns;
		// The command fails silently and we forward this fact using a generic message.
		{
			cmd:    "exit 1;",
			mnt:    true,
			errStr: `cannot discard preserved namespace of snap "snap-name": exit status 1`,
			res:    [][]string{{"snap-discard-ns", "snap-name"}}},
	} {
		cmd := testutil.MockCommand(c, "snap-discard-ns", t.cmd)
		dirs.DistroLibExecDir = cmd.BinDir()
		defer cmd.Restore()

		if t.mnt {
			c.Assert(os.MkdirAll(dirs.SnapRunNsDir, 0755), IsNil)
			c.Assert(os.WriteFile(filepath.Join(dirs.SnapRunNsDir, "snap-name.mnt"), nil, 0644), IsNil)
		} else {
			c.Assert(os.RemoveAll(dirs.SnapRunNsDir), IsNil)
		}

		err := mount.DiscardSnapNamespace("snap-name")
		if t.errStr != "" {
			c.Check(err, ErrorMatches, t.errStr)
		} else {
			c.Check(err, IsNil)
			c.Check(cmd.Calls(), DeepEquals, t.res)
		}
	}
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
			errStr: `cannot update preserved namespace of snap "snap-name": failure`,
			res:    [][]string{{"snap-update-ns", "snap-name"}}},
		// The mnt file is present so we use snap-update-ns;
		// The command fails silently and we forward this fact using a generic message.
		{
			cmd:    "exit 1;",
			mnt:    true,
			errStr: `cannot update preserved namespace of snap "snap-name": exit status 1`,
			res:    [][]string{{"snap-update-ns", "snap-name"}}},
	} {
		cmd := testutil.MockCommand(c, "snap-update-ns", t.cmd)
		dirs.DistroLibExecDir = cmd.BinDir()
		defer cmd.Restore()

		if t.mnt {
			c.Assert(os.MkdirAll(dirs.SnapRunNsDir, 0755), IsNil)
			c.Assert(os.WriteFile(filepath.Join(dirs.SnapRunNsDir, "snap-name.mnt"), nil, 0644), IsNil)
		} else {
			c.Assert(os.RemoveAll(dirs.SnapRunNsDir), IsNil)
		}

		err := mount.UpdateSnapNamespace("snap-name")
		if t.errStr != "" {
			c.Check(err, ErrorMatches, t.errStr)
		} else {
			c.Check(err, IsNil)
			c.Check(cmd.Calls(), DeepEquals, t.res)
		}
	}
}
