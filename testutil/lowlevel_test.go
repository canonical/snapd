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
}

func (s *lowLevelSuite) TestOpenFailure(c *C) {
	// Any call can be made to fail using InsertFault()
	s.sys.InsertFault(`open "/some/path" 0 0`, syscall.ENOENT)
	fd, err := s.sys.Open("/some/path", 0, 0)
	c.Assert(err, ErrorMatches, "no such file or directory")
	c.Assert(fd, Equals, -1)
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
	c.Assert(s.sys.StrayDescriptorsError(), ErrorMatches,
		`unclosed file descriptor 3 \(open "/some/path" O_RDWR 0\)`)
}

func (s *lowLevelSuite) TestUnopenedFile(c *C) {
	// Closing unopened file descriptors is an error.
	err := s.sys.Close(7)
	c.Assert(err, ErrorMatches, "attempting to close a closed file descriptor 7")
	c.Assert(s.sys.Calls(), DeepEquals, []string{`close 7`})
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
}

func (s *lowLevelSuite) TestOpenatBadFd(c *C) {
	fd, err := s.sys.Openat(3, "foo", syscall.O_DIRECTORY, 0)
	c.Assert(err, ErrorMatches, "attempting to openat with an invalid file descriptor 3")
	c.Assert(fd, Equals, -1)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`openat 3 "foo" O_DIRECTORY 0`, // -> error
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
}

func (s *lowLevelSuite) TestFchownBadFd(c *C) {
	err := s.sys.Fchown(3, 0, 0)
	c.Assert(err, ErrorMatches, "attempting to fchown an invalid file descriptor 3")
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`fchown 3 0 0`,
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
}

func (s *lowLevelSuite) TestMkdiratBadFd(c *C) {
	err := s.sys.Mkdirat(3, "foo", 0755)
	c.Assert(err, ErrorMatches, "attempting to mkdirat with an invalid file descriptor 3")
	c.Assert(s.sys.Calls(), DeepEquals, []string{`mkdirat 3 "foo" 0755`})
}

func (s *lowLevelSuite) TestMountSuccess(c *C) {
	err := s.sys.Mount("source", "target", "fstype", syscall.MS_BIND|syscall.MS_REC|syscall.MS_RDONLY, "")
	c.Assert(err, IsNil)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`mount "source" "target" "fstype" MS_BIND|MS_REC|MS_RDONLY ""`,
	})
}

func (s *lowLevelSuite) TestMountFailure(c *C) {
	s.sys.InsertFault(`mount "source" "target" "fstype" 0 ""`, syscall.EPERM)
	err := s.sys.Mount("source", "target", "fstype", 0, "")
	c.Assert(err, ErrorMatches, "operation not permitted")
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`mount "source" "target" "fstype" 0 ""`,
	})
}

func (s *lowLevelSuite) TestUnmountSuccess(c *C) {
	err := s.sys.Unmount("target", testutil.UMOUNT_NOFOLLOW|syscall.MNT_DETACH)
	c.Assert(err, IsNil)
	c.Assert(s.sys.Calls(), DeepEquals, []string{`unmount "target" UMOUNT_NOFOLLOW|MNT_DETACH`})
}

func (s *lowLevelSuite) TestUnmountFailure(c *C) {
	s.sys.InsertFault(`unmount "target" 0`, syscall.EPERM)
	err := s.sys.Unmount("target", 0)
	c.Assert(err, ErrorMatches, "operation not permitted")
	c.Assert(s.sys.Calls(), DeepEquals, []string{`unmount "target" 0`})
}

func (s *lowLevelSuite) TestLstat(c *C) {
	// When a function returns some data it must be fed either an error or a result.
	c.Assert(func() { s.sys.Lstat("/foo") }, PanicMatches,
		`one of InsertLstatResult\(\) or InsertFault\(\) for lstat "/foo" must be used`)
}

func (s *lowLevelSuite) TestLstatSuccess(c *C) {
	// The fed data is returned in absence of errors.
	s.sys.InsertLstatResult(`lstat "/foo"`, testutil.FileInfoFile)
	fi, err := s.sys.Lstat("/foo")
	c.Assert(err, IsNil)
	c.Assert(fi, DeepEquals, testutil.FileInfoFile)
}

