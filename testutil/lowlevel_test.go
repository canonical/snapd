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
	"fmt"
	"io/fs"
	"os"
	"syscall"

	"gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/testutil"
)

type lowLevelSuite struct {
	sys *testutil.SyscallRecorder
}

var _ = check.Suite(&lowLevelSuite{})

func (s *lowLevelSuite) SetUpTest(c *check.C) {
	s.sys = &testutil.SyscallRecorder{}
}

func (s *lowLevelSuite) TestFakeFileInfo(c *check.C) {
	ffi := testutil.FakeDirEntry("name", 0755)
	c.Assert(ffi.Name(), check.Equals, "name")
	fi := mylog.Check2(ffi.Info())
	c.Assert(err, check.IsNil)
	c.Assert(fi.Mode().Perm(), check.Equals, os.FileMode(0755))

	c.Assert(testutil.FileInfoFile.Mode().IsDir(), check.Equals, false)
	c.Assert(testutil.FileInfoFile.Mode().IsRegular(), check.Equals, true)
	c.Assert(testutil.FileInfoFile.IsDir(), check.Equals, false)

	c.Assert(testutil.FileInfoDir.Mode().IsDir(), check.Equals, true)
	c.Assert(testutil.FileInfoDir.Mode().IsRegular(), check.Equals, false)
	c.Assert(testutil.FileInfoDir.IsDir(), check.Equals, true)

	c.Assert(testutil.FileInfoSymlink.Mode().IsDir(), check.Equals, false)
	c.Assert(testutil.FileInfoSymlink.Mode().IsRegular(), check.Equals, false)
	c.Assert(testutil.FileInfoSymlink.IsDir(), check.Equals, false)
}

func (s *lowLevelSuite) TestOpenSuccess(c *check.C) {
	// By default system calls succeed and get recorded for inspection.
	fd := mylog.Check2(s.sys.Open("/some/path", syscall.O_NOFOLLOW|syscall.O_CLOEXEC|syscall.O_RDWR|syscall.O_CREAT|syscall.O_EXCL, 0))
	c.Assert(err, check.IsNil)
	c.Assert(fd, check.Equals, 3)
	c.Assert(s.sys.Calls(), check.DeepEquals, []string{
		`open "/some/path" O_NOFOLLOW|O_CLOEXEC|O_RDWR|O_CREAT|O_EXCL 0`, // -> 3
	})
	c.Assert(s.sys.RCalls(), check.DeepEquals, []testutil.CallResultError{
		{C: `open "/some/path" O_NOFOLLOW|O_CLOEXEC|O_RDWR|O_CREAT|O_EXCL 0`, R: 3},
	})
}

func (s *lowLevelSuite) TestOpenFailure(c *check.C) {
	// Any call can be made to fail using InsertFault()
	s.sys.InsertFault(`open "/some/path" 0 0`, syscall.ENOENT)
	fd := mylog.Check2(s.sys.Open("/some/path", 0, 0))
	c.Assert(err, check.ErrorMatches, "no such file or directory")
	c.Assert(fd, check.Equals, -1)
	c.Assert(s.sys.Calls(), check.DeepEquals, []string{
		`open "/some/path" 0 0`, // -> ENOENT
	})
	c.Assert(s.sys.RCalls(), check.DeepEquals, []testutil.CallResultError{
		{C: `open "/some/path" 0 0`, E: syscall.ENOENT},
	})
}

func (s *lowLevelSuite) TestOpenVariableFailure(c *check.C) {
	// The way a particular call fails may vary over time.
	// Subsequent errors are returned on subsequent calls.
	s.sys.InsertFault(`open "/some/path" O_RDWR 0`, syscall.ENOENT, syscall.EPERM)
	fd := mylog.Check2(s.sys.Open("/some/path", syscall.O_RDWR, 0))
	c.Assert(err, check.ErrorMatches, "no such file or directory")
	c.Assert(fd, check.Equals, -1)
	// 2nd attempt
	fd = mylog.Check2(s.sys.Open("/some/path", syscall.O_RDWR, 0))
	c.Assert(err, check.ErrorMatches, "operation not permitted")
	c.Assert(fd, check.Equals, -1)
	// 3rd attempt
	fd = mylog.Check2(s.sys.Open("/some/path", syscall.O_RDWR, 0))
	c.Assert(err, check.IsNil)
	c.Assert(fd, check.Equals, 3)
	c.Assert(s.sys.Calls(), check.DeepEquals, []string{
		`open "/some/path" O_RDWR 0`, // -> ENOENT
		`open "/some/path" O_RDWR 0`, // -> EPERM
		`open "/some/path" O_RDWR 0`, // -> 3
	})
	c.Assert(s.sys.RCalls(), check.DeepEquals, []testutil.CallResultError{
		{C: `open "/some/path" O_RDWR 0`, E: syscall.ENOENT},
		{C: `open "/some/path" O_RDWR 0`, E: syscall.EPERM},
		{C: `open "/some/path" O_RDWR 0`, R: 3},
	})
}

