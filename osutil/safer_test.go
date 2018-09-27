// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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
	"errors"
	"io/ioutil"
	"os"
	"path/filepath"
	"syscall"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/sys"
	"github.com/snapcore/snapd/testutil"
)

type saferSuite struct {
	testutil.BaseTest
	sys *testutil.SyscallRecorder
	as  osutil.Assumptions
}

var _ = Suite(&saferSuite{})

func (s *saferSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.sys = &testutil.SyscallRecorder{}
	s.BaseTest.AddCleanup(osutil.MockSystemCalls(s.sys))
}

func (s *saferSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
	s.sys.CheckForStrayDescriptors(c)
}

var errTesting = errors.New("testing")

// secure-open-path

func (s *saferSuite) TestSecureOpenPath(c *C) {
	stat := syscall.Stat_t{Mode: syscall.S_IFDIR}
	s.sys.InsertFstatResult("fstat 5 <ptr>", stat)
	fd, err := osutil.OpenPath("/foo/bar")
	c.Assert(err, IsNil)
	defer s.sys.Close(fd)
	c.Assert(fd, Equals, 5)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "foo" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 4},
		{C: `openat 4 "bar" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 5},
		{C: `fstat 5 <ptr>`, R: stat},
		{C: `close 4`},
		{C: `close 3`},
	})
}

func (s *saferSuite) TestSecureOpenPathSingleSegment(c *C) {
	stat := syscall.Stat_t{Mode: syscall.S_IFDIR}
	s.sys.InsertFstatResult("fstat 4 <ptr>", stat)
	fd, err := osutil.OpenPath("/foo")
	c.Assert(err, IsNil)
	defer s.sys.Close(fd)
	c.Assert(fd, Equals, 4)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "foo" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 4},
		{C: `fstat 4 <ptr>`, R: stat},
		{C: `close 3`},
	})
}

func (s *saferSuite) TestSecureOpenPathRoot(c *C) {
	stat := syscall.Stat_t{Mode: syscall.S_IFDIR}
	s.sys.InsertFstatResult("fstat 3 <ptr>", stat)
	fd, err := osutil.OpenPath("/")
	c.Assert(err, IsNil)
	defer s.sys.Close(fd)
	c.Assert(fd, Equals, 3)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `fstat 3 <ptr>`, R: stat},
	})
}

// secure-mkdir-all

// Ensure that we reject unclean paths.
func (s *saferSuite) TestSecureMkdirAllUnclean(c *C) {
	err := osutil.MkdirAll("/unclean//path", 0755, 123, 456, nil)
	c.Assert(err, ErrorMatches, `cannot split unclean path .*`)
	c.Assert(s.sys.RCalls(), HasLen, 0)
}

// Ensure that we refuse to create a directory with an relative path.
func (s *saferSuite) TestSecureMkdirAllRelative(c *C) {
	err := osutil.MkdirAll("rel/path", 0755, 123, 456, nil)
	c.Assert(err, ErrorMatches, `cannot create directory with relative path: "rel/path"`)
	c.Assert(s.sys.RCalls(), HasLen, 0)
}

// Ensure that we can "create the root directory.
func (s *saferSuite) TestSecureMkdirAllLevel0(c *C) {
	c.Assert(osutil.MkdirAll("/", 0755, 123, 456, nil), IsNil)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `close 3`},
	})
}

// Ensure that we can create a directory in the top-level directory.
func (s *saferSuite) TestSecureMkdirAllLevel1(c *C) {
	c.Assert(osutil.MkdirAll("/path", 0755, 123, 456, nil), IsNil)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "path" 0755`},
		{C: `openat 3 "path" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `fchown 4 123 456`},
		{C: `close 4`},
		{C: `close 3`},
	})
}

// Ensure that we can create a directory two levels from the top-level directory.
func (s *saferSuite) TestSecureMkdirAllLevel2(c *C) {
	c.Assert(osutil.MkdirAll("/path/to", 0755, 123, 456, nil), IsNil)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "path" 0755`},
		{C: `openat 3 "path" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `fchown 4 123 456`},
		{C: `close 3`},
		{C: `mkdirat 4 "to" 0755`},
		{C: `openat 4 "to" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `fchown 3 123 456`},
		{C: `close 3`},
		{C: `close 4`},
	})
}

