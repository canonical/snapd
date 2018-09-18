// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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
	"syscall"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/osutil"
)

type sysSuite struct{}

var _ = Suite(&sysSuite{})

func (s *sysSuite) TestSymlinkatAndReadlinkat(c *C) {
	// Create and open a temporary directory.
	d := c.MkDir()
	fd, err := syscall.Open(d, syscall.O_DIRECTORY, 0)
	c.Assert(err, IsNil)
	defer syscall.Close(fd)

	// Create a symlink relative to the directory file descriptor.
	err = osutil.Symlinkat("target", fd, "linkpath")
	c.Assert(err, IsNil)

	// Ensure that the created file is a symlink.
	fname := filepath.Join(d, "linkpath")
	fi, err := os.Lstat(fname)
	c.Assert(err, IsNil)
	c.Assert(fi.Name(), Equals, "linkpath")
	c.Assert(fi.Mode()&os.ModeSymlink, Equals, os.ModeSymlink)

	// Ensure that the symlink target is correct.
	target, err := os.Readlink(fname)
	c.Assert(err, IsNil)
	c.Assert(target, Equals, "target")

	// Use readlinkat with a buffer that fits only part of the target path.
	buf := make([]byte, 2)
	n, err := osutil.Readlinkat(fd, "linkpath", buf)
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 2)
	c.Assert(buf, DeepEquals, []byte{'t', 'a'})

	// Use a buffer that fits all of the target path.
	buf = make([]byte, 100)
	n, err = osutil.Readlinkat(fd, "linkpath", buf)
	c.Assert(err, IsNil)
	c.Assert(n, Equals, len("target"))
	c.Assert(buf[:n], DeepEquals, []byte{'t', 'a', 'r', 'g', 'e', 't'})
}