func (s *lowLevelSuite) TestOpenCustomFailure(c *check.C) {
	// The way a particular call may also be arbitrarily programmed.
	n := 3
	s.sys.InsertFaultFunc(`open "/some/path" O_RDWR 0`, func() error {
		if n > 0 {
			mylog.Check(fmt.Errorf("%d more", n))
			n--
			return err
		}
		return nil
	})
	_ := mylog.Check2(s.sys.Open("/some/path", syscall.O_RDWR, 0))
	c.Assert(err, check.ErrorMatches, "3 more")
	_ = mylog.Check2(s.sys.Open("/some/path", syscall.O_RDWR, 0))
	c.Assert(err, check.ErrorMatches, "2 more")
	_ = mylog.Check2(s.sys.Open("/some/path", syscall.O_RDWR, 0))
	c.Assert(err, check.ErrorMatches, "1 more")
	fd := mylog.Check2(s.sys.Open("/some/path", syscall.O_RDWR, 0))
	c.Assert(err, check.IsNil)
	c.Assert(fd, check.Equals, 3)
	c.Assert(s.sys.Calls(), check.DeepEquals, []string{
		`open "/some/path" O_RDWR 0`, // -> 3 more
		`open "/some/path" O_RDWR 0`, // -> 2 more
		`open "/some/path" O_RDWR 0`, // -> 1 more
		`open "/some/path" O_RDWR 0`, // -> 3
	})
	c.Assert(s.sys.RCalls(), check.DeepEquals, []testutil.CallResultError{
		{C: `open "/some/path" O_RDWR 0`, E: fmt.Errorf("3 more")},
		{C: `open "/some/path" O_RDWR 0`, E: fmt.Errorf("2 more")},
		{C: `open "/some/path" O_RDWR 0`, E: fmt.Errorf("1 more")},
		{C: `open "/some/path" O_RDWR 0`, R: 3},
	})
}

func (s *lowLevelSuite) TestUnclosedFile(c *check.C) {
	// Open file descriptors can be detected in suite teardown using either
	// StrayDescriptorError or CheckForStrayDescriptors.
	fd := mylog.Check2(s.sys.Open("/some/path", syscall.O_RDWR, 0))
	c.Assert(err, check.IsNil)
	c.Assert(fd, check.Equals, 3)
	c.Assert(s.sys.Calls(), check.DeepEquals, []string{
		`open "/some/path" O_RDWR 0`, // -> 3
	})
	c.Assert(s.sys.RCalls(), check.DeepEquals, []testutil.CallResultError{
		{C: `open "/some/path" O_RDWR 0`, R: 3},
	})
	c.Assert(s.sys.StrayDescriptorsError(), check.ErrorMatches,
		`unclosed file descriptor 3 \(open "/some/path" O_RDWR 0\)`)
}

func (s *lowLevelSuite) TestUnopenedFile(c *check.C) {
	mylog.
		// Closing unopened file descriptors is an error.
		Check(s.sys.Close(7))
	c.Assert(err, check.ErrorMatches, "attempting to close a closed file descriptor 7")
	c.Assert(s.sys.Calls(), check.DeepEquals, []string{`close 7`})
	c.Assert(s.sys.RCalls(), check.DeepEquals, []testutil.CallResultError{
		{C: `close 7`, E: fmt.Errorf("attempting to close a closed file descriptor 7")},
	})
}

func (s *lowLevelSuite) TestCloseSuccess(c *check.C) {
	// Closing file descriptors handles the bookkeeping.
	fd := mylog.Check2(s.sys.Open("/some/path", syscall.O_RDWR, 0))
	c.Assert(err, check.IsNil)
	mylog.Check(s.sys.Close(fd))
	c.Assert(err, check.IsNil)
	c.Assert(s.sys.Calls(), check.DeepEquals, []string{
		`open "/some/path" O_RDWR 0`, // -> 3
		`close 3`,
	})
	c.Assert(s.sys.RCalls(), check.DeepEquals, []testutil.CallResultError{
		{C: `open "/some/path" O_RDWR 0`, R: 3},
		{C: `close 3`},
	})
	c.Assert(s.sys.StrayDescriptorsError(), check.IsNil)
}

func (s *lowLevelSuite) TestCloseFailure(c *check.C) {
	// Close can be made to fail just like any other function.
	s.sys.InsertFault(`close 3`, syscall.ENOSYS)
	mylog.Check(s.sys.Close(3))
	c.Assert(err, check.ErrorMatches, "function not implemented")
	c.Assert(s.sys.Calls(), check.DeepEquals, []string{
		`close 3`,
	})
	c.Assert(s.sys.RCalls(), check.DeepEquals, []testutil.CallResultError{
		{C: `close 3`, E: syscall.ENOSYS},
	})
}

func (s *lowLevelSuite) TestOpenatSuccess(c *check.C) {
	dirfd := mylog.Check2(s.sys.Open("/", syscall.O_DIRECTORY, 0))
	c.Assert(err, check.IsNil)
	fd := mylog.Check2(s.sys.Openat(dirfd, "foo", syscall.O_DIRECTORY, 0))
	c.Assert(err, check.IsNil)
	c.Assert(s.sys.Close(fd), check.IsNil)
	c.Assert(s.sys.Close(dirfd), check.IsNil)
	c.Assert(s.sys.Calls(), check.DeepEquals, []string{
		`open "/" O_DIRECTORY 0`,       // -> 3
		`openat 3 "foo" O_DIRECTORY 0`, // -> 4
		`close 4`,
		`close 3`,
	})
	c.Assert(s.sys.RCalls(), check.DeepEquals, []testutil.CallResultError{
		{C: `open "/" O_DIRECTORY 0`, R: 3},
		{C: `openat 3 "foo" O_DIRECTORY 0`, R: 4},
		{C: `close 4`},
		{C: `close 3`},
	})
}