// Ensure that we can create a directory three levels from the top-level directory.
func (s *saferSuite) TestSecureMkdirAllLevel3(c *C) {
	c.Assert(osutil.MkdirAll("/path/to/something", 0755, 123, 456, nil), IsNil)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "path" 0755`},
		{C: `openat 3 "path" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `fchown 4 123 456`},
		{C: `mkdirat 4 "to" 0755`},
		{C: `openat 4 "to" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 5},
		{C: `fchown 5 123 456`},
		{C: `close 4`},
		{C: `close 3`},
		{C: `mkdirat 5 "something" 0755`},
		{C: `openat 5 "something" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `fchown 3 123 456`},
		{C: `close 3`},
		{C: `close 5`},
	})
}

// Ensure that we can detect read only filesystems.
func (s *saferSuite) TestSecureMkdirAllROFS(c *C) {
	s.sys.InsertFault(`mkdirat 3 "rofs" 0755`, syscall.EEXIST) // just realistic
	s.sys.InsertFault(`mkdirat 4 "path" 0755`, syscall.EROFS)
	err := osutil.MkdirAll("/rofs/path", 0755, 123, 456, nil)
	c.Assert(err, ErrorMatches, `cannot operate on read-only filesystem at /rofs`)
	c.Assert(err.(*osutil.ReadOnlyFsError).Path, Equals, "/rofs")
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "rofs" 0755`, E: syscall.EEXIST},
		{C: `openat 3 "rofs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `close 3`},
		{C: `mkdirat 4 "path" 0755`, E: syscall.EROFS},
		{C: `close 4`},
	})
}

// Ensure that we don't chown existing directories.
func (s *saferSuite) TestSecureMkdirAllExistingDirsDontChown(c *C) {
	s.sys.InsertFault(`mkdirat 3 "abs" 0755`, syscall.EEXIST)
	s.sys.InsertFault(`mkdirat 4 "path" 0755`, syscall.EEXIST)
	err := osutil.MkdirAll("/abs/path", 0755, 123, 456, nil)
	c.Assert(err, IsNil)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "abs" 0755`, E: syscall.EEXIST},
		{C: `openat 3 "abs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `close 3`},
		{C: `mkdirat 4 "path" 0755`, E: syscall.EEXIST},
		{C: `openat 4 "path" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `close 3`},
		{C: `close 4`},
	})
}

// Ensure that we we close everything when mkdirat fails.
func (s *saferSuite) TestSecureMkdirAllMkdiratError(c *C) {
	s.sys.InsertFault(`mkdirat 3 "abs" 0755`, errTesting)
	err := osutil.MkdirAll("/abs", 0755, 123, 456, nil)
	c.Assert(err, ErrorMatches, `cannot create directory "/abs": testing`)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "abs" 0755`, E: errTesting},
		{C: `close 3`},
	})
}

// Ensure that we we close everything when fchown fails.
func (s *saferSuite) TestSecureMkdirAllFchownError(c *C) {
	s.sys.InsertFault(`fchown 4 123 456`, errTesting)
	err := osutil.MkdirAll("/path", 0755, 123, 456, nil)
	c.Assert(err, ErrorMatches, `cannot chown directory "/path" to 123.456: testing`)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "path" 0755`},
		{C: `openat 3 "path" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `fchown 4 123 456`, E: errTesting},
		{C: `close 4`},
		{C: `close 3`},
	})
}

// Check error path when we cannot open root directory.
func (s *saferSuite) TestSecureMkdirAllOpenRootError(c *C) {
	s.sys.InsertFault(`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, errTesting)
	err := osutil.MkdirAll("/abs/path", 0755, 123, 456, nil)
	c.Assert(err, ErrorMatches, "cannot open root directory: testing")
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, E: errTesting},
	})
}

// Check error path when we cannot open non-root directory.
func (s *saferSuite) TestSecureMkdirAllOpenError(c *C) {
	s.sys.InsertFault(`openat 3 "abs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, errTesting)
	err := osutil.MkdirAll("/abs/path", 0755, 123, 456, nil)
	c.Assert(err, ErrorMatches, `cannot open directory "/abs": testing`)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "abs" 0755`},
		{C: `openat 3 "abs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, E: errTesting},
		{C: `close 3`},
	})
}

// secure-mkfile-all

