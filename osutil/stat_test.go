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
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

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

func (s *StatTestSuite) TestLookPathDefaultGivesCorrectPath(c *C) {
	lookPath = func(name string) (string, error) { return "/bin/true", nil }
	c.Assert(LookPathDefault("true", "/bin/foo"), Equals, "/bin/true")
}

func (s *StatTestSuite) TestLookPathDefaultReturnsDefaultWhenNotFound(c *C) {
	lookPath = func(name string) (string, error) { return "", fmt.Errorf("Not found") }
	c.Assert(LookPathDefault("bar", "/bin/bla"), Equals, "/bin/bla")
}

func makeTestPath(c *C, path string, mode os.FileMode) string {
	path = filepath.Join(c.MkDir(), path)

	switch {
	// request for directory
	case strings.HasSuffix(path, "/"):
		err := os.MkdirAll(path, os.FileMode(mode))
		c.Assert(err, IsNil)
	default:
		// request for a file
		err := ioutil.WriteFile(path, nil, os.FileMode(mode))
		c.Assert(err, IsNil)
	}

	return path
}

func (s *StatTestSuite) TestIsWritableDir(c *C) {
	for _, t := range []struct {
		path       string
		mode       os.FileMode
		isWritable bool
	}{
		{"dir/", 0755, true},
		{"dir/", 0555, false},
		{"dir/", 0750, true},
		{"dir/", 0550, false},
		{"dir/", 0700, true},
		{"dir/", 0500, false},

		{"file", 0644, true},
		{"file", 0444, false},
		{"file", 0640, true},
		{"file", 0440, false},
		{"file", 0600, true},
		{"file", 0400, false},
	} {
		writable := IsWritable(makeTestPath(c, t.path, t.mode))
		c.Check(writable, Equals, t.isWritable, Commentf("incorrect result for %q (%s), got %v, expected %v", t.path, t.mode, writable, t.isWritable))
	}
}
