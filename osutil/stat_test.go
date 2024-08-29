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

package osutil_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/osutil"
)

type StatTestSuite struct{}

var _ = Suite(&StatTestSuite{})

func (ts *StatTestSuite) TestFileDoesNotExist(c *C) {
	c.Assert(osutil.FileExists("/i-do-not-exist"), Equals, false)
}

func (ts *StatTestSuite) TestFileExistsSimple(c *C) {
	fname := filepath.Join(c.MkDir(), "foo")
	err := os.WriteFile(fname, []byte(fname), 0644)
	c.Assert(err, IsNil)

	c.Assert(osutil.FileExists(fname), Equals, true)
}

func (ts *StatTestSuite) TestFileExistsExistsOddPermissions(c *C) {
	fname := filepath.Join(c.MkDir(), "foo")
	err := os.WriteFile(fname, []byte(fname), 0100)
	c.Assert(err, IsNil)

	c.Assert(osutil.FileExists(fname), Equals, true)
}

func (ts *StatTestSuite) TestIsDirectoryDoesNotExist(c *C) {
	c.Assert(osutil.IsDirectory("/i-do-not-exist"), Equals, false)
}

func (ts *StatTestSuite) TestIsDirectorySimple(c *C) {
	dname := filepath.Join(c.MkDir(), "bar")
	err := os.Mkdir(dname, 0700)
	c.Assert(err, IsNil)

	c.Assert(osutil.IsDirectory(dname), Equals, true)
}

func (ts *StatTestSuite) TestIsSymlink(c *C) {
	sname := filepath.Join(c.MkDir(), "symlink")
	err := os.Symlink("/", sname)
	c.Assert(err, IsNil)

	c.Assert(osutil.IsSymlink(sname), Equals, true)
}

func (ts *StatTestSuite) TestIsSymlinkNoSymlink(c *C) {
	c.Assert(osutil.IsSymlink(c.MkDir()), Equals, false)
}

func (ts *StatTestSuite) TestExecutableExists(c *C) {
	oldPath := os.Getenv("PATH")
	defer os.Setenv("PATH", oldPath)
	d := c.MkDir()
	os.Setenv("PATH", d)
	c.Check(osutil.ExecutableExists("xyzzy"), Equals, false)

	fname := filepath.Join(d, "xyzzy")
	c.Assert(os.WriteFile(fname, []byte{}, 0644), IsNil)
	c.Check(osutil.ExecutableExists("xyzzy"), Equals, false)

	c.Assert(os.Chmod(fname, 0755), IsNil)
	c.Check(osutil.ExecutableExists("xyzzy"), Equals, true)
}

func (ts *StatTestSuite) TestLookPathDefaultGivesCorrectPath(c *C) {
	r := osutil.MockLookPath(func(name string) (string, error) { return "/bin/true", nil })
	defer r()
	c.Assert(osutil.LookPathDefault("true", "/bin/foo"), Equals, "/bin/true")
}

func (ts *StatTestSuite) TestLookPathDefaultReturnsDefaultWhenNotFound(c *C) {
	r := osutil.MockLookPath(func(name string) (string, error) { return "", fmt.Errorf("Not found") })
	defer r()
	c.Assert(osutil.LookPathDefault("bar", "/bin/bla"), Equals, "/bin/bla")
}

func makeTestPath(c *C, path string, mode os.FileMode) string {
	return makeTestPathInDir(c, c.MkDir(), path, mode)
}

func makeTestPathInDir(c *C, dir, path string, mode os.FileMode) string {
	mkdir := strings.HasSuffix(path, "/")
	path = filepath.Join(dir, path)

	if mkdir {
		// request for directory
		c.Assert(os.MkdirAll(path, mode), IsNil)
	} else {
		// request for a file
		c.Assert(os.MkdirAll(filepath.Dir(path), 0755), IsNil)
		c.Assert(os.WriteFile(path, nil, mode), IsNil)
	}

	return path
}