// Ensure that we reject unclean paths.
func (s *saferSuite) TestSecureMkfileAllUnclean(c *C) {
	err := osutil.MkfileAll("/unclean//path", 0755, 123, 456, nil)
	c.Assert(err, ErrorMatches, `cannot split unclean path .*`)
	c.Assert(s.sys.RCalls(), HasLen, 0)
}

// Ensure that we refuse to create a file with an relative path.
func (s *saferSuite) TestSecureMkfileAllRelative(c *C) {
	err := osutil.MkfileAll("rel/path", 0755, 123, 456, nil)
	c.Assert(err, ErrorMatches, `cannot create file with relative path: "rel/path"`)
	c.Assert(s.sys.RCalls(), HasLen, 0)
}

// Ensure that we refuse creating the root directory as a file.
func (s *saferSuite) TestSecureMkfileAllLevel0(c *C) {
	err := osutil.MkfileAll("/", 0755, 123, 456, nil)
	c.Assert(err, ErrorMatches, `cannot create non-file path: "/"`)
	c.Assert(s.sys.RCalls(), HasLen, 0)
}

// Ensure that we can create a file in the top-level directory.
func (s *saferSuite) TestSecureMkfileAllLevel1(c *C) {
	c.Assert(osutil.MkfileAll("/path", 0755, 123, 456, nil), IsNil)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `openat 3 "path" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`, R: 4},
		{C: `fchown 4 123 456`},
		{C: `close 4`},
		{C: `close 3`},
	})
}

// Ensure that we can create a file two levels from the top-level directory.
func (s *saferSuite) TestSecureMkfileAllLevel2(c *C) {
	c.Assert(osutil.MkfileAll("/path/to", 0755, 123, 456, nil), IsNil)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "path" 0755`},
		{C: `openat 3 "path" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `fchown 4 123 456`},
		{C: `close 3`},
		{C: `openat 4 "to" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`, R: 3},
		{C: `fchown 3 123 456`},
		{C: `close 3`},
		{C: `close 4`},
	})
}

// Ensure that we can create a file three levels from the top-level directory.
func (s *saferSuite) TestSecureMkfileAllLevel3(c *C) {
	c.Assert(osutil.MkfileAll("/path/to/something", 0755, 123, 456, nil), IsNil)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "path" 0755`},
		{C: `openat 3 "path" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `fchown 4 123 456`},
		{C: `mkdirat 4 "to" 0755`},
		{C: `openat 4 "to" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 5},
		{C: `fchown 5 123 456`},
		{C: `close 4`},
		{C: `close 3`},
		{C: `openat 5 "something" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`, R: 3},
		{C: `fchown 3 123 456`},
		{C: `close 3`},
		{C: `close 5`},
	})
}

// Ensure that we can detect read only filesystems.
func (s *saferSuite) TestSecureMkfileAllROFS(c *C) {
	s.sys.InsertFault(`mkdirat 3 "rofs" 0755`, syscall.EEXIST) // just realistic
	s.sys.InsertFault(`openat 4 "path" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`, syscall.EROFS)
	err := osutil.MkfileAll("/rofs/path", 0755, 123, 456, nil)
	c.Check(err, ErrorMatches, `cannot operate on read-only filesystem at /rofs`)
	c.Assert(err.(*osutil.ReadOnlyFsError).Path, Equals, "/rofs")
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "rofs" 0755`, E: syscall.EEXIST},
		{C: `openat 3 "rofs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `close 3`},
		{C: `openat 4 "path" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`, E: syscall.EROFS},
		{C: `close 4`},
	})
}

// Ensure that we don't chown existing files or directories.
func (s *saferSuite) TestSecureMkfileAllExistingDirsDontChown(c *C) {
	s.sys.InsertFault(`mkdirat 3 "abs" 0755`, syscall.EEXIST)
	s.sys.InsertFault(`openat 4 "path" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`, syscall.EEXIST)
	err := osutil.MkfileAll("/abs/path", 0755, 123, 456, nil)
	c.Check(err, IsNil)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "abs" 0755`, E: syscall.EEXIST},
		{C: `openat 3 "abs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `close 3`},
		{C: `openat 4 "path" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`, E: syscall.EEXIST},
		{C: `openat 4 "path" O_NOFOLLOW|O_CLOEXEC 0`, R: 3},
		{C: `close 3`},
		{C: `close 4`},
	})
}

// Ensure that we we close everything when openat fails.
func (s *saferSuite) TestSecureMkfileAllOpenat2ndError(c *C) {
	s.sys.InsertFault(`openat 3 "abs" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`, syscall.EEXIST)
	s.sys.InsertFault(`openat 3 "abs" O_NOFOLLOW|O_CLOEXEC 0`, errTesting)
	err := osutil.MkfileAll("/abs", 0755, 123, 456, nil)
	c.Assert(err, ErrorMatches, `cannot open file "/abs": testing`)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `openat 3 "abs" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`, E: syscall.EEXIST},
		{C: `openat 3 "abs" O_NOFOLLOW|O_CLOEXEC 0`, E: errTesting},
		{C: `close 3`},
	})
}

// Ensure that we we close everything when openat (non-exclusive) fails.
func (s *saferSuite) TestSecureMkfileAllOpenatError(c *C) {
	s.sys.InsertFault(`openat 3 "abs" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`, errTesting)
	err := osutil.MkfileAll("/abs", 0755, 123, 456, nil)
	c.Assert(err, ErrorMatches, `cannot open file "/abs": testing`)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `openat 3 "abs" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`, E: errTesting},
		{C: `close 3`},
	})
}

// Ensure that we we close everything when fchown fails.
func (s *saferSuite) TestSecureMkfileAllFchownError(c *C) {
	s.sys.InsertFault(`fchown 4 123 456`, errTesting)
	err := osutil.MkfileAll("/path", 0755, 123, 456, nil)
	c.Assert(err, ErrorMatches, `cannot chown file "/path" to 123.456: testing`)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `openat 3 "path" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`, R: 4},
		{C: `fchown 4 123 456`, E: errTesting},
		{C: `close 4`},
		{C: `close 3`},
	})
}

// Check error path when we cannot open root directory.
func (s *saferSuite) TestSecureMkfileAllOpenRootError(c *C) {
	s.sys.InsertFault(`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, errTesting)
	err := osutil.MkfileAll("/abs/path", 0755, 123, 456, nil)
	c.Assert(err, ErrorMatches, "cannot open root directory: testing")
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, E: errTesting},
	})
}

