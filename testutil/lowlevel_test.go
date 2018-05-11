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

package testutil_test

import (
	"github.com/snapcore/snapd/testutil"

	"fmt"
	"os"
	"syscall"

	. "gopkg.in/check.v1"
)

type lowLevelSuite struct {
	sys *testutil.SyscallRecorder
}

var _ = Suite(&lowLevelSuite{})

func (s *lowLevelSuite) SetUpTest(c *C) {
	s.sys = &testutil.SyscallRecorder{}
}

func (s *lowLevelSuite) TestFakeFileInfo(c *C) {
	ffi := testutil.FakeFileInfo("name", 0755)
	c.Assert(ffi.Name(), Equals, "name")
	c.Assert(ffi.Mode(), Equals, os.FileMode(0755))

	c.Assert(testutil.FileInfoFile.Mode().IsDir(), Equals, false)
	c.Assert(testutil.FileInfoFile.Mode().IsRegular(), Equals, true)
	c.Assert(testutil.FileInfoFile.IsDir(), Equals, false)

	c.Assert(testutil.FileInfoDir.Mode().IsDir(), Equals, true)
	c.Assert(testutil.FileInfoDir.Mode().IsRegular(), Equals, false)
	c.Assert(testutil.FileInfoDir.IsDir(), Equals, true)

	c.Assert(testutil.FileInfoSymlink.Mode().IsDir(), Equals, false)
	c.Assert(testutil.FileInfoSymlink.Mode().IsRegular(), Equals, false)
	c.Assert(testutil.FileInfoSymlink.IsDir(), Equals, false)
}

func (s *lowLevelSuite) TestOpenSuccess(c *C) {
	// By default system calls succeed and get recorded for inspection.
	fd, err := s.sys.Open("/some/path", syscall.O_NOFOLLOW|syscall.O_CLOEXEC|syscall.O_RDWR|syscall.O_CREAT|syscall.O_EXCL, 0)
	c.Assert(err, IsNil)
	c.Assert(fd, Equals, 3)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/some/path" O_NOFOLLOW|O_CLOEXEC|O_RDWR|O_CREAT|O_EXCL 0`, // -> 3
	})
	c.Assert(s.sys.RCalls(), DeepEquals, []testutil.CallResult{
		{`open "/some/path" O_NOFOLLOW|O_CLOEXEC|O_RDWR|O_CREAT|O_EXCL 0`, 3},
	})
}

func (s *lowLevelSuite) TestOpenFailure(c *C) {
	// Any call can be made to fail using InsertFault()
	s.sys.InsertFault(`open "/some/path" 0 0`, syscall.ENOENT)
	fd, err := s.sys.Open("/some/path", 0, 0)
	c.Assert(err, ErrorMatches, "no such file or directory")
	c.Assert(fd, Equals, -1)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/some/path" 0 0`, // -> ENOENT
	})
	c.Assert(s.sys.RCalls(), DeepEquals, []testutil.CallResult{
		{`open "/some/path" 0 0`, syscall.ENOENT},
	})
}

func (s *lowLevelSuite) TestOpenVariableFailure(c *C) {
	// The way a particular call fails may vary over time.
	// Subsequent errors are returned on subsequent calls.
	s.sys.InsertFault(`open "/some/path" O_RDWR 0`, syscall.ENOENT, syscall.EPERM)
	fd, err := s.sys.Open("/some/path", syscall.O_RDWR, 0)
	c.Assert(err, ErrorMatches, "no such file or directory")
	c.Assert(fd, Equals, -1)
	// 2nd attempt
	fd, err = s.sys.Open("/some/path", syscall.O_RDWR, 0)
	c.Assert(err, ErrorMatches, "operation not permitted")
	c.Assert(fd, Equals, -1)
	// 3rd attempt
	fd, err = s.sys.Open("/some/path", syscall.O_RDWR, 0)
	c.Assert(err, IsNil)
	c.Assert(fd, Equals, 3)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/some/path" O_RDWR 0`, // -> ENOENT
		`open "/some/path" O_RDWR 0`, // -> EPERM
		`open "/some/path" O_RDWR 0`, // -> 3
	})
	c.Assert(s.sys.RCalls(), DeepEquals, []testutil.CallResult{
		{`open "/some/path" O_RDWR 0`, syscall.ENOENT},
		{`open "/some/path" O_RDWR 0`, syscall.EPERM},
		{`open "/some/path" O_RDWR 0`, 3},
	})
}

