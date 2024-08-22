// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

package osutil_test

import (
	"os"
	"os/user"
	"strconv"
	"syscall"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/sys"
)

type mkdirSuite struct{}

var _ = check.Suite(&mkdirSuite{})

// Chown requires root, so it's not tested, only test MakeParents, ExistOK, Chmod,
// and the combination of them.
func (mkdirSuite) TestSimpleDir(c *check.C) {
	tmpDir := c.MkDir()

	err := osutil.Mkdir(tmpDir+"/foo", 0o755, nil)
	c.Assert(err, check.IsNil)
	c.Assert(osutil.IsDirectory(tmpDir+"/foo"), check.Equals, true)
}

func (mkdirSuite) TestExistOK(c *check.C) {
	tmpDir := c.MkDir()

	err := osutil.Mkdir(tmpDir+"/foo", 0o755, nil)
	c.Assert(err, check.IsNil)
	c.Assert(osutil.IsDirectory(tmpDir+"/foo"), check.Equals, true)

	err = osutil.Mkdir(tmpDir+"/foo", 0o755, &osutil.MkdirOptions{
		ExistOK: true,
	})
	c.Assert(err, check.IsNil)
}

func (mkdirSuite) TestExistNotOK(c *check.C) {
	tmpDir := c.MkDir()

	err := osutil.Mkdir(tmpDir+"/foo", 0o755, nil)
	c.Assert(err, check.IsNil)
	c.Assert(osutil.IsDirectory(tmpDir+"/foo"), check.Equals, true)

	err = osutil.Mkdir(tmpDir+"/foo", 0o755, nil)
	c.Assert(err, check.ErrorMatches, `.*: file exists`)
}

func (mkdirSuite) TestDirEndWithSlash(c *check.C) {
	tmpDir := c.MkDir()

	err := osutil.Mkdir(tmpDir+"/foo/", 0755, nil)
	c.Assert(err, check.IsNil)
}

func (mkdirSuite) TestMakeParents(c *check.C) {
	tmpDir := c.MkDir()

	err := osutil.Mkdir(tmpDir+"/foo/bar", 0o755, &osutil.MkdirOptions{
		MakeParents: true,
	})
	c.Assert(err, check.IsNil)
	c.Assert(osutil.IsDirectory(tmpDir+"/foo"), check.Equals, true)
	c.Assert(osutil.IsDirectory(tmpDir+"/foo/bar"), check.Equals, true)
}

func (mkdirSuite) TestMakeParentsAndExistOK(c *check.C) {
	tmpDir := c.MkDir()

	err := osutil.Mkdir(tmpDir+"/foo/bar", 0o755, &osutil.MkdirOptions{
		MakeParents: true,
	})
	c.Assert(err, check.IsNil)
	c.Assert(osutil.IsDirectory(tmpDir+"/foo"), check.Equals, true)
	c.Assert(osutil.IsDirectory(tmpDir+"/foo/bar"), check.Equals, true)

	err = osutil.Mkdir(tmpDir+"/foo/bar/", 0o755, &osutil.MkdirOptions{
		ExistOK: true,
	})
	c.Assert(err, check.IsNil)
}

func (mkdirSuite) TestMakeParentsAndExistNotOK(c *check.C) {
	tmpDir := c.MkDir()

	err := osutil.Mkdir(tmpDir+"/foo/bar", 0o755, &osutil.MkdirOptions{
		MakeParents: true,
	})
	c.Assert(err, check.IsNil)
	c.Assert(osutil.IsDirectory(tmpDir+"/foo"), check.Equals, true)
	c.Assert(osutil.IsDirectory(tmpDir+"/foo/bar"), check.Equals, true)

	err = osutil.Mkdir(tmpDir+"/foo/bar", 0o755, nil)
	c.Assert(err, check.ErrorMatches, `.*: file exists`)
}

func (mkdirSuite) TestChmod(c *check.C) {
	oldmask := syscall.Umask(0022)
	defer syscall.Umask(oldmask)

	tmpDir := c.MkDir()

	err := osutil.Mkdir(tmpDir+"/foo", 0o777, &osutil.MkdirOptions{
		Chmod: true,
	})
	c.Assert(err, check.IsNil)
	c.Assert(osutil.IsDirectory(tmpDir+"/foo"), check.Equals, true)

	info, err := os.Stat(tmpDir + "/foo")
	c.Assert(err, check.IsNil)
	c.Assert(info.Mode().Perm(), check.Equals, os.FileMode(0o777))
}