// Check error path when we cannot open non-root directory.
func (s *saferSuite) TestSecureMkfileAllOpenError(c *C) {
	s.sys.InsertFault(`openat 3 "abs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, errTesting)
	err := osutil.MkfileAll("/abs/path", 0755, 123, 456, nil)
	c.Assert(err, ErrorMatches, `cannot open directory "/abs": testing`)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "abs" 0755`},
		{C: `openat 3 "abs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, E: errTesting},
		{C: `close 3`},
	})
}

// We want to create a symlink in $SNAP_DATA and that's fine.
func (s *saferSuite) TestSecureMksymlinkAllInSnapData(c *C) {
	s.sys.InsertFault(`mkdirat 3 "var" 0755`, syscall.EEXIST)
	s.sys.InsertFault(`mkdirat 4 "snap" 0755`, syscall.EEXIST)
	s.sys.InsertFault(`mkdirat 5 "foo" 0755`, syscall.EEXIST)
	s.sys.InsertFault(`mkdirat 6 "42" 0755`, syscall.EEXIST)

	err := osutil.MksymlinkAll("/var/snap/foo/42/symlink", 0755, 0, 0, "/oldname", nil)
	c.Assert(err, IsNil)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "var" 0755`, E: syscall.EEXIST},
		{C: `openat 3 "var" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `mkdirat 4 "snap" 0755`, E: syscall.EEXIST},
		{C: `openat 4 "snap" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 5},
		{C: `mkdirat 5 "foo" 0755`, E: syscall.EEXIST},
		{C: `openat 5 "foo" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 6},
		{C: `mkdirat 6 "42" 0755`, E: syscall.EEXIST},
		{C: `openat 6 "42" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 7},
		{C: `close 6`},
		{C: `close 5`},
		{C: `close 4`},
		{C: `close 3`},
		{C: `symlinkat "/oldname" 7 "symlink"`},
		{C: `close 7`},
	})
}

// realSystemSuite is not isolated / mocked from the system.
type realSystemSuite struct{}

var _ = Suite(&realSystemSuite{})