func (s *lowLevelSuite) TestOpenatFailure(c *check.C) {
	dirfd := mylog.Check2(s.sys.Open("/", syscall.O_DIRECTORY, 0))
	c.Assert(err, check.IsNil)
	s.sys.InsertFault(`openat 3 "foo" O_DIRECTORY 0`, syscall.ENOENT)
	fd := mylog.Check2(s.sys.Openat(dirfd, "foo", syscall.O_DIRECTORY, 0))
	c.Assert(err, check.ErrorMatches, "no such file or directory")
	c.Assert(fd, check.Equals, -1)
	c.Assert(s.sys.Close(dirfd), check.IsNil)
	c.Assert(s.sys.Calls(), check.DeepEquals, []string{
		`open "/" O_DIRECTORY 0`,       // -> 3
		`openat 3 "foo" O_DIRECTORY 0`, // -> ENOENT
		`close 3`,
	})
	c.Assert(s.sys.RCalls(), check.DeepEquals, []testutil.CallResultError{
		{C: `open "/" O_DIRECTORY 0`, R: 3},
		{C: `openat 3 "foo" O_DIRECTORY 0`, E: syscall.ENOENT},
		{C: `close 3`},
	})
}

func (s *lowLevelSuite) TestOpenatBadFd(c *check.C) {
	fd := mylog.Check2(s.sys.Openat(3, "foo", syscall.O_DIRECTORY, 0))
	c.Assert(err, check.ErrorMatches, "attempting to openat with an invalid file descriptor 3")
	c.Assert(fd, check.Equals, -1)
	c.Assert(s.sys.Calls(), check.DeepEquals, []string{
		`openat 3 "foo" O_DIRECTORY 0`, // -> error
	})
	c.Assert(s.sys.RCalls(), check.DeepEquals, []testutil.CallResultError{
		{C: `openat 3 "foo" O_DIRECTORY 0`, E: fmt.Errorf("attempting to openat with an invalid file descriptor 3")},
	})
}

func (s *lowLevelSuite) TestFchownSuccess(c *check.C) {
	fd := mylog.Check2(s.sys.Open("/", syscall.O_DIRECTORY, 0))
	c.Assert(err, check.IsNil)
	mylog.Check(s.sys.Fchown(fd, 0, 0))
	c.Assert(err, check.IsNil)
	c.Assert(s.sys.Close(fd), check.IsNil)
	c.Assert(s.sys.Calls(), check.DeepEquals, []string{
		`open "/" O_DIRECTORY 0`, // -> 3
		`fchown 3 0 0`,
		`close 3`,
	})
	c.Assert(s.sys.RCalls(), check.DeepEquals, []testutil.CallResultError{
		{C: `open "/" O_DIRECTORY 0`, R: 3},
		{C: `fchown 3 0 0`},
		{C: `close 3`},
	})
}

func (s *lowLevelSuite) TestFchownFailure(c *check.C) {
	fd := mylog.Check2(s.sys.Open("/", syscall.O_DIRECTORY, 0))
	c.Assert(err, check.IsNil)
	s.sys.InsertFault(`fchown 3 0 0`, syscall.EPERM)
	mylog.Check(s.sys.Fchown(fd, 0, 0))
	c.Assert(err, check.ErrorMatches, "operation not permitted")
	c.Assert(s.sys.Close(fd), check.IsNil)
	c.Assert(s.sys.Calls(), check.DeepEquals, []string{
		`open "/" O_DIRECTORY 0`, // -> 3
		`fchown 3 0 0`,           // -> EPERM
		`close 3`,
	})
	c.Assert(s.sys.RCalls(), check.DeepEquals, []testutil.CallResultError{
		{C: `open "/" O_DIRECTORY 0`, R: 3},
		{C: `fchown 3 0 0`, E: syscall.EPERM},
		{C: `close 3`},
	})
}

func (s *lowLevelSuite) TestFchownBadFd(c *check.C) {
	mylog.Check(s.sys.Fchown(3, 0, 0))
	c.Assert(err, check.ErrorMatches, "attempting to fchown an invalid file descriptor 3")
	c.Assert(s.sys.Calls(), check.DeepEquals, []string{
		`fchown 3 0 0`,
	})
	c.Assert(s.sys.RCalls(), check.DeepEquals, []testutil.CallResultError{
		{C: `fchown 3 0 0`, E: fmt.Errorf("attempting to fchown an invalid file descriptor 3")},
	})
}

func (s *lowLevelSuite) TestMkdiratSuccess(c *check.C) {
	fd := mylog.Check2(s.sys.Open("/", syscall.O_DIRECTORY, 0))
	c.Assert(err, check.IsNil)
	mylog.Check(s.sys.Mkdirat(fd, "foo", 0755))
	c.Assert(err, check.IsNil)
	c.Assert(s.sys.Close(fd), check.IsNil)
	c.Assert(s.sys.Calls(), check.DeepEquals, []string{
		`open "/" O_DIRECTORY 0`, // -> 3
		`mkdirat 3 "foo" 0755`,
		`close 3`,
	})
	c.Assert(s.sys.RCalls(), check.DeepEquals, []testutil.CallResultError{
		{C: `open "/" O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "foo" 0755`},
		{C: `close 3`},
	})
}

