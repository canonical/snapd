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
	"io/ioutil"
	"os"
	"path/filepath"
	"syscall"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/testutil"
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

func (s *sysSuite) TestExchangeFilesBothFilesMissing(c *C) {
	// Create and open a temporary directory.
	d := c.MkDir()
	fd, err := syscall.Open(d, syscall.O_DIRECTORY, 0)
	c.Assert(err, IsNil)
	defer syscall.Close(fd)

	missingA := filepath.Join(d, "missing-a")
	missingB := filepath.Join(d, "missing-b")

	// Both missing-a and missing-b do not exist.
	c.Assert(missingA, testutil.FileAbsent)
	c.Assert(missingB, testutil.FileAbsent)

	// Exchange fails with ENOENT
	err = osutil.ExchangeFiles(fd, "missing-a", fd, "missing-b")
	c.Assert(err, Equals, syscall.ENOENT)
	c.Assert(os.IsNotExist(err), Equals, true)

	// Exchange did not clobber file system state.
	c.Assert(missingA, testutil.FileAbsent)
	c.Assert(missingB, testutil.FileAbsent)
}

func (s *sysSuite) TestExchangeFilesFirstFileMissing(c *C) {
	// Create and open a temporary directory.
	d := c.MkDir()
	fd, err := syscall.Open(d, syscall.O_DIRECTORY, 0)
	c.Assert(err, IsNil)
	defer syscall.Close(fd)

	missing := filepath.Join(d, "missing")
	regular := filepath.Join(d, "regular")

	err = ioutil.WriteFile(regular, []byte("regular"), 0644)
	c.Assert(err, IsNil)

	// Both the missing file is absent, the regular file is present.
	c.Assert(missing, testutil.FileAbsent)
	c.Assert(regular, testutil.FilePresent)

	// Exchange fails with ENOENT because the first file is missing.
	err = osutil.ExchangeFiles(fd, "missing", fd, "regular")
	c.Assert(err, Equals, syscall.ENOENT)
	c.Assert(os.IsNotExist(err), Equals, true)

	// Exchange did not clobber file system state.
	c.Assert(missing, testutil.FileAbsent)
	c.Assert(regular, testutil.FileEquals, "regular")
}

func (s *sysSuite) TestExchangeFilesSecondFileMissing(c *C) {
	// Create and open a temporary directory.
	d := c.MkDir()
	fd, err := syscall.Open(d, syscall.O_DIRECTORY, 0)
	c.Assert(err, IsNil)
	defer syscall.Close(fd)

	missing := filepath.Join(d, "missing")
	regular := filepath.Join(d, "regular")

	err = ioutil.WriteFile(regular, []byte("regular"), 0644)
	c.Assert(err, IsNil)

	// Both the missing file is absent, the regular file is present.
	c.Assert(missing, testutil.FileAbsent)
	c.Assert(regular, testutil.FilePresent)

	// Exchange fails with ENOENT because the second file is missing.
	err = osutil.ExchangeFiles(fd, "regular", fd, "missing")
	c.Assert(err, Equals, syscall.ENOENT)
	c.Assert(os.IsNotExist(err), Equals, true)

	// Exchange did not clobber file system state.
	c.Assert(missing, testutil.FileAbsent)
	c.Assert(regular, testutil.FileEquals, "regular")
}

func (s *sysSuite) TestExchangeTwoRegularFiles(c *C) {
	// Create and open a temporary directory.
	d := c.MkDir()
	fd, err := syscall.Open(d, syscall.O_DIRECTORY, 0)
	c.Assert(err, IsNil)
	defer syscall.Close(fd)

	regularA := filepath.Join(d, "regular-a")
	regularB := filepath.Join(d, "regular-b")

	err = ioutil.WriteFile(regularA, []byte("regular-a"), 0644)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(regularB, []byte("regular-b"), 0644)
	c.Assert(err, IsNil)

	// Both regular files are present and have content matching their name.
	c.Assert(regularA, testutil.FileEquals, "regular-a")
	c.Assert(regularB, testutil.FileEquals, "regular-b")

	// Exchange succeeds.
	err = osutil.ExchangeFiles(fd, "regular-a", fd, "regular-b")
	c.Assert(err, IsNil)

	// Exchange swapped the content of the two files.
	c.Assert(regularA, testutil.FileEquals, "regular-b")
	c.Assert(regularB, testutil.FileEquals, "regular-a")
}

func (s *sysSuite) TestExchangeTwoSymbolicLinks(c *C) {
	// Create and open a temporary directory.
	d := c.MkDir()
	fd, err := syscall.Open(d, syscall.O_DIRECTORY, 0)
	c.Assert(err, IsNil)
	defer syscall.Close(fd)

	symlinkA := filepath.Join(d, "symlink-a")
	symlinkB := filepath.Join(d, "symlink-b")

	err = os.Symlink("target-a", symlinkA)
	c.Assert(err, IsNil)
	err = os.Symlink("target-b", symlinkB)
	c.Assert(err, IsNil)

	// Both symbolic links are pointing at their respective targets.
	c.Assert(symlinkA, testutil.SymlinkTargetEquals, "target-a")
	c.Assert(symlinkB, testutil.SymlinkTargetEquals, "target-b")

	// Exchange succeeds.
	err = osutil.ExchangeFiles(fd, "symlink-a", fd, "symlink-b")
	c.Assert(err, IsNil)

	// Exchange swapped the two targets.
	c.Assert(symlinkA, testutil.SymlinkTargetEquals, "target-b")
	c.Assert(symlinkB, testutil.SymlinkTargetEquals, "target-a")
}