func (s *lowLevelSuite) TestOpenCustomFailure(c *C) {
	// The way a particular call may also be arbitrarily programmed.
	n := 3
	s.sys.InsertFaultFunc(`open "/some/path" O_RDWR 0`, func() error {
		if n > 0 {
			err := fmt.Errorf("%d more", n)
			n--
			return err
		}
		return nil
	})
	_, err := s.sys.Open("/some/path", syscall.O_RDWR, 0)
	c.Assert(err, ErrorMatches, "3 more")
	_, err = s.sys.Open("/some/path", syscall.O_RDWR, 0)
	c.Assert(err, ErrorMatches, "2 more")
	_, err = s.sys.Open("/some/path", syscall.O_RDWR, 0)
	c.Assert(err, ErrorMatches, "1 more")
	fd, err := s.sys.Open("/some/path", syscall.O_RDWR, 0)
	c.Assert(err, IsNil)
	c.Assert(fd, Equals, 3)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/some/path" O_RDWR 0`, // -> 3 more
		`open "/some/path" O_RDWR 0`, // -> 2 more
		`open "/some/path" O_RDWR 0`, // -> 1 more
		`open "/some/path" O_RDWR 0`, // -> 3
	})
	c.Assert(s.sys.RCalls(), DeepEquals, []testutil.CallResult{
		{`open "/some/path" O_RDWR 0`, fmt.Errorf("3 more")},
		{`open "/some/path" O_RDWR 0`, fmt.Errorf("2 more")},
		{`open "/some/path" O_RDWR 0`, fmt.Errorf("1 more")},
		{`open "/some/path" O_RDWR 0`, 3},
	})
}

func (s *lowLevelSuite) TestUnclosedFile(c *C) {
	// Open file descriptors can be detected in suite teardown using either
	// StrayDescriptorError or CheckForStrayDescriptors.
	fd, err := s.sys.Open("/some/path", syscall.O_RDWR, 0)
	c.Assert(err, IsNil)
	c.Assert(fd, Equals, 3)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/some/path" O_RDWR 0`, // -> 3
	})
	c.Assert(s.sys.RCalls(), DeepEquals, []testutil.CallResult{
		{`open "/some/path" O_RDWR 0`, 3},
	})
	c.Assert(s.sys.StrayDescriptorsError(), ErrorMatches,
		`unclosed file descriptor 3 \(open "/some/path" O_RDWR 0\)`)
}

func (s *lowLevelSuite) TestUnopenedFile(c *C) {
	// Closing unopened file descriptors is an error.
	err := s.sys.Close(7)
	c.Assert(err, ErrorMatches, "attempting to close a closed file descriptor 7")
	c.Assert(s.sys.Calls(), DeepEquals, []string{`close 7`})
	c.Assert(s.sys.RCalls(), DeepEquals, []testutil.CallResult{
		{`close 7`, fmt.Errorf("attempting to close a closed file descriptor 7")},
	})
}

func (s *lowLevelSuite) TestCloseSuccess(c *C) {
	// Closing file descriptors handles the bookkeeping.
	fd, err := s.sys.Open("/some/path", syscall.O_RDWR, 0)
	c.Assert(err, IsNil)
	err = s.sys.Close(fd)
	c.Assert(err, IsNil)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/some/path" O_RDWR 0`, // -> 3
		`close 3`,
	})
	c.Assert(s.sys.RCalls(), DeepEquals, []testutil.CallResult{
		{`open "/some/path" O_RDWR 0`, 3},
		{`close 3`, nil},
	})
	c.Assert(s.sys.StrayDescriptorsError(), IsNil)
}

