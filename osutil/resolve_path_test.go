// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/osutil"
)

type resolvePathSuite struct{}

var _ = Suite(&resolvePathSuite{})

func (s *resolvePathSuite) TestSimple(c *C) {
	sysroot := c.MkDir()
	err := os.MkdirAll(filepath.Join(sysroot, "foo/bar"), 0700)
	c.Assert(err, IsNil)

	resolved, err := osutil.ResolvePathInSysroot(sysroot, "/foo/bar")
	c.Assert(err, IsNil)
	c.Assert(resolved, Equals, "/foo/bar")
}

func (s *resolvePathSuite) TestSimpleRelative(c *C) {
	sysroot := c.MkDir()
	err := os.MkdirAll(filepath.Join(sysroot, "foo/bar"), 0700)
	c.Assert(err, IsNil)

	resolved, err := osutil.ResolvePathInSysroot(sysroot, "foo/bar")
	c.Assert(err, IsNil)
	c.Assert(resolved, Equals, "/foo/bar")
}

func (s *resolvePathSuite) TestDot(c *C) {
	sysroot := c.MkDir()
	err := os.MkdirAll(filepath.Join(sysroot, "foo/bar"), 0700)
	c.Assert(err, IsNil)

	resolved, err := osutil.ResolvePathInSysroot(sysroot, "/./foo/./bar/.")
	c.Assert(err, IsNil)
	c.Assert(resolved, Equals, "/foo/bar")
}

func (s *resolvePathSuite) TestEmpty(c *C) {
	sysroot := c.MkDir()
	err := os.MkdirAll(filepath.Join(sysroot, "foo/bar"), 0700)
	c.Assert(err, IsNil)

	resolved, err := osutil.ResolvePathInSysroot(sysroot, "//foo/////bar//")
	c.Assert(err, IsNil)
	c.Assert(resolved, Equals, "/foo/bar")
}

func (s *resolvePathSuite) TestDotDot(c *C) {
	sysroot := c.MkDir()
	err := os.MkdirAll(filepath.Join(sysroot, "foo/bar"), 0700)
	c.Assert(err, IsNil)

	resolved, err := osutil.ResolvePathInSysroot(sysroot, "../../../../foo/bar")
	c.Assert(err, IsNil)
	c.Assert(resolved, Equals, "/foo/bar")
}

func (s *resolvePathSuite) TestDotDotInSymlink(c *C) {
	sysroot := c.MkDir()
	err := os.MkdirAll(filepath.Join(sysroot, "foo"), 0700)
	c.Assert(err, IsNil)
	err = os.Symlink("../../../../../../..", filepath.Join(sysroot, "foo", "bar"))
	c.Assert(err, IsNil)
	err = os.MkdirAll(filepath.Join(sysroot, "etc"), 0700)
	c.Assert(err, IsNil)
	file, err := os.Create(filepath.Join(sysroot, "etc", "passwd"))
	c.Assert(err, IsNil)
	defer file.Close()

	resolved, err := osutil.ResolvePathInSysroot(sysroot, "/foo/bar/etc/passwd")
	c.Assert(err, IsNil)
	c.Assert(resolved, Equals, "/etc/passwd")
}

func (s *resolvePathSuite) TestSymlinkRecursion(c *C) {
	sysroot := c.MkDir()
	err := os.MkdirAll(filepath.Join(sysroot, "foo"), 0700)
	c.Assert(err, IsNil)
	err = os.MkdirAll(filepath.Join(sysroot, "bar"), 0700)
	c.Assert(err, IsNil)
	err = os.Symlink("../bar/baz", filepath.Join(sysroot, "foo", "baz"))
	c.Assert(err, IsNil)
	err = os.Symlink("../foo/baz", filepath.Join(sysroot, "bar", "baz"))
	c.Assert(err, IsNil)

	_, err = osutil.ResolvePathInSysroot(sysroot, "/foo/baz/some/path")
	c.Assert(err, ErrorMatches, "maximum recursion reached when reading symlinks")
}