func (s *realSystemSuite) TestSecureOpenPathDirectory(c *C) {
	path := filepath.Join(c.MkDir(), "test")
	c.Assert(os.Mkdir(path, 0755), IsNil)

	fd, err := osutil.OpenPath(path)
	c.Assert(err, IsNil)
	defer syscall.Close(fd)

	// check that the file descriptor is for the expected path
	origDir, err := os.Getwd()
	c.Assert(err, IsNil)
	defer os.Chdir(origDir)

	c.Assert(syscall.Fchdir(fd), IsNil)
	cwd, err := os.Getwd()
	c.Assert(err, IsNil)
	c.Check(cwd, Equals, path)
}

func (s *realSystemSuite) TestSecureOpenPathRelativePath(c *C) {
	fd, err := osutil.OpenPath("relative/path")
	c.Check(fd, Equals, -1)
	c.Check(err, ErrorMatches, "path .* is not absolute")
}

func (s *realSystemSuite) TestSecureOpenPathUncleanPath(c *C) {
	base := c.MkDir()
	path := filepath.Join(base, "test")
	c.Assert(os.Mkdir(path, 0755), IsNil)

	fd, err := osutil.OpenPath(base + "//test")
	c.Check(fd, Equals, -1)
	c.Check(err, ErrorMatches, `cannot open path: cannot iterate over unclean path ".*//test"`)

	fd, err = osutil.OpenPath(base + "/./test")
	c.Check(fd, Equals, -1)
	c.Check(err, ErrorMatches, `cannot open path: cannot iterate over unclean path ".*/./test"`)

	fd, err = osutil.OpenPath(base + "/test/../test")
	c.Check(fd, Equals, -1)
	c.Check(err, ErrorMatches, `cannot open path: cannot iterate over unclean path ".*/test/../test"`)
}

func (s *realSystemSuite) TestSecureOpenPathFile(c *C) {
	path := filepath.Join(c.MkDir(), "file.txt")
	c.Assert(ioutil.WriteFile(path, []byte("hello"), 0644), IsNil)

	fd, err := osutil.OpenPath(path)
	c.Assert(err, IsNil)
	defer syscall.Close(fd)

	// Check that the file descriptor matches the file.
	var pathStat, fdStat syscall.Stat_t
	c.Assert(syscall.Stat(path, &pathStat), IsNil)
	c.Assert(syscall.Fstat(fd, &fdStat), IsNil)
	c.Check(pathStat, Equals, fdStat)
}

func (s *realSystemSuite) TestSecureOpenPathNotFound(c *C) {
	path := filepath.Join(c.MkDir(), "test")

	fd, err := osutil.OpenPath(path)
	c.Check(fd, Equals, -1)
	c.Check(err, ErrorMatches, "no such file or directory")
}

func (s *realSystemSuite) TestSecureOpenPathSymlink(c *C) {
	base := c.MkDir()
	dir := filepath.Join(base, "test")
	c.Assert(os.Mkdir(dir, 0755), IsNil)

	symlink := filepath.Join(base, "symlink")
	c.Assert(os.Symlink(dir, symlink), IsNil)

	fd, err := osutil.OpenPath(symlink)
	c.Check(fd, Equals, -1)
	c.Check(err, ErrorMatches, `".*" is a symbolic link`)
}

func (s *realSystemSuite) TestSecureOpenPathSymlinkedParent(c *C) {
	base := c.MkDir()
	dir := filepath.Join(base, "dir1")
	symlink := filepath.Join(base, "symlink")

	path := filepath.Join(dir, "dir2")
	symlinkedPath := filepath.Join(symlink, "dir2")

	c.Assert(os.Mkdir(dir, 0755), IsNil)
	c.Assert(os.Symlink(dir, symlink), IsNil)
	c.Assert(os.Mkdir(path, 0755), IsNil)

	fd, err := osutil.OpenPath(symlinkedPath)
	c.Check(fd, Equals, -1)
	c.Check(err, ErrorMatches, "not a directory")
}