func (s *lowLevelSuite) TestCloseFailure(c *C) {
	// Close can be made to fail just like any other function.
	s.sys.InsertFault(`close 3`, syscall.ENOSYS)
	err := s.sys.Close(3)
	c.Assert(err, ErrorMatches, "function not implemented")
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`close 3`,
	})
	c.Assert(s.sys.RCalls(), DeepEquals, []testutil.CallResult{
		{`close 3`, syscall.ENOSYS},
	})
}

func (s *lowLevelSuite) TestOpenatSuccess(c *C) {
	dirfd, err := s.sys.Open("/", syscall.O_DIRECTORY, 0)
	c.Assert(err, IsNil)
	fd, err := s.sys.Openat(dirfd, "foo", syscall.O_DIRECTORY, 0)
	c.Assert(err, IsNil)
	c.Assert(s.sys.Close(fd), IsNil)
	c.Assert(s.sys.Close(dirfd), IsNil)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_DIRECTORY 0`,       // -> 3
		`openat 3 "foo" O_DIRECTORY 0`, // -> 4
		`close 4`,
		`close 3`,
	})
	c.Assert(s.sys.RCalls(), DeepEquals, []testutil.CallResult{
		{`open "/" O_DIRECTORY 0`, 3},
		{`openat 3 "foo" O_DIRECTORY 0`, 4},
		{`close 4`, nil},
		{`close 3`, nil},
	})
}

func (s *lowLevelSuite) TestOpenatFailure(c *C) {
	dirfd, err := s.sys.Open("/", syscall.O_DIRECTORY, 0)
	c.Assert(err, IsNil)
	s.sys.InsertFault(`openat 3 "foo" O_DIRECTORY 0`, syscall.ENOENT)
	fd, err := s.sys.Openat(dirfd, "foo", syscall.O_DIRECTORY, 0)
	c.Assert(err, ErrorMatches, "no such file or directory")
	c.Assert(fd, Equals, -1)
	c.Assert(s.sys.Close(dirfd), IsNil)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_DIRECTORY 0`,       // -> 3
		`openat 3 "foo" O_DIRECTORY 0`, // -> ENOENT
		`close 3`,
	})
	c.Assert(s.sys.RCalls(), DeepEquals, []testutil.CallResult{
		{`open "/" O_DIRECTORY 0`, 3},
		{`openat 3 "foo" O_DIRECTORY 0`, syscall.ENOENT},
		{`close 3`, nil},
	})
}

func (s *lowLevelSuite) TestOpenatBadFd(c *C) {
	fd, err := s.sys.Openat(3, "foo", syscall.O_DIRECTORY, 0)
	c.Assert(err, ErrorMatches, "attempting to openat with an invalid file descriptor 3")
	c.Assert(fd, Equals, -1)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`openat 3 "foo" O_DIRECTORY 0`, // -> error
	})
	c.Assert(s.sys.RCalls(), DeepEquals, []testutil.CallResult{
		{`openat 3 "foo" O_DIRECTORY 0`, fmt.Errorf("attempting to openat with an invalid file descriptor 3")},
	})
}