func (s *lowLevelSuite) TestLstatFailure(c *C) {
	// Errors take priority over data.
	s.sys.InsertLstatResult(`lstat "/foo"`, testutil.FileInfoFile)
	s.sys.InsertFault(`lstat "/foo"`, syscall.ENOENT)
	fi, err := s.sys.Lstat("/foo")
	c.Assert(err, ErrorMatches, "no such file or directory")
	c.Assert(fi, IsNil)
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
	c.Assert(s.sys.Calls(), DeepEquals, []string{`fstat 3 <ptr>`})
}

func (s *lowLevelSuite) TestFstatSuccess(c *C) {
	s.sys.InsertFstatResult(`fstat 3 <ptr>`, syscall.Stat_t{Dev: 0xC0FE})
	fd, err := s.sys.Open("/foo", syscall.O_RDONLY, 0)
	c.Assert(err, IsNil)
	var buf syscall.Stat_t
	err = s.sys.Fstat(fd, &buf)
	c.Assert(err, IsNil)
	c.Assert(buf, Equals, syscall.Stat_t{Dev: 0xC0FE})
}

func (s *lowLevelSuite) TestFstatFailure(c *C) {
	s.sys.InsertFault(`fstat 3 <ptr>`, syscall.EPERM)
	fd, err := s.sys.Open("/foo", syscall.O_RDONLY, 0)
	c.Assert(err, IsNil)
	var buf syscall.Stat_t
	err = s.sys.Fstat(fd, &buf)
	c.Assert(err, ErrorMatches, "operation not permitted")
	c.Assert(buf, Equals, syscall.Stat_t{})
}

func (s *lowLevelSuite) TestReadDir(c *C) {
	c.Assert(func() { s.sys.ReadDir("/foo") }, PanicMatches,
		`one of InsertReadDirResult\(\) or InsertFault\(\) for readdir "/foo" must be used`)
}

func (s *lowLevelSuite) TestReadDirSuccess(c *C) {
	s.sys.InsertReadDirResult(`readdir "/foo"`, []os.FileInfo{
		testutil.FakeFileInfo("file", 0644),
		testutil.FakeFileInfo("dir", 0755|os.ModeDir),
	})
	files, err := s.sys.ReadDir("/foo")
	c.Assert(err, IsNil)
	c.Assert(files, HasLen, 2)
}

func (s *lowLevelSuite) TestReadDirFailure(c *C) {
	s.sys.InsertFault(`readdir "/foo"`, syscall.ENOENT)
	files, err := s.sys.ReadDir("/foo")
	c.Assert(err, ErrorMatches, "no such file or directory")
	c.Assert(files, IsNil)
}

func (s *lowLevelSuite) TestSymlinkSuccess(c *C) {
	err := s.sys.Symlink("oldname", "newname")
	c.Assert(err, IsNil)
	c.Assert(s.sys.Calls(), DeepEquals, []string{`symlink "newname" -> "oldname"`})
}

func (s *lowLevelSuite) TestSymlinkFailure(c *C) {
	s.sys.InsertFault(`symlink "newname" -> "oldname"`, syscall.EPERM)
	err := s.sys.Symlink("oldname", "newname")
	c.Assert(err, ErrorMatches, "operation not permitted")
	c.Assert(s.sys.Calls(), DeepEquals, []string{`symlink "newname" -> "oldname"`})
}

func (s *lowLevelSuite) TestRemoveSuccess(c *C) {
	err := s.sys.Remove("file")
	c.Assert(err, IsNil)
	c.Assert(s.sys.Calls(), DeepEquals, []string{`remove "file"`})
}

func (s *lowLevelSuite) TestRemoveFailure(c *C) {
	s.sys.InsertFault(`remove "file"`, syscall.EPERM)
	err := s.sys.Remove("file")
	c.Assert(err, ErrorMatches, "operation not permitted")
	c.Assert(s.sys.Calls(), DeepEquals, []string{`remove "file"`})
}