func (s *lowLevelSuite) TestMkdiratFailure(c *check.C) {
	fd := mylog.Check2(s.sys.Open("/", syscall.O_DIRECTORY, 0))
	c.Assert(err, check.IsNil)
	s.sys.InsertFault(`mkdirat 3 "foo" 0755`, syscall.EPERM)
	mylog.Check(s.sys.Mkdirat(fd, "foo", 0755))
	c.Assert(err, check.ErrorMatches, "operation not permitted")
	c.Assert(s.sys.Close(fd), check.IsNil)
	c.Assert(s.sys.Calls(), check.DeepEquals, []string{
		`open "/" O_DIRECTORY 0`, // -> 3
		`mkdirat 3 "foo" 0755`,   // -> EPERM
		`close 3`,
	})
	c.Assert(s.sys.RCalls(), check.DeepEquals, []testutil.CallResultError{
		{C: `open "/" O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "foo" 0755`, E: syscall.EPERM},
		{C: `close 3`},
	})
}

func (s *lowLevelSuite) TestMkdiratBadFd(c *check.C) {
	mylog.Check(s.sys.Mkdirat(3, "foo", 0755))
	c.Assert(err, check.ErrorMatches, "attempting to mkdirat with an invalid file descriptor 3")
	c.Assert(s.sys.Calls(), check.DeepEquals, []string{
		`mkdirat 3 "foo" 0755`,
	})
	c.Assert(s.sys.RCalls(), check.DeepEquals, []testutil.CallResultError{
		{C: `mkdirat 3 "foo" 0755`, E: fmt.Errorf("attempting to mkdirat with an invalid file descriptor 3")},
	})
}

func (s *lowLevelSuite) TestMountSuccess(c *check.C) {
	mylog.Check(s.sys.Mount("source", "target", "fstype", syscall.MS_BIND|syscall.MS_REC|syscall.MS_RDONLY, ""))
	c.Assert(err, check.IsNil)
	c.Assert(s.sys.Calls(), check.DeepEquals, []string{
		`mount "source" "target" "fstype" MS_BIND|MS_REC|MS_RDONLY ""`,
	})
	c.Assert(s.sys.RCalls(), check.DeepEquals, []testutil.CallResultError{
		{C: `mount "source" "target" "fstype" MS_BIND|MS_REC|MS_RDONLY ""`},
	})
}

func (s *lowLevelSuite) TestMountPropagation(c *check.C) {
	c.Assert(s.sys.Mount("", "target", "", syscall.MS_SHARED, ""), check.IsNil)
	c.Assert(s.sys.Mount("", "target", "", syscall.MS_SLAVE, ""), check.IsNil)
	c.Assert(s.sys.Mount("", "target", "", syscall.MS_PRIVATE, ""), check.IsNil)
	c.Assert(s.sys.Mount("", "target", "", syscall.MS_UNBINDABLE, ""), check.IsNil)
	c.Assert(s.sys.Calls(), check.DeepEquals, []string{
		`mount "" "target" "" MS_SHARED ""`,
		`mount "" "target" "" MS_SLAVE ""`,
		`mount "" "target" "" MS_PRIVATE ""`,
		`mount "" "target" "" MS_UNBINDABLE ""`,
	})
	c.Assert(s.sys.RCalls(), check.DeepEquals, []testutil.CallResultError{
		{C: `mount "" "target" "" MS_SHARED ""`},
		{C: `mount "" "target" "" MS_SLAVE ""`},
		{C: `mount "" "target" "" MS_PRIVATE ""`},
		{C: `mount "" "target" "" MS_UNBINDABLE ""`},
	})
}

func (s *lowLevelSuite) TestMountFailure(c *check.C) {
	s.sys.InsertFault(`mount "source" "target" "fstype" 0 ""`, syscall.EPERM)
	mylog.Check(s.sys.Mount("source", "target", "fstype", 0, ""))
	c.Assert(err, check.ErrorMatches, "operation not permitted")
	c.Assert(s.sys.Calls(), check.DeepEquals, []string{
		`mount "source" "target" "fstype" 0 ""`,
	})
	c.Assert(s.sys.RCalls(), check.DeepEquals, []testutil.CallResultError{
		{C: `mount "source" "target" "fstype" 0 ""`, E: syscall.EPERM},
	})
}

func (s *lowLevelSuite) TestUnmountSuccess(c *check.C) {
	mylog.Check(s.sys.Unmount("target", testutil.UmountNoFollow|syscall.MNT_DETACH))
	c.Assert(err, check.IsNil)
	c.Assert(s.sys.Calls(), check.DeepEquals, []string{`unmount "target" UMOUNT_NOFOLLOW|MNT_DETACH`})
	c.Assert(s.sys.RCalls(), check.DeepEquals, []testutil.CallResultError{
		{C: `unmount "target" UMOUNT_NOFOLLOW|MNT_DETACH`},
	})
}

func (s *lowLevelSuite) TestUnmountFailure(c *check.C) {
	s.sys.InsertFault(`unmount "target" 0`, syscall.EPERM)
	mylog.Check(s.sys.Unmount("target", 0))
	c.Assert(err, check.ErrorMatches, "operation not permitted")
	c.Assert(s.sys.Calls(), check.DeepEquals, []string{`unmount "target" 0`})
	c.Assert(s.sys.RCalls(), check.DeepEquals, []testutil.CallResultError{
		{C: `unmount "target" 0`, E: syscall.EPERM},
	})
}