func (s *lowLevelSuite) TestFchownSuccess(c *C) {
	fd, err := s.sys.Open("/", syscall.O_DIRECTORY, 0)
	c.Assert(err, IsNil)
	err = s.sys.Fchown(fd, 0, 0)
	c.Assert(err, IsNil)
	c.Assert(s.sys.Close(fd), IsNil)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_DIRECTORY 0`, // -> 3
		`fchown 3 0 0`,
		`close 3`,
	})
	c.Assert(s.sys.RCalls(), DeepEquals, []testutil.CallResult{
		{`open "/" O_DIRECTORY 0`, 3},
		{`fchown 3 0 0`, nil},
		{`close 3`, nil},
	})
}

func (s *lowLevelSuite) TestFchownFailure(c *C) {
	fd, err := s.sys.Open("/", syscall.O_DIRECTORY, 0)
	c.Assert(err, IsNil)
	s.sys.InsertFault(`fchown 3 0 0`, syscall.EPERM)
	err = s.sys.Fchown(fd, 0, 0)
	c.Assert(err, ErrorMatches, "operation not permitted")
	c.Assert(s.sys.Close(fd), IsNil)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_DIRECTORY 0`, // -> 3
		`fchown 3 0 0`,           // -> EPERM
		`close 3`,
	})
	c.Assert(s.sys.RCalls(), DeepEquals, []testutil.CallResult{
		{`open "/" O_DIRECTORY 0`, 3},
		{`fchown 3 0 0`, syscall.EPERM},
		{`close 3`, nil},
	})
}

func (s *lowLevelSuite) TestFchownBadFd(c *C) {
	err := s.sys.Fchown(3, 0, 0)
	c.Assert(err, ErrorMatches, "attempting to fchown an invalid file descriptor 3")
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`fchown 3 0 0`,
	})
	c.Assert(s.sys.RCalls(), DeepEquals, []testutil.CallResult{
		{`fchown 3 0 0`, fmt.Errorf("attempting to fchown an invalid file descriptor 3")},
	})
}

func (s *lowLevelSuite) TestMkdiratSuccess(c *C) {
	fd, err := s.sys.Open("/", syscall.O_DIRECTORY, 0)
	c.Assert(err, IsNil)
	err = s.sys.Mkdirat(fd, "foo", 0755)
	c.Assert(err, IsNil)
	c.Assert(s.sys.Close(fd), IsNil)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_DIRECTORY 0`, // -> 3
		`mkdirat 3 "foo" 0755`,
		`close 3`,
	})
	c.Assert(s.sys.RCalls(), DeepEquals, []testutil.CallResult{
		{`open "/" O_DIRECTORY 0`, 3},
		{`mkdirat 3 "foo" 0755`, nil},
		{`close 3`, nil},
	})
}

func (s *lowLevelSuite) TestMkdiratFailure(c *C) {
	fd, err := s.sys.Open("/", syscall.O_DIRECTORY, 0)
	c.Assert(err, IsNil)
	s.sys.InsertFault(`mkdirat 3 "foo" 0755`, syscall.EPERM)
	err = s.sys.Mkdirat(fd, "foo", 0755)
	c.Assert(err, ErrorMatches, "operation not permitted")
	c.Assert(s.sys.Close(fd), IsNil)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_DIRECTORY 0`, // -> 3
		`mkdirat 3 "foo" 0755`,   // -> EPERM
		`close 3`,
	})
	c.Assert(s.sys.RCalls(), DeepEquals, []testutil.CallResult{
		{`open "/" O_DIRECTORY 0`, 3},
		{`mkdirat 3 "foo" 0755`, syscall.EPERM},
		{`close 3`, nil},
	})
}

func (s *lowLevelSuite) TestMkdiratBadFd(c *C) {
	err := s.sys.Mkdirat(3, "foo", 0755)
	c.Assert(err, ErrorMatches, "attempting to mkdirat with an invalid file descriptor 3")
	c.Assert(s.sys.Calls(), DeepEquals, []string{`mkdirat 3 "foo" 0755`})
	c.Assert(s.sys.RCalls(), DeepEquals, []testutil.CallResult{
		{`mkdirat 3 "foo" 0755`, fmt.Errorf("attempting to mkdirat with an invalid file descriptor 3")},
	})
}

func (s *lowLevelSuite) TestMountSuccess(c *C) {
	err := s.sys.Mount("source", "target", "fstype", syscall.MS_BIND|syscall.MS_REC|syscall.MS_RDONLY, "")
	c.Assert(err, IsNil)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`mount "source" "target" "fstype" MS_BIND|MS_REC|MS_RDONLY ""`,
	})
	c.Assert(s.sys.RCalls(), DeepEquals, []testutil.CallResult{
		{`mount "source" "target" "fstype" MS_BIND|MS_REC|MS_RDONLY ""`, nil},
	})
}

