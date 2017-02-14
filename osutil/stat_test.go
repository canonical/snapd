// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package osutil

import (
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"
)

type StatTestSuite struct{}

var _ = Suite(&StatTestSuite{})

func (ts *StatTestSuite) TestFileDoesNotExist(c *C) {
	c.Assert(FileExists("/i-do-not-exist"), Equals, false)
}

func (ts *StatTestSuite) TestFileExistsSimple(c *C) {
	fname := filepath.Join(c.MkDir(), "foo")
	err := ioutil.WriteFile(fname, []byte(fname), 0644)
	c.Assert(err, IsNil)

	c.Assert(FileExists(fname), Equals, true)
}

func (ts *StatTestSuite) TestFileExistsExistsOddPermissions(c *C) {
	fname := filepath.Join(c.MkDir(), "foo")
	err := ioutil.WriteFile(fname, []byte(fname), 0100)
	c.Assert(err, IsNil)

	c.Assert(FileExists(fname), Equals, true)
}

func (ts *StatTestSuite) TestIsDirectoryDoesNotExist(c *C) {
	c.Assert(IsDirectory("/i-do-not-exist"), Equals, false)
}

func (ts *StatTestSuite) TestIsDirectorySimple(c *C) {
	dname := filepath.Join(c.MkDir(), "bar")
	err := os.Mkdir(dname, 0700)
	c.Assert(err, IsNil)

	c.Assert(IsDirectory(dname), Equals, true)
}

func (ts *StatTestSuite) TestIsSymlink(c *C) {
	sname := filepath.Join(c.MkDir(), "symlink")
	err := os.Symlink("/", sname)
	c.Assert(err, IsNil)

	c.Assert(IsSymlink(sname), Equals, true)
}

func (ts *StatTestSuite) TestIsSymlinkNoSymlink(c *C) {
	c.Assert(IsSymlink(c.MkDir()), Equals, false)
}

func (ts *StatTestSuite) TestExecutableExists(c *C) {
	oldPath := os.Getenv("PATH")
	defer os.Setenv("PATH", oldPath)
	d := c.MkDir()
	os.Setenv("PATH", d)
	c.Check(ExecutableExists("xyzzy"), Equals, false)

	fname := filepath.Join(d, "xyzzy")
	c.Assert(ioutil.WriteFile(fname, []byte{}, 0644), IsNil)
	c.Check(ExecutableExists("xyzzy"), Equals, false)

	c.Assert(os.Chmod(fname, 0755), IsNil)
	c.Check(ExecutableExists("xyzzy"), Equals, true)
}