func (s *lowLevelSuite) TestOsLstat(c *check.C) {
	// When a function returns some data it must be fed either an error or a result.
	c.Assert(func() { s.sys.OsLstat("/foo") }, check.PanicMatches,
		`one of InsertOsLstatResult\(\) or InsertFault\(\) for lstat "/foo" must be used`)
}

func (s *lowLevelSuite) TestOsLstatSuccess(c *check.C) {
	// The fed data is returned in absence of errors.
	s.sys.InsertOsLstatResult(`lstat "/foo"`, testutil.FileInfoFile)
	fi := mylog.Check2(s.sys.OsLstat("/foo"))
	c.Assert(err, check.IsNil)
	c.Assert(fi, check.DeepEquals, testutil.FileInfoFile)
	c.Assert(s.sys.Calls(), check.DeepEquals, []string{`lstat "/foo"`})
	c.Assert(s.sys.RCalls(), check.DeepEquals, []testutil.CallResultError{
		{C: `lstat "/foo"`, R: testutil.FileInfoFile},
	})
}

func (s *lowLevelSuite) TestOsLstatFailure(c *check.C) {
	// Errors take priority over data.
	s.sys.InsertOsLstatResult(`lstat "/foo"`, testutil.FileInfoFile)
	s.sys.InsertFault(`lstat "/foo"`, syscall.ENOENT)
	fi := mylog.Check2(s.sys.OsLstat("/foo"))
	c.Assert(err, check.ErrorMatches, "no such file or directory")
	c.Assert(fi, check.IsNil)
	c.Assert(s.sys.Calls(), check.DeepEquals, []string{`lstat "/foo"`})
	c.Assert(s.sys.RCalls(), check.DeepEquals, []testutil.CallResultError{
		{C: `lstat "/foo"`, E: syscall.ENOENT},
	})
}

func (s *lowLevelSuite) TestSysLstat(c *check.C) {
	// When a function returns some data it must be fed either an error or a result.
	var buf syscall.Stat_t
	c.Assert(func() { s.sys.SysLstat("/foo", &buf) }, check.PanicMatches,
		`one of InsertSysLstatResult\(\) or InsertFault\(\) for lstat "/foo" <ptr> must be used`)
}

func (s *lowLevelSuite) TestSysLstatSuccess(c *check.C) {
	// The fed data is returned in absence of errors.
	var buf syscall.Stat_t
	s.sys.InsertSysLstatResult(`lstat "/foo" <ptr>`, syscall.Stat_t{Uid: 123})
	mylog.Check(s.sys.SysLstat("/foo", &buf))
	c.Assert(err, check.IsNil)
	c.Assert(buf, check.DeepEquals, syscall.Stat_t{Uid: 123})
	c.Assert(s.sys.Calls(), check.DeepEquals, []string{
		`lstat "/foo" <ptr>`,
	})
	c.Assert(s.sys.RCalls(), check.DeepEquals, []testutil.CallResultError{
		{C: `lstat "/foo" <ptr>`, R: syscall.Stat_t{Uid: 123}},
	})
}

func (s *lowLevelSuite) TestSysLstatFailure(c *check.C) {
	// Errors take priority over data.
	var buf syscall.Stat_t
	s.sys.InsertSysLstatResult(`lstat "/foo" <ptr>`, syscall.Stat_t{Uid: 123})
	s.sys.InsertFault(`lstat "/foo" <ptr>`, syscall.ENOENT)
	mylog.Check(s.sys.SysLstat("/foo", &buf))
	c.Assert(err, check.ErrorMatches, "no such file or directory")
	c.Assert(buf, check.DeepEquals, syscall.Stat_t{})
	c.Assert(s.sys.Calls(), check.DeepEquals, []string{
		`lstat "/foo" <ptr>`, // -> ENOENT
	})
	c.Assert(s.sys.RCalls(), check.DeepEquals, []testutil.CallResultError{
		{C: `lstat "/foo" <ptr>`, E: syscall.ENOENT},
	})
}

func (s *lowLevelSuite) TestFstat(c *check.C) {
	fd := mylog.Check2(s.sys.Open("/foo", syscall.O_RDONLY, 0))
	c.Assert(err, check.IsNil)
	var buf syscall.Stat_t
	c.Assert(func() { s.sys.Fstat(fd, &buf) }, check.PanicMatches,
		`one of InsertFstatResult\(\) or InsertFault\(\) for fstat 3 <ptr> must be used`)
}

func (s *lowLevelSuite) TestFstatBadFd(c *check.C) {
	var buf syscall.Stat_t
	mylog.Check(s.sys.Fstat(3, &buf))
	c.Assert(err, check.ErrorMatches, "attempting to fstat with an invalid file descriptor 3")
	c.Assert(s.sys.Calls(), check.DeepEquals, []string{
		`fstat 3 <ptr>`,
	})
	c.Assert(s.sys.RCalls(), check.DeepEquals, []testutil.CallResultError{
		{C: `fstat 3 <ptr>`, E: fmt.Errorf("attempting to fstat with an invalid file descriptor 3")},
	})
}

