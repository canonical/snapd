// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
package volmgr_test

import (
	"io/ioutil"
	"os"
	"path"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/cmd/snap-install/volmgr"
	"github.com/snapcore/snapd/testutil"
)

func (s *volmgrTestSuite) TestWipe(c *C) {
	data := []byte("12345678")
	temp := path.Join(c.MkDir(), "myfile")
	err := ioutil.WriteFile(temp, data, 0600)
	c.Assert(err, IsNil)
	c.Assert(temp, testutil.FilePresent)
	err = volmgr.Wipe(temp)
	c.Assert(err, IsNil)
	c.Assert(temp, testutil.FileAbsent)
}

func (s *volmgrTestSuite) TestCreateKey(c *C) {
	data, err := volmgr.CreateKey(16)
	c.Assert(err, IsNil)
	c.Assert(data, Not(DeepEquals), make([]byte, 16))
}

func (s *volmgrTestSuite) TestMount(c *C) {
	cmd := testutil.MockCommand(c, "mount", "exit 0")
	defer cmd.Restore()

	err := volmgr.Mount("/dev/node", "mountpoint")
	c.Assert(err, IsNil)
	c.Assert(cmd.Calls(), DeepEquals, [][]string{
		{"mount", "/dev/node", "mountpoint"},
	})
}

func (s *volmgrTestSuite) TestMountOptions(c *C) {
	cmd := testutil.MockCommand(c, "mount", "exit 0")
	defer cmd.Restore()

	err := volmgr.Mount("/dev/node", "mountpoint", "-o", "rw,remount")
	c.Assert(err, IsNil)
	c.Assert(cmd.Calls(), DeepEquals, [][]string{
		{"mount", "-o", "rw,remount", "/dev/node", "mountpoint"},
	})
}

func (s *volmgrTestSuite) TestMountError(c *C) {
	cmd := testutil.MockCommand(c, "mount", `echo "mount: some error"; exit 32`)
	defer cmd.Restore()

	err := volmgr.Mount("/dev/node", "mountpoint")
	c.Assert(err, ErrorMatches, "mount: some error")
	c.Assert(cmd.Calls(), DeepEquals, [][]string{
		{"mount", "/dev/node", "mountpoint"},
	})
}

func (s *volmgrTestSuite) TestUnmount(c *C) {
	cmd := testutil.MockCommand(c, "umount", "exit 0")
	defer cmd.Restore()

	err := volmgr.Unmount("mountpoint")
	c.Assert(err, IsNil)
	c.Assert(cmd.Calls(), DeepEquals, [][]string{
		{"umount", "mountpoint"},
	})
}

func (s *volmgrTestSuite) TestUnmountError(c *C) {
	cmd := testutil.MockCommand(c, "umount", `echo "umount: some error"; exit 1`)
	defer cmd.Restore()

	err := volmgr.Unmount("mountpoint")
	c.Assert(err, ErrorMatches, "umount: some error")
	c.Assert(cmd.Calls(), DeepEquals, [][]string{
		{"umount", "mountpoint"},
	})
}

func (s *volmgrTestSuite) TestEnsureDirectory(c *C) {
	name := c.MkDir()

	// test if path exists
	err := volmgr.EnsureDirectory(name)
	c.Assert(err, IsNil)

	// test with non-existent path (should create directory)
	p := path.Join(name, "new", "path")
	err = volmgr.EnsureDirectory(p)
	c.Assert(err, IsNil)
	stat, err := os.Stat(p)
	c.Assert(err, IsNil)
	c.Assert(stat.IsDir(), Equals, true)

	// test with non-directory path
	p = path.Join(name, "newfile")
	f, err := os.Create(p)
	c.Assert(err, IsNil)
	f.Close()
	err = volmgr.EnsureDirectory(p)
	c.Assert(err, ErrorMatches, "path exists and is not a directory: .*")
}