func (s *lowLevelSuite) TestMountFailure(c *C) {
	s.sys.InsertFault(`mount "source" "target" "fstype" 0 ""`, syscall.EPERM)
	err := s.sys.Mount("source", "target", "fstype", 0, "")
	c.Assert(err, ErrorMatches, "operation not permitted")
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`mount "source" "target" "fstype" 0 ""`,
	})
	c.Assert(s.sys.RCalls(), DeepEquals, []testutil.CallResult{
		{`mount "source" "target" "fstype" 0 ""`, syscall.EPERM},
	})
}

func (s *lowLevelSuite) TestUnmountSuccess(c *C) {
	err := s.sys.Unmount("target", testutil.UmountNoFollow|syscall.MNT_DETACH)
	c.Assert(err, IsNil)
	c.Assert(s.sys.Calls(), DeepEquals, []string{`unmount "target" UMOUNT_NOFOLLOW|MNT_DETACH`})
	c.Assert(s.sys.RCalls(), DeepEquals, []testutil.CallResult{
		{`unmount "target" UMOUNT_NOFOLLOW|MNT_DETACH`, nil},
	})
}

func (s *lowLevelSuite) TestUnmountFailure(c *C) {
	s.sys.InsertFault(`unmount "target" 0`, syscall.EPERM)
	err := s.sys.Unmount("target", 0)
	c.Assert(err, ErrorMatches, "operation not permitted")
	c.Assert(s.sys.Calls(), DeepEquals, []string{`unmount "target" 0`})
	c.Assert(s.sys.RCalls(), DeepEquals, []testutil.CallResult{
		{`unmount "target" 0`, syscall.EPERM},
	})
}

func (s *lowLevelSuite) TestOsLstat(c *C) {
	// When a function returns some data it must be fed either an error or a result.
	c.Assert(func() { s.sys.OsLstat("/foo") }, PanicMatches,
		`one of InsertOsLstatResult\(\) or InsertFault\(\) for lstat "/foo" must be used`)
}

func (s *lowLevelSuite) TestOsLstatSuccess(c *C) {
	// The fed data is returned in absence of errors.
	s.sys.InsertOsLstatResult(`lstat "/foo"`, testutil.FileInfoFile)
	fi, err := s.sys.OsLstat("/foo")
	c.Assert(err, IsNil)
	c.Assert(fi, DeepEquals, testutil.FileInfoFile)
	c.Assert(s.sys.Calls(), DeepEquals, []string{`lstat "/foo"`})
	c.Assert(s.sys.RCalls(), DeepEquals, []testutil.CallResult{
		{`lstat "/foo"`, testutil.FileInfoFile},
	})
}

func (s *lowLevelSuite) TestOsLstatFailure(c *C) {
	// Errors take priority over data.
	s.sys.InsertOsLstatResult(`lstat "/foo"`, testutil.FileInfoFile)
	s.sys.InsertFault(`lstat "/foo"`, syscall.ENOENT)
	fi, err := s.sys.OsLstat("/foo")
	c.Assert(err, ErrorMatches, "no such file or directory")
	c.Assert(fi, IsNil)
	c.Assert(s.sys.Calls(), DeepEquals, []string{`lstat "/foo"`})
	c.Assert(s.sys.RCalls(), DeepEquals, []testutil.CallResult{
		{`lstat "/foo"`, syscall.ENOENT},
	})
}

func (s *lowLevelSuite) TestSysLstat(c *C) {
	// When a function returns some data it must be fed either an error or a result.
	var buf syscall.Stat_t
	c.Assert(func() { s.sys.SysLstat("/foo", &buf) }, PanicMatches,
		`one of InsertSysLstatResult\(\) or InsertFault\(\) for lstat "/foo" <ptr> must be used`)
}