func (s *lowLevelSuite) TestFstatSuccess(c *check.C) {
	s.sys.InsertFstatResult(`fstat 3 <ptr>`, syscall.Stat_t{Dev: 0xC0FE})
	fd := mylog.Check2(s.sys.Open("/foo", syscall.O_RDONLY, 0))
	c.Assert(err, check.IsNil)
	var buf syscall.Stat_t
	mylog.Check(s.sys.Fstat(fd, &buf))
	c.Assert(err, check.IsNil)
	c.Assert(buf, check.Equals, syscall.Stat_t{Dev: 0xC0FE})
	c.Assert(s.sys.Calls(), check.DeepEquals, []string{
		`open "/foo" 0 0`, // -> 3
		`fstat 3 <ptr>`,
	})
	c.Assert(s.sys.RCalls(), check.DeepEquals, []testutil.CallResultError{
		{C: `open "/foo" 0 0`, R: 3},
		{C: `fstat 3 <ptr>`, R: syscall.Stat_t{Dev: 0xC0FE}},
	})
}

func (s *lowLevelSuite) TestFstatFailure(c *check.C) {
	s.sys.InsertFault(`fstat 3 <ptr>`, syscall.EPERM)
	fd := mylog.Check2(s.sys.Open("/foo", syscall.O_RDONLY, 0))
	c.Assert(err, check.IsNil)
	var buf syscall.Stat_t
	mylog.Check(s.sys.Fstat(fd, &buf))
	c.Assert(err, check.ErrorMatches, "operation not permitted")
	c.Assert(buf, check.Equals, syscall.Stat_t{})
	c.Assert(s.sys.Calls(), check.DeepEquals, []string{
		`open "/foo" 0 0`, // -> 3
		`fstat 3 <ptr>`,   // -> EPERM
	})
	c.Assert(s.sys.RCalls(), check.DeepEquals, []testutil.CallResultError{
		{C: `open "/foo" 0 0`, R: 3},
		{C: `fstat 3 <ptr>`, E: syscall.EPERM},
	})
}

func (s *lowLevelSuite) TestFstatfs(c *check.C) {
	fd := mylog.Check2(s.sys.Open("/foo", syscall.O_RDONLY, 0))
	c.Assert(err, check.IsNil)
	var buf syscall.Statfs_t
	c.Assert(func() { s.sys.Fstatfs(fd, &buf) }, check.PanicMatches,
		`one of InsertFstatfsResult\(\) or InsertFault\(\) for fstatfs 3 <ptr> must be used`)
}

func (s *lowLevelSuite) TestFstatfsBadFd(c *check.C) {
	var buf syscall.Statfs_t
	mylog.Check(s.sys.Fstatfs(3, &buf))
	c.Assert(err, check.ErrorMatches, "attempting to fstatfs with an invalid file descriptor 3")
	c.Assert(s.sys.Calls(), check.DeepEquals, []string{`fstatfs 3 <ptr>`})
	c.Assert(s.sys.RCalls(), check.DeepEquals, []testutil.CallResultError{
		{C: `fstatfs 3 <ptr>`, E: fmt.Errorf("attempting to fstatfs with an invalid file descriptor 3")},
	})
}

func (s *lowLevelSuite) TestFstatfsSuccess(c *check.C) {
	s.sys.InsertFstatfsResult(`fstatfs 3 <ptr>`, syscall.Statfs_t{Type: 0x123})
	fd := mylog.Check2(s.sys.Open("/foo", syscall.O_RDONLY, 0))
	c.Assert(err, check.IsNil)
	var buf syscall.Statfs_t
	mylog.Check(s.sys.Fstatfs(fd, &buf))
	c.Assert(err, check.IsNil)
	c.Assert(buf, check.Equals, syscall.Statfs_t{Type: 0x123})
	c.Assert(s.sys.Calls(), check.DeepEquals, []string{
		`open "/foo" 0 0`, // -> 3
		`fstatfs 3 <ptr>`, // -> Type: 0x123
	})
	c.Assert(s.sys.RCalls(), check.DeepEquals, []testutil.CallResultError{
		{C: `open "/foo" 0 0`, R: 3},
		{C: `fstatfs 3 <ptr>`, R: syscall.Statfs_t{Type: 0x123}},
	})
}

func (s *lowLevelSuite) TestFstatfsChain(c *check.C) {
	s.sys.InsertFstatfsResult(`fstatfs 3 <ptr>`,
		syscall.Statfs_t{Type: 0x123}, syscall.Statfs_t{Type: 0x456})
	fd := mylog.Check2(s.sys.Open("/foo", syscall.O_RDONLY, 0))
	c.Assert(err, check.IsNil)
	var buf syscall.Statfs_t
	mylog.Check(s.sys.Fstatfs(fd, &buf))
	c.Assert(err, check.IsNil)
	c.Assert(buf, check.Equals, syscall.Statfs_t{Type: 0x123})
	mylog.Check(s.sys.Fstatfs(fd, &buf))
	c.Assert(err, check.IsNil)
	c.Assert(buf, check.Equals, syscall.Statfs_t{Type: 0x456})
	mylog.Check(s.sys.Fstatfs(fd, &buf))
	c.Assert(err, check.IsNil)
	c.Assert(buf, check.Equals, syscall.Statfs_t{Type: 0x456})
	c.Assert(s.sys.Calls(), check.DeepEquals, []string{
		`open "/foo" 0 0`, // -> 3
		`fstatfs 3 <ptr>`, // -> Type: 0x123
		`fstatfs 3 <ptr>`, // -> Type: 0x456
		`fstatfs 3 <ptr>`, // -> Type: 0x456
	})
	c.Assert(s.sys.RCalls(), check.DeepEquals, []testutil.CallResultError{
		{C: `open "/foo" 0 0`, R: 3},
		{C: `fstatfs 3 <ptr>`, R: syscall.Statfs_t{Type: 0x123}},
		{C: `fstatfs 3 <ptr>`, R: syscall.Statfs_t{Type: 0x456}},
		{C: `fstatfs 3 <ptr>`, R: syscall.Statfs_t{Type: 0x456}},
	})
}