// Check that we can actually create files.
// This doesn't test the chown logic as that requires root.
func (s *realSystemSuite) TestSecureMkfileAllForReal(c *C) {
	d := c.MkDir()

	// Create f1, which is a simple subdirectory, with a distinct mode and
	// check that it was applied. Note that default umask 022 is subtracted so
	// effective directory has different permissions.
	f1 := filepath.Join(d, "file")
	c.Assert(osutil.MkfileAll(f1, 0707, sys.FlagID, sys.FlagID, nil), IsNil)
	fi, err := os.Stat(f1)
	c.Assert(err, IsNil)
	c.Check(fi.Mode().IsRegular(), Equals, true)
	c.Check(fi.Mode().Perm(), Equals, os.FileMode(0705))

	// Create f2, which is a deeper subdirectory, with another distinct mode
	// and check that it was applied.
	f2 := filepath.Join(d, "subdir/subdir/file")
	c.Assert(osutil.MkfileAll(f2, 0750, sys.FlagID, sys.FlagID, nil), IsNil)
	fi, err = os.Stat(f2)
	c.Assert(err, IsNil)
	c.Check(fi.Mode().IsRegular(), Equals, true)
	c.Check(fi.Mode().Perm(), Equals, os.FileMode(0750))
}

// Check that we can actually create symlinks.
// This doesn't test the chown logic as that requires root.
func (s *realSystemSuite) TestSecureMksymlinkAllForReal(c *C) {
	d := c.MkDir()

	// Create symlink f1 that points to "oldname" and check that it
	// is correct. Note that symlink permissions are always set to 0777
	f1 := filepath.Join(d, "symlink")
	err := osutil.MksymlinkAll(f1, 0755, sys.FlagID, sys.FlagID, "oldname", nil)
	c.Assert(err, IsNil)
	fi, err := os.Lstat(f1)
	c.Assert(err, IsNil)
	c.Check(fi.Mode()&os.ModeSymlink, Equals, os.ModeSymlink)
	c.Check(fi.Mode().Perm(), Equals, os.FileMode(0777))

	target, err := os.Readlink(f1)
	c.Assert(err, IsNil)
	c.Check(target, Equals, "oldname")

	// Create an identical symlink to see that it doesn't fail.
	err = osutil.MksymlinkAll(f1, 0755, sys.FlagID, sys.FlagID, "oldname", nil)
	c.Assert(err, IsNil)

	// Create a different symlink and see that it fails now
	err = osutil.MksymlinkAll(f1, 0755, sys.FlagID, sys.FlagID, "other", nil)
	c.Assert(err, ErrorMatches, `cannot create symbolic link ".*/symlink": existing symbolic link in the way`)

	// Create an file and check that it clashes with a symlink we attempt to create.
	f2 := filepath.Join(d, "file")
	err = osutil.MkfileAll(f2, 0755, sys.FlagID, sys.FlagID, nil)
	c.Assert(err, IsNil)
	err = osutil.MksymlinkAll(f2, 0755, sys.FlagID, sys.FlagID, "oldname", nil)
	c.Assert(err, ErrorMatches, `cannot create symbolic link ".*/file": existing file in the way`)

	// Create an file and check that it clashes with a symlink we attempt to create.
	f3 := filepath.Join(d, "dir")
	err = osutil.MkdirAll(f3, 0755, sys.FlagID, sys.FlagID, nil)
	c.Assert(err, IsNil)
	err = osutil.MksymlinkAll(f3, 0755, sys.FlagID, sys.FlagID, "oldname", nil)
	c.Assert(err, ErrorMatches, `cannot create symbolic link ".*/dir": existing file in the way`)

	err = osutil.MksymlinkAll("/", 0755, sys.FlagID, sys.FlagID, "oldname", nil)
	c.Assert(err, ErrorMatches, `cannot create non-file path: "/"`)
}

// Check that we can actually create directories.
// This doesn't test the chown logic as that requires root.
func (s *realSystemSuite) TestSecureMkdirAllForReal(c *C) {
	d := c.MkDir()

	// Create d (which already exists) with mode 0777 (but c.MkDir() used 0700
	// internally and since we are not creating the directory we should not be
	// changing that.
	c.Assert(osutil.MkdirAll(d, 0777, sys.FlagID, sys.FlagID, nil), IsNil)
	fi, err := os.Stat(d)
	c.Assert(err, IsNil)
	c.Check(fi.IsDir(), Equals, true)
	c.Check(fi.Mode().Perm(), Equals, os.FileMode(0700))

	// Create d1, which is a simple subdirectory, with a distinct mode and
	// check that it was applied. Note that default umask 022 is subtracted so
	// effective directory has different permissions.
	d1 := filepath.Join(d, "subdir")
	c.Assert(osutil.MkdirAll(d1, 0707, sys.FlagID, sys.FlagID, nil), IsNil)
	fi, err = os.Stat(d1)
	c.Assert(err, IsNil)
	c.Check(fi.IsDir(), Equals, true)
	c.Check(fi.Mode().Perm(), Equals, os.FileMode(0705))

	// Create d2, which is a deeper subdirectory, with another distinct mode
	// and check that it was applied.
	d2 := filepath.Join(d, "subdir/subdir/subdir")
	c.Assert(osutil.MkdirAll(d2, 0750, sys.FlagID, sys.FlagID, nil), IsNil)
	fi, err = os.Stat(d2)
	c.Assert(err, IsNil)
	c.Check(fi.IsDir(), Equals, true)
	c.Check(fi.Mode().Perm(), Equals, os.FileMode(0750))
}