func (s *lowLevelSuite) TestSysLstatSuccess(c *C) {
	// The fed data is returned in absence of errors.
	var buf syscall.Stat_t
	s.sys.InsertSysLstatResult(`lstat "/foo" <ptr>`, syscall.Stat_t{Uid: 123})
	err := s.sys.SysLstat("/foo", &buf)
	c.Assert(err, IsNil)
	c.Assert(buf, DeepEquals, syscall.Stat_t{Uid: 123})
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`lstat "/foo" <ptr>`,
	})
	c.Assert(s.sys.RCalls(), DeepEquals, []testutil.CallResult{
		{`lstat "/foo" <ptr>`, syscall.Stat_t{Uid: 123}},
	})
}

func (s *lowLevelSuite) TestSysLstatFailure(c *C) {
	// Errors take priority over data.
	var buf syscall.Stat_t
	s.sys.InsertSysLstatResult(`lstat "/foo" <ptr>`, syscall.Stat_t{Uid: 123})
	s.sys.InsertFault(`lstat "/foo" <ptr>`, syscall.ENOENT)
	err := s.sys.SysLstat("/foo", &buf)
	c.Assert(err, ErrorMatches, "no such file or directory")
	c.Assert(buf, DeepEquals, syscall.Stat_t{})
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`lstat "/foo" <ptr>`, // -> ENOENT
	})
	c.Assert(s.sys.RCalls(), DeepEquals, []testutil.CallResult{
		{`lstat "/foo" <ptr>`, syscall.ENOENT},
	})
}

func (s *lowLevelSuite) TestFstat(c *C) {
	fd, err := s.sys.Open("/foo", syscall.O_RDONLY, 0)
	c.Assert(err, IsNil)
	var buf syscall.Stat_t
	c.Assert(func() { s.sys.Fstat(fd, &buf) }, PanicMatches,
		`one of InsertFstatResult\(\) or InsertFault\(\) for fstat 3 <ptr> must be used`)
}

func (s *lowLevelSuite) TestFstatBadFd(c *C) {
	var buf syscall.Stat_t
	err := s.sys.Fstat(3, &buf)
	c.Assert(err, ErrorMatches, "attempting to fstat with an invalid file descriptor 3")
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`fstat 3 <ptr>`,
	})
	c.Assert(s.sys.RCalls(), DeepEquals, []testutil.CallResult{
		{`fstat 3 <ptr>`, fmt.Errorf("attempting to fstat with an invalid file descriptor 3")},
	})
}

func (s *lowLevelSuite) TestFstatSuccess(c *C) {
	s.sys.InsertFstatResult(`fstat 3 <ptr>`, syscall.Stat_t{Dev: 0xC0FE})
	fd, err := s.sys.Open("/foo", syscall.O_RDONLY, 0)
	c.Assert(err, IsNil)
	var buf syscall.Stat_t
	err = s.sys.Fstat(fd, &buf)
	c.Assert(err, IsNil)
	c.Assert(buf, Equals, syscall.Stat_t{Dev: 0xC0FE})
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/foo" 0 0`, // -> 3
		`fstat 3 <ptr>`,
	})
	c.Assert(s.sys.RCalls(), DeepEquals, []testutil.CallResult{
		{`open "/foo" 0 0`, 3},
		{`fstat 3 <ptr>`, syscall.Stat_t{Dev: 0xC0FE}},
	})
}

func (s *lowLevelSuite) TestFstatFailure(c *C) {
	s.sys.InsertFault(`fstat 3 <ptr>`, syscall.EPERM)
	fd, err := s.sys.Open("/foo", syscall.O_RDONLY, 0)
	c.Assert(err, IsNil)
	var buf syscall.Stat_t
	err = s.sys.Fstat(fd, &buf)
	c.Assert(err, ErrorMatches, "operation not permitted")
	c.Assert(buf, Equals, syscall.Stat_t{})
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/foo" 0 0`, // -> 3
		`fstat 3 <ptr>`,   // -> EPERM
	})
	c.Assert(s.sys.RCalls(), DeepEquals, []testutil.CallResult{
		{`open "/foo" 0 0`, 3},
		{`fstat 3 <ptr>`, syscall.EPERM},
	})
}