func (s *lowLevelSuite) TestFstatfsFailure(c *check.C) {
	s.sys.InsertFault(`fstatfs 3 <ptr>`, syscall.EPERM)
	fd := mylog.Check2(s.sys.Open("/foo", syscall.O_RDONLY, 0))
	c.Assert(err, check.IsNil)
	var buf syscall.Statfs_t
	mylog.Check(s.sys.Fstatfs(fd, &buf))
	c.Assert(err, check.ErrorMatches, "operation not permitted")
	c.Assert(buf, check.Equals, syscall.Statfs_t{})
	c.Assert(s.sys.Calls(), check.DeepEquals, []string{
		`open "/foo" 0 0`, // -> 3
		`fstatfs 3 <ptr>`, // -> EPERM
	})
	c.Assert(s.sys.RCalls(), check.DeepEquals, []testutil.CallResultError{
		{C: `open "/foo" 0 0`, R: 3},
		{C: `fstatfs 3 <ptr>`, E: syscall.EPERM},
	})
}

func (s *lowLevelSuite) TestReadDir(c *check.C) {
	c.Assert(func() { s.sys.ReadDir("/foo") }, check.PanicMatches,
		`one of InsertReadDirResult\(\) or InsertFault\(\) for readdir "/foo" must be used`)
}

func (s *lowLevelSuite) TestReadDirSuccess(c *check.C) {
	files := []fs.DirEntry{
		testutil.FakeDirEntry("file", 0644),
		testutil.FakeDirEntry("dir", 0755|os.ModeDir),
	}
	s.sys.InsertReadDirResult(`readdir "/foo"`, files)
	files := mylog.Check2(s.sys.ReadDir("/foo"))
	c.Assert(err, check.IsNil)
	c.Assert(files, check.HasLen, 2)
	c.Assert(s.sys.Calls(), check.DeepEquals, []string{
		`readdir "/foo"`,
	})
	c.Assert(s.sys.RCalls(), check.DeepEquals, []testutil.CallResultError{
		{C: `readdir "/foo"`, R: files},
	})
}

func (s *lowLevelSuite) TestReadDirFailure(c *check.C) {
	s.sys.InsertFault(`readdir "/foo"`, syscall.ENOENT)
	files := mylog.Check2(s.sys.ReadDir("/foo"))
	c.Assert(err, check.ErrorMatches, "no such file or directory")
	c.Assert(files, check.IsNil)
	c.Assert(s.sys.Calls(), check.DeepEquals, []string{
		`readdir "/foo"`, // -> ENOENT
	})
	c.Assert(s.sys.RCalls(), check.DeepEquals, []testutil.CallResultError{
		{C: `readdir "/foo"`, E: syscall.ENOENT},
	})
}

func (s *lowLevelSuite) TestSymlinkSuccess(c *check.C) {
	mylog.Check(s.sys.Symlink("oldname", "newname"))
	c.Assert(err, check.IsNil)
	c.Assert(s.sys.Calls(), check.DeepEquals, []string{
		`symlink "newname" -> "oldname"`,
	})
	c.Assert(s.sys.RCalls(), check.DeepEquals, []testutil.CallResultError{
		{C: `symlink "newname" -> "oldname"`},
	})
}

func (s *lowLevelSuite) TestSymlinkFailure(c *check.C) {
	s.sys.InsertFault(`symlink "newname" -> "oldname"`, syscall.EPERM)
	mylog.Check(s.sys.Symlink("oldname", "newname"))
	c.Assert(err, check.ErrorMatches, "operation not permitted")
	c.Assert(s.sys.Calls(), check.DeepEquals, []string{
		`symlink "newname" -> "oldname"`, // -> EPERM
	})
	c.Assert(s.sys.RCalls(), check.DeepEquals, []testutil.CallResultError{
		{C: `symlink "newname" -> "oldname"`, E: syscall.EPERM},
	})
}

func (s *lowLevelSuite) TestRemoveSuccess(c *check.C) {
	mylog.Check(s.sys.Remove("file"))
	c.Assert(err, check.IsNil)
	c.Assert(s.sys.Calls(), check.DeepEquals, []string{
		`remove "file"`,
	})
	c.Assert(s.sys.RCalls(), check.DeepEquals, []testutil.CallResultError{
		{C: `remove "file"`},
	})
}

func (s *lowLevelSuite) TestRemoveFailure(c *check.C) {
	s.sys.InsertFault(`remove "file"`, syscall.EPERM)
	mylog.Check(s.sys.Remove("file"))
	c.Assert(err, check.ErrorMatches, "operation not permitted")
	c.Assert(s.sys.Calls(), check.DeepEquals, []string{`remove "file"`})
	c.Assert(s.sys.Calls(), check.DeepEquals, []string{
		`remove "file"`, // -> EPERM
	})
	c.Assert(s.sys.RCalls(), check.DeepEquals, []testutil.CallResultError{
		{C: `remove "file"`, E: syscall.EPERM},
	})
}