type secureBindMountSuite struct {
	testutil.BaseTest
	sys *testutil.SyscallRecorder
}

var _ = Suite(&secureBindMountSuite{})

func (s *secureBindMountSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.sys = &testutil.SyscallRecorder{}
	s.BaseTest.AddCleanup(osutil.MockSystemCalls(s.sys))
}

func (s *secureBindMountSuite) TearDownTest(c *C) {
	s.sys.CheckForStrayDescriptors(c)
	s.BaseTest.TearDownTest(c)
}

func (s *secureBindMountSuite) TestMount(c *C) {
	s.sys.InsertFstatResult(`fstat 5 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFstatResult(`fstat 6 <ptr>`, syscall.Stat_t{})
	err := osutil.BindMount("/source/dir", "/target/dir", syscall.MS_BIND)
	c.Assert(err, IsNil)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "source" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 4},
		{C: `openat 4 "dir" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 5},
		{C: `fstat 5 <ptr>`, R: syscall.Stat_t{}},
		{C: `close 4`}, // "/source"
		{C: `close 3`}, // "/"
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "target" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 4},
		{C: `openat 4 "dir" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 6},
		{C: `fstat 6 <ptr>`, R: syscall.Stat_t{}},
		{C: `close 4`}, // "/target"
		{C: `close 3`}, // "/"
		{C: `mount "/proc/self/fd/5" "/proc/self/fd/6" "" MS_BIND ""`},
		{C: `close 6`}, // "/target/dir"
		{C: `close 5`}, // "/source/dir"
	})
}

func (s *secureBindMountSuite) TestMountRecursive(c *C) {
	s.sys.InsertFstatResult(`fstat 5 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFstatResult(`fstat 6 <ptr>`, syscall.Stat_t{})
	err := osutil.BindMount("/source/dir", "/target/dir", syscall.MS_BIND|syscall.MS_REC)
	c.Assert(err, IsNil)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "source" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 4},
		{C: `openat 4 "dir" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 5},
		{C: `fstat 5 <ptr>`, R: syscall.Stat_t{}},
		{C: `close 4`}, // "/source"
		{C: `close 3`}, // "/"
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "target" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 4},
		{C: `openat 4 "dir" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 6},
		{C: `fstat 6 <ptr>`, R: syscall.Stat_t{}},
		{C: `close 4`}, // "/target"
		{C: `close 3`}, // "/"
		{C: `mount "/proc/self/fd/5" "/proc/self/fd/6" "" MS_BIND|MS_REC ""`},
		{C: `close 6`}, // "/target/dir"
		{C: `close 5`}, // "/source/dir"
	})
}