func (s *lowLevelSuite) TestReadDir(c *C) {
	c.Assert(func() { s.sys.ReadDir("/foo") }, PanicMatches,
		`one of InsertReadDirResult\(\) or InsertFault\(\) for readdir "/foo" must be used`)
}

func (s *lowLevelSuite) TestReadDirSuccess(c *C) {
	files := []os.FileInfo{
		testutil.FakeFileInfo("file", 0644),
		testutil.FakeFileInfo("dir", 0755|os.ModeDir),
	}
	s.sys.InsertReadDirResult(`readdir "/foo"`, files)
	files, err := s.sys.ReadDir("/foo")
	c.Assert(err, IsNil)
	c.Assert(files, HasLen, 2)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`readdir "/foo"`,
	})
	c.Assert(s.sys.RCalls(), DeepEquals, []testutil.CallResult{
		{`readdir "/foo"`, files},
	})
}

func (s *lowLevelSuite) TestReadDirFailure(c *C) {
	s.sys.InsertFault(`readdir "/foo"`, syscall.ENOENT)
	files, err := s.sys.ReadDir("/foo")
	c.Assert(err, ErrorMatches, "no such file or directory")
	c.Assert(files, IsNil)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`readdir "/foo"`, // -> ENOENT
	})
	c.Assert(s.sys.RCalls(), DeepEquals, []testutil.CallResult{
		{`readdir "/foo"`, syscall.ENOENT},
	})
}

func (s *lowLevelSuite) TestSymlinkSuccess(c *C) {
	err := s.sys.Symlink("oldname", "newname")
	c.Assert(err, IsNil)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`symlink "newname" -> "oldname"`,
	})
	c.Assert(s.sys.RCalls(), DeepEquals, []testutil.CallResult{
		{`symlink "newname" -> "oldname"`, nil},
	})
}

func (s *lowLevelSuite) TestSymlinkFailure(c *C) {
	s.sys.InsertFault(`symlink "newname" -> "oldname"`, syscall.EPERM)
	err := s.sys.Symlink("oldname", "newname")
	c.Assert(err, ErrorMatches, "operation not permitted")
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`symlink "newname" -> "oldname"`, // -> EPERM
	})
	c.Assert(s.sys.RCalls(), DeepEquals, []testutil.CallResult{
		{`symlink "newname" -> "oldname"`, syscall.EPERM},
	})
}

func (s *lowLevelSuite) TestRemoveSuccess(c *C) {
	err := s.sys.Remove("file")
	c.Assert(err, IsNil)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`remove "file"`,
	})
	c.Assert(s.sys.RCalls(), DeepEquals, []testutil.CallResult{
		{`remove "file"`, nil},
	})
}

func (s *lowLevelSuite) TestRemoveFailure(c *C) {
	s.sys.InsertFault(`remove "file"`, syscall.EPERM)
	err := s.sys.Remove("file")
	c.Assert(err, ErrorMatches, "operation not permitted")
	c.Assert(s.sys.Calls(), DeepEquals, []string{`remove "file"`})
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`remove "file"`, // -> EPERM
	})
	c.Assert(s.sys.RCalls(), DeepEquals, []testutil.CallResult{
		{`remove "file"`, syscall.EPERM},
	})
}

func (s *lowLevelSuite) TestSymlinkatBadFd(c *C) {
	err := s.sys.Symlinkat("/old", 3, "new")
	c.Assert(err, ErrorMatches, "attempting to symlinkat with an invalid file descriptor 3")
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`symlinkat "/old" 3 "new"`,
	})
	c.Assert(s.sys.RCalls(), DeepEquals, []testutil.CallResult{
		{`symlinkat "/old" 3 "new"`, fmt.Errorf("attempting to symlinkat with an invalid file descriptor 3")},
	})
}