func (ts *StatTestSuite) TestIsWritableDir(c *C) {
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
		writable := osutil.IsWritable(makeTestPath(c, t.path, t.mode))
		c.Check(writable, Equals, t.isWritable, Commentf("incorrect result for %q (%s), got %v, expected %v", t.path, t.mode, writable, t.isWritable))
	}
}

func (ts *StatTestSuite) TestIsDirNotExist(c *C) {
	for _, e := range []error{
		os.ErrNotExist,
		syscall.ENOENT,
		syscall.ENOTDIR,
		&os.PathError{Err: syscall.ENOENT},
		&os.PathError{Err: syscall.ENOTDIR},
		&os.LinkError{Err: syscall.ENOENT},
		&os.LinkError{Err: syscall.ENOTDIR},
		&os.SyscallError{Err: syscall.ENOENT},
		&os.SyscallError{Err: syscall.ENOTDIR},
	} {
		c.Check(osutil.IsDirNotExist(e), Equals, true, Commentf("%#v (%v)", e, e))
	}

	for _, e := range []error{
		nil,
		fmt.Errorf("hello"),
	} {
		c.Check(osutil.IsDirNotExist(e), Equals, false)
	}
}

func (ts *StatTestSuite) TestDirExists(c *C) {
	for _, t := range []struct {
		make   string
		path   string
		exists bool
		isDir  bool
	}{
		{"", "foo", false, false},
		{"", "foo/bar", false, false},
		{"foo", "foo/bar", false, false},
		{"foo", "foo", true, false},
		{"foo/", "foo", true, true},
	} {
		base := c.MkDir()
		comm := Commentf("path:%q make:%q", t.path, t.make)
		if t.make != "" {
			makeTestPathInDir(c, base, t.make, 0755)
		}
		exists, isDir, err := osutil.DirExists(filepath.Join(base, t.path))
		c.Check(exists, Equals, t.exists, comm)
		c.Check(isDir, Equals, t.isDir, comm)
		c.Check(err, IsNil, comm)
	}

	p := makeTestPath(c, "foo/bar", 0)
	c.Assert(os.Chmod(filepath.Dir(p), 0), IsNil)
	defer os.Chmod(filepath.Dir(p), 0755)
	exists, isDir, err := osutil.DirExists(p)
	c.Check(exists, Equals, false)
	c.Check(isDir, Equals, false)
	c.Check(err, NotNil)
}

func (ts *StatTestSuite) TestIsExecutable(c *C) {
	c.Check(osutil.IsExecutable("non-existent"), Equals, false)
	c.Check(osutil.IsExecutable("."), Equals, false)
	dir := c.MkDir()
	c.Check(osutil.IsExecutable(dir), Equals, false)

	for _, tc := range []struct {
		mode os.FileMode
		is   bool
	}{
		{0644, false},
		{0444, false},
		{0444, false},
		{0000, false},
		{0100, true},
		{0010, true},
		{0001, true},
		{0755, true},
	} {
		c.Logf("tc: %v %v", tc.mode, tc.is)
		p := filepath.Join(dir, "foo")
		err := os.Remove(p)
		c.Check(err == nil || os.IsNotExist(err), Equals, true)

		err = os.WriteFile(p, []byte(""), tc.mode)
		c.Assert(err, IsNil)
		c.Check(osutil.IsExecutable(p), Equals, tc.is)
	}
}