func (s *secureBindMountSuite) TestMountReadOnly(c *C) {
	s.sys.InsertFstatResult(`fstat 5 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFstatResult(`fstat 6 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFstatResult(`fstat 7 <ptr>`, syscall.Stat_t{})
	err := osutil.BindMount("/source/dir", "/target/dir", syscall.MS_BIND|syscall.MS_RDONLY)
	c.Assert(err, IsNil)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "source" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 4},
		{C: `openat 4 "dir" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 5},
		{C: `fstat 5 <ptr>`, R: syscall.Stat_t{}},
		{C: `close 4`}, // "/source"
		{C: `close 3`}, // "/"
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "target" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 4},
		{C: `openat 4 "dir" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 6},
		{C: `fstat 6 <ptr>`, R: syscall.Stat_t{}},
		{C: `close 4`}, // "/target"
		{C: `close 3`}, // "/"
		{C: `mount "/proc/self/fd/5" "/proc/self/fd/6" "" MS_BIND ""`},
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "target" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 4},
		{C: `openat 4 "dir" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 7},
		{C: `fstat 7 <ptr>`, R: syscall.Stat_t{}},
		{C: `close 4`}, // "/target"
		{C: `close 3`}, // "/"
		{C: `mount "none" "/proc/self/fd/7" "" MS_REMOUNT|MS_BIND|MS_RDONLY ""`},
		{C: `close 7`}, // "/target/dir"
		{C: `close 6`}, // "/target/dir"
		{C: `close 5`}, // "/source/dir"
	})
}

func (s *secureBindMountSuite) TestBindFlagRequired(c *C) {
	err := osutil.BindMount("/source/dir", "/target/dir", syscall.MS_REC)
	c.Assert(err, ErrorMatches, "cannot perform non-bind mount operation")
	c.Check(s.sys.RCalls(), HasLen, 0)
}

func (s *secureBindMountSuite) TestMountReadOnlyRecursive(c *C) {
	err := osutil.BindMount("/source/dir", "/target/dir", syscall.MS_BIND|syscall.MS_RDONLY|syscall.MS_REC)
	c.Assert(err, ErrorMatches, "cannot use MS_RDONLY and MS_REC together")
	c.Check(s.sys.RCalls(), HasLen, 0)
}

func (s *secureBindMountSuite) TestBindMountFails(c *C) {
	s.sys.InsertFstatResult(`fstat 5 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFstatResult(`fstat 6 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFault(`mount "/proc/self/fd/5" "/proc/self/fd/6" "" MS_BIND ""`, errTesting)
	err := osutil.BindMount("/source/dir", "/target/dir", syscall.MS_BIND|syscall.MS_RDONLY)
	c.Assert(err, ErrorMatches, "testing")
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "source" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 4},
		{C: `openat 4 "dir" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 5},
		{C: `fstat 5 <ptr>`, R: syscall.Stat_t{}},
		{C: `close 4`}, // "/source"
		{C: `close 3`}, // "/"
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "target" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 4},
		{C: `openat 4 "dir" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 6},
		{C: `fstat 6 <ptr>`, R: syscall.Stat_t{}},
		{C: `close 4`}, // "/target"
		{C: `close 3`}, // "/"
		{C: `mount "/proc/self/fd/5" "/proc/self/fd/6" "" MS_BIND ""`, E: errTesting},
		{C: `close 6`}, // "/target/dir"
		{C: `close 5`}, // "/source/dir"
	})
}

func (s *secureBindMountSuite) TestRemountReadOnlyFails(c *C) {
	s.sys.InsertFstatResult(`fstat 5 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFstatResult(`fstat 6 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFstatResult(`fstat 7 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFault(`mount "none" "/proc/self/fd/7" "" MS_REMOUNT|MS_BIND|MS_RDONLY ""`, errTesting)
	err := osutil.BindMount("/source/dir", "/target/dir", syscall.MS_BIND|syscall.MS_RDONLY)
	c.Assert(err, ErrorMatches, "testing")
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "source" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 4},
		{C: `openat 4 "dir" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 5},
		{C: `fstat 5 <ptr>`, R: syscall.Stat_t{}},
		{C: `close 4`}, // "/source"
		{C: `close 3`}, // "/"
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "target" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 4},
		{C: `openat 4 "dir" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 6},
		{C: `fstat 6 <ptr>`, R: syscall.Stat_t{}},
		{C: `close 4`}, // "/target"
		{C: `close 3`}, // "/"
		{C: `mount "/proc/self/fd/5" "/proc/self/fd/6" "" MS_BIND ""`},
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "target" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 4},
		{C: `openat 4 "dir" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 7},
		{C: `fstat 7 <ptr>`, R: syscall.Stat_t{}},
		{C: `close 4`}, // "/target"
		{C: `close 3`}, // "/"
		{C: `mount "none" "/proc/self/fd/7" "" MS_REMOUNT|MS_BIND|MS_RDONLY ""`, E: errTesting},
		{C: `unmount "/proc/self/fd/7" UMOUNT_NOFOLLOW|MNT_DETACH`},
		{C: `close 7`}, // "/target/dir"
		{C: `close 6`}, // "/target/dir"
		{C: `close 5`}, // "/source/dir"
	})
}