func (s *lowLevelSuite) TestSymlinkatSuccess(c *C) {
	fd, err := s.sys.Open("/foo", syscall.O_RDONLY, 0)
	c.Assert(err, IsNil)
	err = s.sys.Symlinkat("/old", fd, "new")
	c.Assert(err, IsNil)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/foo" 0 0`,
		`symlinkat "/old" 3 "new"`,
	})
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/foo" 0 0`, // -> 3
		`symlinkat "/old" 3 "new"`,
	})
	c.Assert(s.sys.RCalls(), DeepEquals, []testutil.CallResult{
		{`open "/foo" 0 0`, 3},
		{`symlinkat "/old" 3 "new"`, nil},
	})
}

func (s *lowLevelSuite) TestSymlinkatFailure(c *C) {
	s.sys.InsertFault(`symlinkat "/old" 3 "new"`, syscall.EPERM)
	fd, err := s.sys.Open("/foo", syscall.O_RDONLY, 0)
	c.Assert(err, IsNil)
	err = s.sys.Symlinkat("/old", fd, "new")
	c.Assert(err, ErrorMatches, "operation not permitted")
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/foo" 0 0`, // -> 3
		`symlinkat "/old" 3 "new"`,
	})
	c.Assert(s.sys.RCalls(), DeepEquals, []testutil.CallResult{
		{`open "/foo" 0 0`, 3},
		{`symlinkat "/old" 3 "new"`, syscall.EPERM},
	})
}

func (s *lowLevelSuite) TestReadlinkat(c *C) {
	fd, err := s.sys.Open("/foo", syscall.O_RDONLY, 0)
	c.Assert(err, IsNil)
	buf := make([]byte, 10)
	c.Assert(func() { s.sys.Readlinkat(fd, "new", buf) }, PanicMatches,
		`one of InsertReadlinkatResult\(\) or InsertFault\(\) for readlinkat 3 "new" <ptr> must be used`)
}

func (s *lowLevelSuite) TestReadlinkatBadFd(c *C) {
	buf := make([]byte, 10)
	n, err := s.sys.Readlinkat(3, "new", buf)
	c.Assert(err, ErrorMatches, "attempting to readlinkat with an invalid file descriptor 3")
	c.Assert(n, Equals, 0)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`readlinkat 3 "new" <ptr>`,
	})
	c.Assert(s.sys.RCalls(), DeepEquals, []testutil.CallResult{
		{`readlinkat 3 "new" <ptr>`, fmt.Errorf("attempting to readlinkat with an invalid file descriptor 3")},
	})
}

func (s *lowLevelSuite) TestReadlinkatSuccess(c *C) {
	s.sys.InsertReadlinkatResult(`readlinkat 3 "new" <ptr>`, "/old")
	fd, err := s.sys.Open("/foo", syscall.O_RDONLY, 0)
	c.Assert(err, IsNil)

	// Buffer has enough room
	buf := make([]byte, 10)
	n, err := s.sys.Readlinkat(fd, "new", buf)
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 4)
	c.Assert(buf, DeepEquals, []byte{'/', 'o', 'l', 'd', 0, 0, 0, 0, 0, 0})

	// Buffer is too short
	buf = make([]byte, 2)
	n, err = s.sys.Readlinkat(fd, "new", buf)
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 2)
	c.Assert(buf, DeepEquals, []byte{'/', 'o'})
}

func (s *lowLevelSuite) TestReadlinkatFailure(c *C) {
	s.sys.InsertFault(`readlinkat 3 "new" <ptr>`, syscall.EPERM)
	fd, err := s.sys.Open("/foo", syscall.O_RDONLY, 0)
	c.Assert(err, IsNil)

	buf := make([]byte, 10)
	n, err := s.sys.Readlinkat(fd, "new", buf)
	c.Assert(err, ErrorMatches, "operation not permitted")
	c.Assert(n, Equals, 0)
	c.Assert(buf, DeepEquals, []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0})
}