func (mkdirSuite) TestNoChmod(c *check.C) {
	oldmask := syscall.Umask(0022)
	defer syscall.Umask(oldmask)

	tmpDir := c.MkDir()

	err := osutil.Mkdir(tmpDir+"/foo", 0o777, nil)
	c.Assert(err, check.IsNil)
	c.Assert(osutil.IsDirectory(tmpDir+"/foo"), check.Equals, true)

	info, err := os.Stat(tmpDir + "/foo")
	c.Assert(err, check.IsNil)
	c.Assert(info.Mode().Perm(), check.Equals, os.FileMode(0o755))
}

func (mkdirSuite) TestMakeParentsAndChmod(c *check.C) {
	tmpDir := c.MkDir()

	err := osutil.Mkdir(tmpDir+"/foo/bar", 0o777, &osutil.MkdirOptions{
		MakeParents: true,
		Chmod:       true,
	})
	c.Assert(err, check.IsNil)
	c.Assert(osutil.IsDirectory(tmpDir+"/foo"), check.Equals, true)
	c.Assert(osutil.IsDirectory(tmpDir+"/foo/bar"), check.Equals, true)

	info, err := os.Stat(tmpDir + "/foo")
	c.Assert(err, check.IsNil)
	c.Assert(info.Mode().Perm(), check.Equals, os.FileMode(0o777))

	info, err = os.Stat(tmpDir + "/foo/bar")
	c.Assert(err, check.IsNil)
	c.Assert(info.Mode().Perm(), check.Equals, os.FileMode(0o777))
}

func (mkdirSuite) TestMakeParentsAndNoChmod(c *check.C) {
	oldmask := syscall.Umask(0022)
	defer syscall.Umask(oldmask)

	tmpDir := c.MkDir()

	err := osutil.Mkdir(tmpDir+"/foo/bar", 0o777, &osutil.MkdirOptions{
		MakeParents: true,
	})
	c.Assert(err, check.IsNil)
	c.Assert(osutil.IsDirectory(tmpDir+"/foo"), check.Equals, true)
	c.Assert(osutil.IsDirectory(tmpDir+"/foo/bar"), check.Equals, true)

	info, err := os.Stat(tmpDir + "/foo")
	c.Assert(err, check.IsNil)
	c.Assert(info.Mode().Perm(), check.Equals, os.FileMode(0o755))

	info, err = os.Stat(tmpDir + "/foo/bar")
	c.Assert(err, check.IsNil)
	c.Assert(info.Mode().Perm(), check.Equals, os.FileMode(0o755))
}

// See .github/workflows/tests.yml for how to run this test as root.
func (mkdirSuite) TestMakeParentsChmodAndChown(c *check.C) {
	if os.Getuid() != 0 {
		c.Skip("requires running as root")
	}

	username := os.Getenv("PEBBLE_TEST_USER")
	group := os.Getenv("PEBBLE_TEST_GROUP")
	if username == "" || group == "" {
		c.Fatalf("must set PEBBLE_TEST_USER and PEBBLE_TEST_GROUP")
	}

	u, err := user.Lookup(username)
	c.Assert(err, check.IsNil)
	g, err := user.LookupGroup(group)
	c.Assert(err, check.IsNil)
	uid, err := strconv.Atoi(u.Uid)
	c.Assert(err, check.IsNil)
	gid, err := strconv.Atoi(g.Gid)
	c.Assert(err, check.IsNil)
	tmpDir := c.MkDir()

	err = osutil.Mkdir(tmpDir+"/foo/bar", 0o777, &osutil.MkdirOptions{
		MakeParents: true,
		Chmod:       true,
		Chown:       true,
		UserID:      sys.UserID(uid),
		GroupID:     sys.GroupID(gid),
	})
	c.Assert(err, check.IsNil)
	c.Assert(osutil.IsDirectory(tmpDir+"/foo"), check.Equals, true)
	c.Assert(osutil.IsDirectory(tmpDir+"/foo/bar"), check.Equals, true)

	info, err := os.Stat(tmpDir + "/foo")
	c.Assert(err, check.IsNil)
	c.Assert(info.Mode().Perm(), check.Equals, os.FileMode(0o777))
	stat, ok := info.Sys().(*syscall.Stat_t)
	c.Assert(ok, check.Equals, true)
	c.Assert(int(stat.Uid), check.Equals, uid)
	c.Assert(int(stat.Gid), check.Equals, gid)

	info, err = os.Stat(tmpDir + "/foo/bar")
	c.Assert(err, check.IsNil)
	c.Assert(info.Mode().Perm(), check.Equals, os.FileMode(0o777))
	stat, ok = info.Sys().(*syscall.Stat_t)
	c.Assert(ok, check.Equals, true)
	c.Assert(int(stat.Uid), check.Equals, uid)
	c.Assert(int(stat.Gid), check.Equals, gid)
}