func (ts *StatTestSuite) TestRegularFileExists(c *C) {
	tt := []struct {
		make           bool
		makeNonRegular bool
		path           string
		expExists      bool
		expIsReg       bool
		expErr         string
		comment        string
	}{
		{
			make:      true,
			path:      "foo",
			expExists: true,
			expIsReg:  true,
			comment:   "file is regular",
		},
		{
			make:           true,
			makeNonRegular: true,
			path:           "bar",
			expExists:      true,
			comment:        "file is symlink",
		},
		{
			path:      "not-exists",
			expExists: false,
			expErr:    ".*no such file or directory",
			comment:   "file doesn't exist",
		},
	}

	for _, t := range tt {
		fullpath := filepath.Join(c.MkDir(), t.path)
		comment := Commentf(t.comment)

		if t.make {
			if t.makeNonRegular {
				// make it a symlink
				err := os.Symlink("foo", fullpath)
				c.Assert(err, IsNil, comment)
			} else {
				// make it a normal file
				err := os.WriteFile(fullpath, nil, 0644)
				c.Assert(err, IsNil, comment)
			}
		}

		exists, isReg, err := osutil.RegularFileExists(fullpath)
		if t.expErr != "" {
			c.Assert(err, ErrorMatches, t.expErr, comment)
			continue
		}
		c.Assert(exists, Equals, t.expExists, comment)
		c.Assert(isReg, Equals, t.expIsReg, comment)
	}
}

func (ts *StatTestSuite) TestComparePathsByDeviceInodeHappy(c *C) {
	base := c.MkDir()

	// Same file
	path_a := filepath.Join(base, "file-a")
	c.Assert(os.WriteFile(path_a, nil, 0644), IsNil)
	match, err := osutil.ComparePathsByDeviceInode(path_a, path_a)
	c.Assert(err, IsNil)
	c.Assert(match, Equals, true)

	// Different files
	path_b := filepath.Join(base, "file-b")
	c.Assert(os.WriteFile(path_b, nil, 0644), IsNil)
	match, err = osutil.ComparePathsByDeviceInode(path_a, path_b)
	c.Assert(err, IsNil)
	c.Assert(match, Equals, false)

	// Same directory
	match, err = osutil.ComparePathsByDeviceInode(base, base)
	c.Assert(err, IsNil)
	c.Assert(match, Equals, true)

	// Different directories
	path_a = filepath.Join(base, "dir-a")
	c.Assert(os.Mkdir(path_a, 0644), IsNil)
	match, err = osutil.ComparePathsByDeviceInode(base, path_a)
	c.Assert(err, IsNil)
	c.Assert(match, Equals, false)

	// Symlink to directory and directory
	path_b = filepath.Join(base, "symlink-to-dir-a")
	c.Assert(os.Symlink(path_a, path_b), IsNil)
	match, err = osutil.ComparePathsByDeviceInode(path_b, path_b)
	c.Assert(err, IsNil)
	c.Assert(match, Equals, true)

	// Different symlinks to same directory
	path_c := filepath.Join(base, "another-symlink-to-dir-a")
	c.Assert(os.Symlink(path_a, path_c), IsNil)
	match, err = osutil.ComparePathsByDeviceInode(path_b, path_c)
	c.Assert(err, IsNil)
	c.Assert(match, Equals, true)

	// Path including symlink to directory and directory
	path_a = filepath.Join(base, "dir-b/dir-c/dir-e/dir-f")
	c.Assert(os.MkdirAll(path_a, 0755), IsNil)
	path_b = filepath.Join(base, "dir-b/dir-c")
	path_c = filepath.Join(base, "symlink-to-dir-c")
	c.Assert(os.Symlink(path_b, path_c), IsNil)
	match, err = osutil.ComparePathsByDeviceInode(path_a, filepath.Join(path_c, "dir-e/dir-f"))
	c.Assert(err, IsNil)
	c.Assert(match, Equals, true)
}

func (ts *StatTestSuite) TestComparePathsByDeviceInodeErrorPathNotExist(c *C) {
	base := c.MkDir()

	// Path a does not exist
	match, err := osutil.ComparePathsByDeviceInode(filepath.Join(base, "missing-dir"), base)
	c.Assert(err, ErrorMatches, "*: no such file or directory")
	c.Assert(match, Equals, false)

	// Path b does not exist
	match, err = osutil.ComparePathsByDeviceInode(base, filepath.Join(base, "missing-dir"))
	c.Assert(err, ErrorMatches, "*: no such file or directory")
	c.Assert(match, Equals, false)
}