func (s *lowLevelSuite) TestSymlinkatBadFd(c *check.C) {
	mylog.Check(s.sys.Symlinkat("/old", 3, "new"))
	c.Assert(err, check.ErrorMatches, "attempting to symlinkat with an invalid file descriptor 3")
	c.Assert(s.sys.Calls(), check.DeepEquals, []string{
		`symlinkat "/old" 3 "new"`,
	})
	c.Assert(s.sys.RCalls(), check.DeepEquals, []testutil.CallResultError{
		{C: `symlinkat "/old" 3 "new"`, E: fmt.Errorf("attempting to symlinkat with an invalid file descriptor 3")},
	})
}

func (s *lowLevelSuite) TestSymlinkatSuccess(c *check.C) {
	fd := mylog.Check2(s.sys.Open("/foo", syscall.O_RDONLY, 0))
	c.Assert(err, check.IsNil)
	mylog.Check(s.sys.Symlinkat("/old", fd, "new"))
	c.Assert(err, check.IsNil)
	c.Assert(s.sys.Calls(), check.DeepEquals, []string{
		`open "/foo" 0 0`,
		`symlinkat "/old" 3 "new"`,
	})
	c.Assert(s.sys.RCalls(), check.DeepEquals, []testutil.CallResultError{
		{C: `open "/foo" 0 0`, R: 3},
		{C: `symlinkat "/old" 3 "new"`},
	})
}

func (s *lowLevelSuite) TestSymlinkatFailure(c *check.C) {
	s.sys.InsertFault(`symlinkat "/old" 3 "new"`, syscall.EPERM)
	fd := mylog.Check2(s.sys.Open("/foo", syscall.O_RDONLY, 0))
	c.Assert(err, check.IsNil)
	mylog.Check(s.sys.Symlinkat("/old", fd, "new"))
	c.Assert(err, check.ErrorMatches, "operation not permitted")
	c.Assert(s.sys.Calls(), check.DeepEquals, []string{
		`open "/foo" 0 0`, // -> 3
		`symlinkat "/old" 3 "new"`,
	})
	c.Assert(s.sys.RCalls(), check.DeepEquals, []testutil.CallResultError{
		{C: `open "/foo" 0 0`, R: 3},
		{C: `symlinkat "/old" 3 "new"`, E: syscall.EPERM},
	})
}

func (s *lowLevelSuite) TestReadlinkat(c *check.C) {
	fd := mylog.Check2(s.sys.Open("/foo", syscall.O_RDONLY, 0))
	c.Assert(err, check.IsNil)
	buf := make([]byte, 10)
	c.Assert(func() { s.sys.Readlinkat(fd, "new", buf) }, check.PanicMatches,
		`one of InsertReadlinkatResult\(\) or InsertFault\(\) for readlinkat 3 "new" <ptr> must be used`)
}

func (s *lowLevelSuite) TestReadlinkatBadFd(c *check.C) {
	buf := make([]byte, 10)
	n := mylog.Check2(s.sys.Readlinkat(3, "new", buf))
	c.Assert(err, check.ErrorMatches, "attempting to readlinkat with an invalid file descriptor 3")
	c.Assert(n, check.Equals, 0)
	c.Assert(s.sys.Calls(), check.DeepEquals, []string{
		`readlinkat 3 "new" <ptr>`,
	})
	c.Assert(s.sys.RCalls(), check.DeepEquals, []testutil.CallResultError{
		{C: `readlinkat 3 "new" <ptr>`, E: fmt.Errorf("attempting to readlinkat with an invalid file descriptor 3")},
	})
}

func (s *lowLevelSuite) TestReadlinkatSuccess(c *check.C) {
	s.sys.InsertReadlinkatResult(`readlinkat 3 "new" <ptr>`, "/old")
	fd := mylog.Check2(s.sys.Open("/foo", syscall.O_RDONLY, 0))
	c.Assert(err, check.IsNil)

	// Buffer has enough room
	buf := make([]byte, 10)
	n := mylog.Check2(s.sys.Readlinkat(fd, "new", buf))
	c.Assert(err, check.IsNil)
	c.Assert(n, check.Equals, 4)
	c.Assert(buf, check.DeepEquals, []byte{'/', 'o', 'l', 'd', 0, 0, 0, 0, 0, 0})

	// Buffer is too short
	buf = make([]byte, 2)
	n = mylog.Check2(s.sys.Readlinkat(fd, "new", buf))
	c.Assert(err, check.IsNil)
	c.Assert(n, check.Equals, 2)
	c.Assert(buf, check.DeepEquals, []byte{'/', 'o'})
}

func (s *lowLevelSuite) TestReadlinkatFailure(c *check.C) {
	s.sys.InsertFault(`readlinkat 3 "new" <ptr>`, syscall.EPERM)
	fd := mylog.Check2(s.sys.Open("/foo", syscall.O_RDONLY, 0))
	c.Assert(err, check.IsNil)

	buf := make([]byte, 10)
	n := mylog.Check2(s.sys.Readlinkat(fd, "new", buf))
	c.Assert(err, check.ErrorMatches, "operation not permitted")
	c.Assert(n, check.Equals, 0)
	c.Assert(buf, check.DeepEquals, []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0})
}
