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

package main_test

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	. "gopkg.in/check.v1"

	update "github.com/snapcore/snapd/cmd/snap-update-ns"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/sys"
	"github.com/snapcore/snapd/testutil"
)

type utilsSuite struct {
	testutil.BaseTest
	sys *testutil.SyscallRecorder
	log *bytes.Buffer
	as  *update.Assumptions
}

var _ = Suite(&utilsSuite{})

func (s *utilsSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.sys = &testutil.SyscallRecorder{}
	s.BaseTest.AddCleanup(update.MockSystemCalls(s.sys))
	buf, restore := logger.MockLogger()
	s.BaseTest.AddCleanup(restore)
	s.log = buf
	s.as = &update.Assumptions{}
}

func (s *utilsSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
	s.sys.CheckForStrayDescriptors(c)
}

// secure-mkdir-all-within

// Ensure that we refuse to create a directory with an relative path.
func (s *utilsSuite) TestSecureMkdirAllWithinRelative(c *C) {
	err := update.MkdirAllWithin("rel/path", "/", 0755, 123, 456, nil)
	c.Assert(err, ErrorMatches, `cannot use relative path "rel/path"`)
	c.Assert(s.sys.RCalls(), HasLen, 0)

	err = update.MkdirAllWithin("/abs/path", "", 0755, 123, 456, nil)
	c.Assert(err, ErrorMatches, `cannot use relative parent path "."`)
	c.Assert(s.sys.RCalls(), HasLen, 0)
}

// Ensure that we refuse to accept an incorrect parent path.
func (s *utilsSuite) TestSecureMkdirAllWithinBadParent(c *C) {
	err := update.MkdirAllWithin("/parent1/parent2/dir", "/parent2/", 0755, 123, 456, nil)
	c.Assert(err, ErrorMatches, `path "/parent2" is not a parent of "/parent1/parent2/dir"`)
	c.Assert(s.sys.RCalls(), HasLen, 0)

	err = update.MkdirAllWithin("/parent1/parent2/dir", "/parent", 0755, 123, 456, nil)
	c.Assert(err, ErrorMatches, `path "/parent" is not a parent of "/parent1/parent2/dir"`)
	c.Assert(s.sys.RCalls(), HasLen, 0)

	err = update.MkdirAllWithin("/", "/", 0755, 123, 456, nil)
	c.Assert(err, ErrorMatches, `path "/" is not a parent of "/"`)
	c.Assert(s.sys.RCalls(), HasLen, 0)

	err = update.MkdirAllWithin("/parent", "/parent/", 0755, 123, 456, nil)
	c.Assert(err, ErrorMatches, `path "/parent" is not a parent of "/parent"`)
	c.Assert(s.sys.RCalls(), HasLen, 0)
}

// Path already exists.
func (s *utilsSuite) TestSecureMkdirAllWithinPathExists(c *C) {
	s.sys.InsertOsLstatResult(`lstat "/parent/dir"`, testutil.FileInfoDir)
	c.Assert(update.MkdirAllWithin("/parent/dir", "/parent", 0755, 123, 456, nil), IsNil)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/parent/dir"`, R: testutil.FileInfoDir},
	})
}

// Path exists but not a directory.
func (s *utilsSuite) TestSecureMkdirAllWithinPathExistsNotDir(c *C) {
	s.sys.InsertOsLstatResult(`lstat "/parent/dir"`, testutil.FileInfoSymlink)
	c.Assert(update.MkdirAllWithin("/parent/dir", "/parent", 0755, 123, 456, nil), ErrorMatches, `cannot create directory "/parent/dir": existing file in the way`)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/parent/dir"`, R: testutil.FileInfoSymlink},
	})
}

// Cannot inpect path existence.
func (s *utilsSuite) TestSecureMkdirAllWithinPathStatError(c *C) {
	s.sys.InsertFault(`lstat "/parent/dir"`, errors.New("error other than os.ErrNotExist"))
	c.Assert(update.MkdirAllWithin("/parent/dir", "/parent", 0755, 123, 456, nil), ErrorMatches, `cannot inspect path "/parent/dir": error other than os.ErrNotExist`)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/parent/dir"`, E: errors.New("error other than os.ErrNotExist")},
	})
}

// Parent does not exist.
func (s *utilsSuite) TestSecureMkdirAllWithinParentNotExist(c *C) {
	s.sys.InsertFault(`lstat "/parent/dir"`, os.ErrNotExist)
	s.sys.InsertFault(`lstat "/parent"`, os.ErrNotExist)
	c.Assert(update.MkdirAllWithin("/parent/dir", "/parent", 0755, 123, 456, nil), ErrorMatches, `parent directory "/parent" does not exist`)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/parent/dir"`, E: os.ErrNotExist},
		{C: `lstat "/parent"`, E: os.ErrNotExist},
	})
}

// Parent exists but not a directory.
func (s *utilsSuite) TestSecureMkdirAllWithinParentExitsNotDir(c *C) {
	s.sys.InsertFault(`lstat "/parent/dir"`, os.ErrNotExist)
	s.sys.InsertOsLstatResult(`lstat "/parent"`, testutil.FileInfoSymlink)
	c.Assert(update.MkdirAllWithin("/parent/dir", "/parent", 0755, 123, 456, nil), ErrorMatches, `cannot use parent path "/parent": not a directory`)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/parent/dir"`, E: os.ErrNotExist},
		{C: `lstat "/parent"`, R: testutil.FileInfoSymlink},
	})
}

// Cannot inspect parent existence.
func (s *utilsSuite) TestSecureMkdirAllWithinParentStatError(c *C) {
	s.sys.InsertFault(`lstat "/parent/dir"`, os.ErrNotExist)
	s.sys.InsertFault(`lstat "/parent"`, errors.New("error other than os.ErrNotExist"))
	c.Assert(update.MkdirAllWithin("/parent/dir", "/parent", 0755, 123, 456, nil), ErrorMatches, `cannot inspect parent path "/parent": error other than os.ErrNotExist`)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/parent/dir"`, E: os.ErrNotExist},
		{C: `lstat "/parent"`, E: errors.New("error other than os.ErrNotExist")},
	})
}

// First missing dir exists but not a directory
func (s *utilsSuite) TestSecureMkdirAllFirstMissingDirExistsNotDir(c *C) {
	s.sys.InsertFault(`lstat "/parent/missing/dir"`, os.ErrNotExist)
	s.sys.InsertOsLstatResult(`lstat "/parent"`, testutil.FileInfoDir)
	s.sys.InsertOsLstatResult(`lstat "/parent/missing"`, testutil.FileInfoFile)
	c.Assert(update.MkdirAllWithin("/parent/missing/dir", "/parent", 0755, 123, 456, nil), ErrorMatches, `cannot create directory "/parent/missing": existing file in the way`)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/parent/missing/dir"`, E: os.ErrNotExist},
		{C: `lstat "/parent"`, R: testutil.FileInfoDir},
		{C: `lstat "/parent/missing"`, R: testutil.FileInfoFile},
	})
}

// Cannot inspect first missing dir
func (s *utilsSuite) TestSecureMkdsrAllFirstMissingDirStatError(c *C) {
	s.sys.InsertFault(`lstat "/parent/missing/dir"`, os.ErrNotExist)
	s.sys.InsertOsLstatResult(`lstat "/parent"`, testutil.FileInfoDir)
	s.sys.InsertFault(`lstat "/parent/missing"`, errors.New("error other than os.ErrNotExist"))
	c.Assert(update.MkdirAllWithin("/parent/missing/dir", "/parent", 0755, 123, 456, nil), ErrorMatches, `cannot inspect path "/parent/missing": error other than os.ErrNotExist`)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/parent/missing/dir"`, E: os.ErrNotExist},
		{C: `lstat "/parent"`, R: testutil.FileInfoDir},
		{C: `lstat "/parent/missing"`, E: errors.New("error other than os.ErrNotExist")},
	})
}

// Cannot open parent of first missing directory
func (s *utilsSuite) TestSecureMkdirAllOpenParentOfFirstMissingDirError(c *C) {
	s.sys.InsertFault(`lstat "/dir"`, os.ErrNotExist)
	s.sys.InsertOsLstatResult(`lstat "/"`, testutil.FileInfoDir)
	s.sys.InsertFault(`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, errTesting)
	c.Assert(update.MkdirAllWithin("/dir", "/", 0755, 123, 456, nil), ErrorMatches, `cannot open directory "/": testing`)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/dir"`, E: os.ErrNotExist},
		{C: `lstat "/"`, R: testutil.FileInfoDir},
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, E: errTesting},
	})
}

// Cannot create first missing dir
func (s *utilsSuite) TestSecureMkdirAllCreateFirstMissingDirError(c *C) {
	s.sys.InsertFault(`lstat "/dir"`, os.ErrNotExist)
	s.sys.InsertOsLstatResult(`lstat "/"`, testutil.FileInfoDir)
	s.sys.InsertFault(`mkdirat 3 "dir" 0755`, errTesting)
	c.Assert(update.MkdirAllWithin("/dir", "/", 0755, 123, 456, nil), ErrorMatches, `cannot create directory "/dir": testing`)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/dir"`, E: os.ErrNotExist},
		{C: `lstat "/"`, R: testutil.FileInfoDir},
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "dir" 0755`, E: errTesting},
		{C: `close 3`},
	})
}

// Cannot create second missing dir
func (s *utilsSuite) TestSecureMkdirAllCreateSecondMissingDirError(c *C) {
	s.sys.InsertFault(`lstat "/parent1/parent2/dir"`, os.ErrNotExist)
	s.sys.InsertOsLstatResult(`lstat "/"`, testutil.FileInfoDir)
	s.sys.InsertFault(`lstat "/parent1"`, os.ErrNotExist)
	s.sys.InsertFault(`lstat "/parent1/parent2"`, os.ErrNotExist)
	s.sys.InsertFault(`mkdirat 4 "parent2" 0755`, errTesting)
	c.Assert(update.MkdirAllWithin("/parent1/parent2/dir", "/", 0755, 123, 456, nil), ErrorMatches, `cannot create directory \"/parent1/parent2\": testing`)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/parent1/parent2/dir"`, E: os.ErrNotExist},
		{C: `lstat "/"`, R: testutil.FileInfoDir},
		{C: `lstat "/parent1"`, E: os.ErrNotExist},
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "parent1" 0755`},
		{C: `openat 3 "parent1" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `fchown 4 123 456`},
		{C: `mkdirat 4 "parent2" 0755`, E: errTesting},
		{C: `close 4`},
		{C: `close 3`},
	})
}

// Ensure that we can create a directory in the top-level directory.
func (s *utilsSuite) TestSecureMkdirAllWithinLevel1(c *C) {
	s.sys.InsertFault(`lstat "/dir"`, os.ErrNotExist)
	s.sys.InsertOsLstatResult(`lstat "/"`, testutil.FileInfoDir)
	c.Assert(update.MkdirAllWithin("/dir", "/", 0755, 123, 456, nil), IsNil)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/dir"`, E: os.ErrNotExist},
		{C: `lstat "/"`, R: testutil.FileInfoDir},
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "dir" 0755`},
		{C: `openat 3 "dir" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `fchown 4 123 456`},
		{C: `close 4`},
		{C: `close 3`},
	})
}

// Ensure that we can create a directory two levels from the top-level directory.
func (s *utilsSuite) TestSecureMkdirAllWithinLevel2(c *C) {
	s.sys.InsertFault(`lstat "/dir1/dir2"`, os.ErrNotExist)
	s.sys.InsertFault(`lstat "/dir1"`, os.ErrNotExist)
	s.sys.InsertOsLstatResult(`lstat "/"`, testutil.FileInfoDir)
	c.Assert(update.MkdirAllWithin("/dir1/dir2", "/", 0755, 123, 456, nil), IsNil)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/dir1/dir2"`, E: os.ErrNotExist},
		{C: `lstat "/"`, R: testutil.FileInfoDir},
		{C: `lstat "/dir1"`, E: os.ErrNotExist},
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "dir1" 0755`},
		{C: `openat 3 "dir1" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `fchown 4 123 456`},
		{C: `mkdirat 4 "dir2" 0755`},
		{C: `openat 4 "dir2" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 5},
		{C: `fchown 5 123 456`},
		{C: `close 5`},
		{C: `close 4`},
		{C: `close 3`},
	})
}

// Ensure that we can create a directory one level from the one level parent directory.
func (s *utilsSuite) TestSecureMkdirAllWithinLevel1Parent1(c *C) {
	s.sys.InsertFault(`lstat "/dir1/dir2"`, os.ErrNotExist)
	s.sys.InsertOsLstatResult(`lstat "/dir1"`, testutil.FileInfoDir)
	c.Assert(update.MkdirAllWithin("/dir1/dir2", "/dir1", 0755, 123, 456, nil), IsNil)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/dir1/dir2"`, E: os.ErrNotExist},
		{C: `lstat "/dir1"`, R: testutil.FileInfoDir},
		{C: `open "/dir1" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "dir2" 0755`},
		{C: `openat 3 "dir2" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `fchown 4 123 456`},
		{C: `close 4`},
		{C: `close 3`},
	})
}

// Ensure that we can create a directory three levels from the top-level directory.
func (s *utilsSuite) TestSecureMkdirAllWithinLevel3(c *C) {
	s.sys.InsertFault(`lstat "/dir1/dir2/dir3"`, os.ErrNotExist)
	s.sys.InsertFault(`lstat "/dir1/dir2"`, os.ErrNotExist)
	s.sys.InsertFault(`lstat "/dir1"`, os.ErrNotExist)
	s.sys.InsertOsLstatResult(`lstat "/"`, testutil.FileInfoDir)
	c.Assert(update.MkdirAllWithin("/dir1/dir2/dir3", "/", 0755, 123, 456, nil), IsNil)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/dir1/dir2/dir3"`, E: os.ErrNotExist},
		{C: `lstat "/"`, R: testutil.FileInfoDir},
		{C: `lstat "/dir1"`, E: os.ErrNotExist},
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "dir1" 0755`},
		{C: `openat 3 "dir1" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `fchown 4 123 456`},
		{C: `mkdirat 4 "dir2" 0755`},
		{C: `openat 4 "dir2" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 5},
		{C: `fchown 5 123 456`},
		{C: `mkdirat 5 "dir3" 0755`},
		{C: `openat 5 "dir3" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 6},
		{C: `fchown 6 123 456`},
		{C: `close 6`},
		{C: `close 5`},
		{C: `close 4`},
		{C: `close 3`},
	})
}

// Ensure that we can create a directory one level from the two level parent directory.
func (s *utilsSuite) TestSecureMkdirAllWithinLevel3Parent2(c *C) {
	s.sys.InsertFault(`lstat "/dir1/dir2/dir3"`, os.ErrNotExist)
	s.sys.InsertOsLstatResult(`lstat "/dir1/dir2"`, testutil.FileInfoDir)
	c.Assert(update.MkdirAllWithin("/dir1/dir2/dir3", "/dir1/dir2", 0755, 123, 456, nil), IsNil)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/dir1/dir2/dir3"`, E: os.ErrNotExist},
		{C: `lstat "/dir1/dir2"`, R: testutil.FileInfoDir},
		{C: `open "/dir1/dir2" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "dir3" 0755`},
		{C: `openat 3 "dir3" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `fchown 4 123 456`},
		{C: `close 4`},
		{C: `close 3`},
	})
}

// Ensure that we can create a directory two levels from the two level parent directory.
func (s *utilsSuite) TestSecureMkdirAllWithinLevel4Parent2(c *C) {
	s.sys.InsertFault(`lstat "/dir1/dir2/dir3/dir4"`, os.ErrNotExist)
	s.sys.InsertFault(`lstat "/dir1/dir2/dir3"`, os.ErrNotExist)
	s.sys.InsertOsLstatResult(`lstat "/dir1/dir2"`, testutil.FileInfoDir)
	c.Assert(update.MkdirAllWithin("/dir1/dir2/dir3/dir4", "/dir1/dir2", 0755, 123, 456, nil), IsNil)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/dir1/dir2/dir3/dir4"`, E: os.ErrNotExist},
		{C: `lstat "/dir1/dir2"`, R: testutil.FileInfoDir},
		{C: `lstat "/dir1/dir2/dir3"`, E: os.ErrNotExist},
		{C: `open "/dir1/dir2" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "dir3" 0755`},
		{C: `openat 3 "dir3" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `fchown 4 123 456`},
		{C: `mkdirat 4 "dir4" 0755`},
		{C: `openat 4 "dir4" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 5},
		{C: `fchown 5 123 456`},
		{C: `close 5`},
		{C: `close 4`},
		{C: `close 3`},
	})
}

// Ensure that we can create a directory two levels from the two level parent directory with level three existing
func (s *utilsSuite) TestSecureMkdirAllWithinLevel4Parent2Level3Exists(c *C) {
	s.sys.InsertFault(`lstat "/dir1/dir2/dir3/dir4"`, os.ErrNotExist)
	s.sys.InsertOsLstatResult(`lstat "/dir1/dir2/dir3"`, testutil.FileInfoDir)
	s.sys.InsertOsLstatResult(`lstat "/dir1/dir2"`, testutil.FileInfoDir)
	c.Assert(update.MkdirAllWithin("/dir1/dir2/dir3/dir4", "/dir1/dir2", 0755, 123, 456, nil), IsNil)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/dir1/dir2/dir3/dir4"`, E: os.ErrNotExist},
		{C: `lstat "/dir1/dir2"`, R: testutil.FileInfoDir},
		{C: `lstat "/dir1/dir2/dir3"`, R: testutil.FileInfoDir},
		{C: `open "/dir1/dir2/dir3" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "dir4" 0755`},
		{C: `openat 3 "dir4" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `fchown 4 123 456`},
		{C: `close 4`},
		{C: `close 3`},
	})
}

// Ensure that writes to /etc/demo are interrupted if /etc is restricted.
func (s *utilsSuite) TestSecureMkdirAllWithinRestrictedEtc(c *C) {
	s.sys.InsertFstatfsResult(`fstatfs 3 <ptr>`, syscall.Statfs_t{Type: update.Ext4Magic})
	s.sys.InsertFstatResult(`fstat 3 <ptr>`, syscall.Stat_t{})
	rs := s.as.RestrictionsFor("/etc/demo")
	s.sys.InsertFault(`lstat "/etc/demo"`, os.ErrNotExist)
	s.sys.InsertOsLstatResult(`lstat "/"`, testutil.FileInfoDir)
	s.sys.InsertOsLstatResult(`lstat "/etc"`, testutil.FileInfoDir)
	err := update.MkdirAllWithin("/etc/demo", "/", 0755, 123, 456, rs)
	c.Assert(err, ErrorMatches, `cannot write to "/etc/demo" because it would affect the host in "/etc"`)
	c.Assert(err.(*update.TrespassingError).ViolatedPath, Equals, "/etc")
	c.Assert(err.(*update.TrespassingError).DesiredPath, Equals, "/etc/demo")
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/etc/demo"`, E: os.ErrNotExist},
		{C: `lstat "/"`, R: testutil.FileInfoDir},
		// Skip over inspecting /etc because it exists.
		{C: `lstat "/etc"`, R: testutil.FileInfoDir},
		{C: `open "/etc" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		// /etc/demo is ext4 which is writable, refuse further operations.
		{C: `fstatfs 3 <ptr>`, R: syscall.Statfs_t{Type: update.Ext4Magic}},
		{C: `fstat 3 <ptr>`, R: syscall.Stat_t{}},
		{C: `close 3`},
	})
}

// Ensure that writes to /etc/demo allowed if /etc is unrestricted.
func (s *utilsSuite) TestSecureMkdirAllWithinUnrestrictedEtc(c *C) {
	defer s.as.MockUnrestrictedPaths("/etc")()
	rs := s.as.RestrictionsFor("/etc/demo")
	s.sys.InsertFault(`lstat "/etc/demo"`, os.ErrNotExist)
	s.sys.InsertOsLstatResult(`lstat "/"`, testutil.FileInfoDir)
	s.sys.InsertOsLstatResult(`lstat "/etc"`, testutil.FileInfoDir)
	c.Assert(update.MkdirAllWithin("/etc/demo", "/", 0755, 123, 456, rs), IsNil)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/etc/demo"`, E: os.ErrNotExist},
		{C: `lstat "/"`, R: testutil.FileInfoDir},
		// Skip over inspecting /etc because it exists.
		{C: `lstat "/etc"`, R: testutil.FileInfoDir},
		{C: `open "/etc" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		// No inspection required parent /etc is unrestricted.
		{C: `mkdirat 3 "demo" 0755`},
		{C: `openat 3 "demo" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `fchown 4 123 456`},
		{C: `close 4`},
		{C: `close 3`},
	})
}

// secure-mkdir-all

// Ensure that we reject unclean paths.
func (s *utilsSuite) TestSecureMkdirAllUnclean(c *C) {
	err := update.MkdirAll("/unclean//path", 0755, 123, 456, nil)
	c.Assert(err, ErrorMatches, `cannot split unclean path .*`)
	c.Assert(s.sys.RCalls(), HasLen, 0)
}

// Ensure that we refuse to create a directory with an relative path.
func (s *utilsSuite) TestSecureMkdirAllRelative(c *C) {
	err := update.MkdirAll("rel/path", 0755, 123, 456, nil)
	c.Assert(err, ErrorMatches, `cannot create directory with relative path: "rel/path"`)
	c.Assert(s.sys.RCalls(), HasLen, 0)
}

// Ensure that we can create the root directory.
func (s *utilsSuite) TestSecureMkdirAllLevel0(c *C) {
	c.Assert(update.MkdirAll("/", 0755, 123, 456, nil), IsNil)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `close 3`},
	})
}

// Ensure that we can create a directory in the top-level directory.
func (s *utilsSuite) TestSecureMkdirAllLevel1(c *C) {
	c.Assert(update.MkdirAll("/path", 0755, 123, 456, nil), IsNil)
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
func (s *utilsSuite) TestSecureMkdirAllLevel2(c *C) {
	c.Assert(update.MkdirAll("/path/to", 0755, 123, 456, nil), IsNil)
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
func (s *utilsSuite) TestSecureMkdirAllLevel3(c *C) {
	c.Assert(update.MkdirAll("/path/to/something", 0755, 123, 456, nil), IsNil)
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

// Ensure that we are not masking out the sticky bit when creating directories
func (s *utilsSuite) TestSecureMkdirAllAllowsStickyBit(c *C) {
	s.sys.InsertFault(`mkdirat 3 "dev" 01777`, syscall.EEXIST)
	s.sys.InsertFault(`mkdirat 4 "shm" 01777`, syscall.EEXIST)
	c.Assert(update.MkdirAll("/dev/shm/snap.foo", 0777|os.ModeSticky, 0, 0, nil), IsNil)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "dev" 01777`, E: syscall.EEXIST},
		{C: `openat 3 "dev" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `mkdirat 4 "shm" 01777`, E: syscall.EEXIST},
		{C: `openat 4 "shm" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 5},
		{C: `close 4`},
		{C: `close 3`},
		{C: `mkdirat 5 "snap.foo" 01777`},
		{C: `openat 5 "snap.foo" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `fchown 3 0 0`},
		{C: `close 3`},
		{C: `close 5`},
	})
}

// Ensure that trespassing for prefix is matched using clean base path.
func (s *utilsSuite) TestTrespassingMatcher(c *C) {
	// We mounted tmpfs at "/path".
	s.as.AddChange(&update.Change{Action: update.Mount, Entry: osutil.MountEntry{Dir: "/path", Type: "tmpfs", Name: "tmpfs"}})
	s.sys.InsertFstatfsResult(`fstatfs 3 <ptr>`, syscall.Statfs_t{Type: update.SquashfsMagic})
	s.sys.InsertFstatResult(`fstat 3 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFstatfsResult(`fstatfs 4 <ptr>`, syscall.Statfs_t{Type: update.TmpfsMagic})
	s.sys.InsertFstatResult(`fstat 4 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFault(`mkdirat 3 "path" 0755`, syscall.EEXIST)
	rs := s.as.RestrictionsFor("/path/to/something")
	// Trespassing detector checked "/path", not "/path/" (which would not match).
	c.Assert(update.MkdirAll("/path/to/something", 0755, 123, 456, rs), IsNil)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `fstatfs 3 <ptr>`, R: syscall.Statfs_t{Type: update.SquashfsMagic}},
		{C: `fstat 3 <ptr>`, R: syscall.Stat_t{}},
		{C: `mkdirat 3 "path" 0755`, E: syscall.EEXIST},
		{C: `openat 3 "path" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `fstatfs 4 <ptr>`, R: syscall.Statfs_t{Type: update.TmpfsMagic}},
		{C: `fstat 4 <ptr>`, R: syscall.Stat_t{}},

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

// Ensure that writes to /etc/demo are interrupted if /etc is restricted.
func (s *utilsSuite) TestSecureMkdirAllWithRestrictedEtc(c *C) {
	s.sys.InsertFstatfsResult(`fstatfs 3 <ptr>`, syscall.Statfs_t{Type: update.SquashfsMagic})
	s.sys.InsertFstatResult(`fstat 3 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFstatfsResult(`fstatfs 4 <ptr>`, syscall.Statfs_t{Type: update.Ext4Magic})
	s.sys.InsertFstatResult(`fstat 4 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFault(`mkdirat 3 "etc" 0755`, syscall.EEXIST)
	rs := s.as.RestrictionsFor("/etc/demo")
	err := update.MkdirAll("/etc/demo", 0755, 123, 456, rs)
	c.Assert(err, ErrorMatches, `cannot write to "/etc/demo" because it would affect the host in "/etc"`)
	c.Assert(err.(*update.TrespassingError).ViolatedPath, Equals, "/etc")
	c.Assert(err.(*update.TrespassingError).DesiredPath, Equals, "/etc/demo")
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		// we are inspecting the type of the filesystem we are about to perform operation on.
		{C: `fstatfs 3 <ptr>`, R: syscall.Statfs_t{Type: update.SquashfsMagic}},
		{C: `fstat 3 <ptr>`, R: syscall.Stat_t{}},
		{C: `mkdirat 3 "etc" 0755`, E: syscall.EEXIST},
		{C: `openat 3 "etc" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `close 3`},
		// ext4 is writable, refuse further operations.
		{C: `fstatfs 4 <ptr>`, R: syscall.Statfs_t{Type: update.Ext4Magic}},
		{C: `fstat 4 <ptr>`, R: syscall.Stat_t{}},
		{C: `close 4`},
	})
}

// Ensure that writes to /etc/demo allowed if /etc is unrestricted.
func (s *utilsSuite) TestSecureMkdirAllWithUnrestrictedEtc(c *C) {
	defer s.as.MockUnrestrictedPaths("/etc")() // Mark /etc as unrestricted.
	s.sys.InsertFault(`mkdirat 3 "etc" 0755`, syscall.EEXIST)
	rs := s.as.RestrictionsFor("/etc/demo")
	c.Assert(update.MkdirAll("/etc/demo", 0755, 123, 456, rs), IsNil)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		// We are not interested in the type of filesystem at /
		{C: `mkdirat 3 "etc" 0755`, E: syscall.EEXIST},
		{C: `openat 3 "etc" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		// We are not interested in the type of filesystem at /etc
		{C: `close 3`},
		{C: `mkdirat 4 "demo" 0755`},
		{C: `openat 4 "demo" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `fchown 3 123 456`},
		{C: `close 3`},
		{C: `close 4`},
	})
}

// Ensure that we can detect read only filesystems.
func (s *utilsSuite) TestSecureMkdirAllROFS(c *C) {
	s.sys.InsertFault(`mkdirat 3 "rofs" 0755`, syscall.EEXIST) // just realistic
	s.sys.InsertFault(`mkdirat 4 "path" 0755`, syscall.EROFS)
	err := update.MkdirAll("/rofs/path", 0755, 123, 456, nil)
	c.Assert(err, ErrorMatches, `cannot operate on read-only filesystem at /rofs`)
	c.Assert(err.(*update.ReadOnlyFsError).Path, Equals, "/rofs")
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
func (s *utilsSuite) TestSecureMkdirAllExistingDirsDontChown(c *C) {
	s.sys.InsertFault(`mkdirat 3 "abs" 0755`, syscall.EEXIST)
	s.sys.InsertFault(`mkdirat 4 "path" 0755`, syscall.EEXIST)
	err := update.MkdirAll("/abs/path", 0755, 123, 456, nil)
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
func (s *utilsSuite) TestSecureMkdirAllMkdiratError(c *C) {
	s.sys.InsertFault(`mkdirat 3 "abs" 0755`, errTesting)
	err := update.MkdirAll("/abs", 0755, 123, 456, nil)
	c.Assert(err, ErrorMatches, `cannot create directory "/abs": testing`)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "abs" 0755`, E: errTesting},
		{C: `close 3`},
	})
}

// Ensure that we we close everything when fchown fails.
func (s *utilsSuite) TestSecureMkdirAllFchownError(c *C) {
	s.sys.InsertFault(`fchown 4 123 456`, errTesting)
	err := update.MkdirAll("/path", 0755, 123, 456, nil)
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
func (s *utilsSuite) TestSecureMkdirAllOpenRootError(c *C) {
	s.sys.InsertFault(`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, errTesting)
	err := update.MkdirAll("/abs/path", 0755, 123, 456, nil)
	c.Assert(err, ErrorMatches, "cannot open root directory: testing")
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, E: errTesting},
	})
}

// Check error path when we cannot open non-root directory.
func (s *utilsSuite) TestSecureMkdirAllOpenError(c *C) {
	s.sys.InsertFault(`openat 3 "abs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, errTesting)
	err := update.MkdirAll("/abs/path", 0755, 123, 456, nil)
	c.Assert(err, ErrorMatches, `cannot open directory "/abs": testing`)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "abs" 0755`},
		{C: `openat 3 "abs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, E: errTesting},
		{C: `close 3`},
	})
}

func (s *utilsSuite) TestPlanWritableMimic(c *C) {
	s.sys.InsertSysLstatResult(`lstat "/foo" <ptr>`, syscall.Stat_t{Uid: 0, Gid: 0, Mode: 0755})
	restore := update.MockReadDir(func(dir string) ([]os.FileInfo, error) {
		c.Assert(dir, Equals, "/foo")
		return []os.FileInfo{
			testutil.FakeFileInfo("file", 0),
			testutil.FakeFileInfo("dir", os.ModeDir),
			testutil.FakeFileInfo("symlink", os.ModeSymlink),
			testutil.FakeFileInfo("error-symlink-readlink", os.ModeSymlink),
			// NOTE: None of the filesystem entries below are supported because
			// they cannot be placed inside snaps or can only be created at
			// runtime in areas that are already writable and this would never
			// have to be handled in a writable mimic.
			testutil.FakeFileInfo("block-dev", os.ModeDevice),
			testutil.FakeFileInfo("char-dev", os.ModeDevice|os.ModeCharDevice),
			testutil.FakeFileInfo("socket", os.ModeSocket),
			testutil.FakeFileInfo("pipe", os.ModeNamedPipe),
		}, nil
	})
	defer restore()
	restore = update.MockReadlink(func(name string) (string, error) {
		switch name {
		case "/foo/symlink":
			return "target", nil
		case "/foo/error-symlink-readlink":
			return "", errTesting
		}
		panic("unexpected")
	})
	defer restore()

	changes, err := update.PlanWritableMimic("/foo", "/foo/bar")
	c.Assert(err, IsNil)

	c.Assert(changes, DeepEquals, []*update.Change{
		// Store /foo in /tmp/.snap/foo while we set things up
		{Entry: osutil.MountEntry{Name: "/foo", Dir: "/tmp/.snap/foo", Options: []string{"rbind"}}, Action: update.Mount},
		// Put a tmpfs over /foo
		{Entry: osutil.MountEntry{Name: "tmpfs", Dir: "/foo", Type: "tmpfs", Options: []string{"x-snapd.synthetic", "x-snapd.needed-by=/foo/bar", "mode=0755", "uid=0", "gid=0"}}, Action: update.Mount},
		// Bind mount files and directories over. Note that files are identified by x-snapd.kind=file option.
		{Entry: osutil.MountEntry{Name: "/tmp/.snap/foo/file", Dir: "/foo/file", Options: []string{"bind", "x-snapd.kind=file", "x-snapd.synthetic", "x-snapd.needed-by=/foo/bar"}}, Action: update.Mount},
		{Entry: osutil.MountEntry{Name: "/tmp/.snap/foo/dir", Dir: "/foo/dir", Options: []string{"rbind", "x-snapd.synthetic", "x-snapd.needed-by=/foo/bar"}}, Action: update.Mount},
		// Create symlinks.
		// Bad symlinks and all other file types are skipped and not
		// recorded in mount changes.
		{Entry: osutil.MountEntry{Name: "/tmp/.snap/foo/symlink", Dir: "/foo/symlink", Options: []string{"x-snapd.kind=symlink", "x-snapd.symlink=target", "x-snapd.synthetic", "x-snapd.needed-by=/foo/bar"}}, Action: update.Mount},
		// Unmount the safe-keeping directory
		{Entry: osutil.MountEntry{Name: "none", Dir: "/tmp/.snap/foo", Options: []string{"x-snapd.detach"}}, Action: update.Unmount},
	})
}

func (s *utilsSuite) TestPlanWritableMimicErrors(c *C) {
	s.sys.InsertSysLstatResult(`lstat "/foo" <ptr>`, syscall.Stat_t{Uid: 0, Gid: 0, Mode: 0755})
	restore := update.MockReadDir(func(dir string) ([]os.FileInfo, error) {
		c.Assert(dir, Equals, "/foo")
		return nil, errTesting
	})
	defer restore()
	restore = update.MockReadlink(func(name string) (string, error) {
		return "", errTesting
	})
	defer restore()

	changes, err := update.PlanWritableMimic("/foo", "/foo/bar")
	c.Assert(err, ErrorMatches, "testing")
	c.Assert(changes, HasLen, 0)
}

func (s *utilsSuite) TestExecWirableMimicSuccess(c *C) {
	// This plan is the same as in the test above. This is what comes out of planWritableMimic.
	plan := []*update.Change{
		{Entry: osutil.MountEntry{Name: "/foo", Dir: "/tmp/.snap/foo", Options: []string{"rbind"}}, Action: update.Mount},
		{Entry: osutil.MountEntry{Name: "tmpfs", Dir: "/foo", Type: "tmpfs", Options: []string{"x-snapd.synthetic", "x-snapd.needed-by=/foo/bar"}}, Action: update.Mount},
		{Entry: osutil.MountEntry{Name: "/tmp/.snap/foo/file", Dir: "/foo/file", Options: []string{"bind", "x-snapd.kind=file", "x-snapd.synthetic", "x-snapd.needed-by=/foo/bar"}}, Action: update.Mount},
		{Entry: osutil.MountEntry{Name: "/tmp/.snap/foo/dir", Dir: "/foo/dir", Options: []string{"rbind", "x-snapd.synthetic", "x-snapd.needed-by=/foo/bar"}}, Action: update.Mount},
		{Entry: osutil.MountEntry{Name: "/tmp/.snap/foo/symlink", Dir: "/foo/symlink", Options: []string{"x-snapd.kind=symlink", "x-snapd.symlink=target", "x-snapd.synthetic", "x-snapd.needed-by=/foo/bar"}}, Action: update.Mount},
		{Entry: osutil.MountEntry{Name: "none", Dir: "/tmp/.snap/foo", Options: []string{"x-snapd.detach"}}, Action: update.Unmount},
	}

	// Mock the act of performing changes, each of the change we perform is coming from the plan.
	restore := update.MockChangePerform(func(chg *update.Change, as *update.Assumptions) ([]*update.Change, error) {
		c.Assert(plan, testutil.DeepContains, chg)
		return nil, nil
	})
	defer restore()

	// The executed plan leaves us with a simplified view of the plan that is suitable for undo.
	undoPlan, err := update.ExecWritableMimic(plan, s.as)
	c.Assert(err, IsNil)
	c.Assert(undoPlan, DeepEquals, []*update.Change{
		{Entry: osutil.MountEntry{Name: "tmpfs", Dir: "/foo", Type: "tmpfs", Options: []string{"x-snapd.synthetic", "x-snapd.needed-by=/foo/bar"}}, Action: update.Mount},
		{Entry: osutil.MountEntry{Name: "/foo/file", Dir: "/foo/file", Options: []string{"bind", "x-snapd.kind=file", "x-snapd.synthetic", "x-snapd.needed-by=/foo/bar"}}, Action: update.Mount},
		{Entry: osutil.MountEntry{Name: "/foo/dir", Dir: "/foo/dir", Options: []string{"rbind", "x-snapd.synthetic", "x-snapd.needed-by=/foo/bar", "x-snapd.detach"}}, Action: update.Mount},
	})
}

func (s *utilsSuite) TestExecWirableMimicErrorWithRecovery(c *C) {
	// This plan is the same as in the test above. This is what comes out of planWritableMimic.
	plan := []*update.Change{
		{Entry: osutil.MountEntry{Name: "/foo", Dir: "/tmp/.snap/foo", Options: []string{"rbind"}}, Action: update.Mount},
		{Entry: osutil.MountEntry{Name: "tmpfs", Dir: "/foo", Type: "tmpfs", Options: []string{"x-snapd.synthetic", "x-snapd.needed-by=/foo/bar"}}, Action: update.Mount},
		{Entry: osutil.MountEntry{Name: "/tmp/.snap/foo/file", Dir: "/foo/file", Options: []string{"bind", "x-snapd.kind=file", "x-snapd.synthetic", "x-snapd.needed-by=/foo/bar"}}, Action: update.Mount},
		{Entry: osutil.MountEntry{Name: "/tmp/.snap/foo/symlink", Dir: "/foo/symlink", Options: []string{"x-snapd.kind=symlink", "x-snapd.symlink=target", "x-snapd.synthetic", "x-snapd.needed-by=/foo/bar"}}, Action: update.Mount},
		// NOTE: the next perform will fail. Notably the symlink did not fail.
		{Entry: osutil.MountEntry{Name: "/tmp/.snap/foo/dir", Dir: "/foo/dir", Options: []string{"rbind"}}, Action: update.Mount},
		{Entry: osutil.MountEntry{Name: "none", Dir: "/tmp/.snap/foo", Options: []string{"x-snapd.detach"}}, Action: update.Unmount},
	}

	// Mock the act of performing changes. Before we inject a failure we ensure
	// that each of the change we perform is coming from the plan. For the
	// purpose of the test the change that bind mounts the "dir" over itself
	// will fail and will trigger an recovery path. The changes performed in
	// the recovery path are recorded.
	var recoveryPlan []*update.Change
	recovery := false
	restore := update.MockChangePerform(func(chg *update.Change, as *update.Assumptions) ([]*update.Change, error) {
		if !recovery {
			c.Assert(plan, testutil.DeepContains, chg)
			if chg.Entry.Name == "/tmp/.snap/foo/dir" {
				recovery = true // switch to recovery mode
				return nil, errTesting
			}
		} else {
			recoveryPlan = append(recoveryPlan, chg)
		}
		return nil, nil
	})
	defer restore()

	// The executed plan fails, leaving us with the error and an empty undo plan.
	undoPlan, err := update.ExecWritableMimic(plan, s.as)
	c.Assert(err, Equals, errTesting)
	c.Assert(undoPlan, HasLen, 0)
	// The changes we managed to perform were undone correctly.
	c.Assert(recoveryPlan, DeepEquals, []*update.Change{
		// NOTE: there is no symlink undo entry as it is implicitly undone by unmounting the tmpfs.
		{Entry: osutil.MountEntry{Name: "/foo/file", Dir: "/foo/file", Options: []string{"bind", "x-snapd.kind=file", "x-snapd.synthetic", "x-snapd.needed-by=/foo/bar"}}, Action: update.Unmount},
		{Entry: osutil.MountEntry{Name: "tmpfs", Dir: "/foo", Type: "tmpfs", Options: []string{"x-snapd.synthetic", "x-snapd.needed-by=/foo/bar"}}, Action: update.Unmount},
		{Entry: osutil.MountEntry{Name: "/foo", Dir: "/tmp/.snap/foo", Options: []string{"rbind", "x-snapd.detach"}}, Action: update.Unmount},
	})
}

func (s *utilsSuite) TestExecWirableMimicErrorNothingDone(c *C) {
	// This plan is the same as in the test above. This is what comes out of planWritableMimic.
	plan := []*update.Change{
		{Entry: osutil.MountEntry{Name: "/foo", Dir: "/tmp/.snap/foo", Options: []string{"rbind"}}, Action: update.Mount},
		{Entry: osutil.MountEntry{Name: "tmpfs", Dir: "/foo", Type: "tmpfs", Options: []string{"x-snapd.synthetic", "x-snapd.needed-by=/foo/bar"}}, Action: update.Mount},
		{Entry: osutil.MountEntry{Name: "/tmp/.snap/foo/file", Dir: "/foo/file", Options: []string{"bind", "x-snapd.kind=file", "x-snapd.synthetic", "x-snapd.needed-by=/foo/bar"}}, Action: update.Mount},
		{Entry: osutil.MountEntry{Name: "/tmp/.snap/foo/dir", Dir: "/foo/dir", Options: []string{"rbind", "x-snapd.synthetic", "x-snapd.needed-by=/foo/bar"}}, Action: update.Mount},
		{Entry: osutil.MountEntry{Name: "/tmp/.snap/foo/symlink", Dir: "/foo/symlink", Options: []string{"x-snapd.kind=symlink", "x-snapd.symlink=target", "x-snapd.synthetic", "x-snapd.needed-by=/foo/bar"}}, Action: update.Mount},
		{Entry: osutil.MountEntry{Name: "none", Dir: "/tmp/.snap/foo", Options: []string{"x-snapd.detach"}}, Action: update.Unmount},
	}

	// Mock the act of performing changes and just fail on any request.
	restore := update.MockChangePerform(func(chg *update.Change, as *update.Assumptions) ([]*update.Change, error) {
		return nil, errTesting
	})
	defer restore()

	// The executed plan fails, the recovery didn't fail (it's empty) so we just return that error.
	undoPlan, err := update.ExecWritableMimic(plan, s.as)
	c.Assert(err, Equals, errTesting)
	c.Assert(undoPlan, HasLen, 0)
}

func (s *utilsSuite) TestExecWirableMimicErrorCannotUndo(c *C) {
	// This plan is the same as in the test above. This is what comes out of planWritableMimic.
	plan := []*update.Change{
		{Entry: osutil.MountEntry{Name: "/foo", Dir: "/tmp/.snap/foo", Options: []string{"rbind"}}, Action: update.Mount},
		{Entry: osutil.MountEntry{Name: "tmpfs", Dir: "/foo", Type: "tmpfs", Options: []string{"x-snapd.synthetic", "x-snapd.needed-by=/foo/bar"}}, Action: update.Mount},
		{Entry: osutil.MountEntry{Name: "/tmp/.snap/foo/file", Dir: "/foo/file", Options: []string{"bind", "x-snapd.kind=file", "x-snapd.synthetic", "x-snapd.needed-by=/foo/bar"}}, Action: update.Mount},
		{Entry: osutil.MountEntry{Name: "/tmp/.snap/foo/dir", Dir: "/foo/dir", Options: []string{"rbind", "x-snapd.synthetic", "x-snapd.needed-by=/foo/bar"}}, Action: update.Mount},
		{Entry: osutil.MountEntry{Name: "/tmp/.snap/foo/symlink", Dir: "/foo/symlink", Options: []string{"x-snapd.kind=symlink", "x-snapd.symlink=target", "x-snapd.synthetic", "x-snapd.needed-by=/foo/bar"}}, Action: update.Mount},
		{Entry: osutil.MountEntry{Name: "none", Dir: "/tmp/.snap/foo", Options: []string{"x-snapd.detach"}}, Action: update.Unmount},
	}

	// Mock the act of performing changes. After performing the first change
	// correctly we will fail forever (this includes the recovery path) so the
	// execute function ends up in a situation where it cannot perform the
	// recovery path and will have to return a fatal error.
	i := -1
	restore := update.MockChangePerform(func(chg *update.Change, as *update.Assumptions) ([]*update.Change, error) {
		i++
		if i > 0 {
			return nil, fmt.Errorf("failure-%d", i)
		}
		return nil, nil
	})
	defer restore()

	// The plan partially succeeded and we cannot undo those changes.
	_, err := update.ExecWritableMimic(plan, s.as)
	c.Assert(err, ErrorMatches, `cannot undo change ".*" while recovering from earlier error failure-1: failure-2`)
	c.Assert(err, FitsTypeOf, &update.FatalError{})
}

// realSystemSuite is not isolated / mocked from the system.
type realSystemSuite struct {
	as *update.Assumptions
}

var _ = Suite(&realSystemSuite{})

func (s *realSystemSuite) SetUpTest(c *C) {
	s.as = &update.Assumptions{}
	s.as.AddUnrestrictedPaths("/tmp")
}

// secure-mkdir-all-within

// Check that we can actually create directories.
// This doesn't test the chown logic as that requires root.
func (s *realSystemSuite) TestSecureMkdirAllWithinForReal(c *C) {
	d := c.MkDir()
	// Create d (which already exists) with mode 0777 (but c.MkDir() used 0700
	// internally and since we are not creating the directory we should not be
	// changing that.
	c.Assert(update.MkdirAllWithin(d, "/tmp", 0777, sys.FlagID, sys.FlagID, nil), IsNil)
	fi, err := os.Stat(d)
	c.Assert(err, IsNil)
	c.Check(fi.IsDir(), Equals, true)
	c.Check(fi.Mode().Perm(), Equals, os.FileMode(0700))

	// Create d1, which is a simple subdirectory, with a distinct mode and
	// check that it was applied. Note that default umask 022 is subtracted so
	// effective directory has different permissions.
	d1 := filepath.Join(d, "subdir")
	c.Assert(update.MkdirAllWithin(d1, "/tmp", 0707, sys.FlagID, sys.FlagID, nil), IsNil)
	fi, err = os.Stat(d1)
	c.Assert(err, IsNil)
	c.Check(fi.IsDir(), Equals, true)
	c.Check(fi.Mode().Perm(), Equals, os.FileMode(0705))

	// Create d2, which is a deeper subdirectory, with another distinct mode
	// and check that it was applied.
	d2 := filepath.Join(d, "subdir/subdir/subdir")
	c.Assert(update.MkdirAllWithin(d2, "/tmp", 0750, sys.FlagID, sys.FlagID, nil), IsNil)
	fi, err = os.Stat(d1)
	c.Assert(err, IsNil)
	c.Check(fi.IsDir(), Equals, true)
	c.Check(fi.Mode().Perm(), Equals, os.FileMode(0705))
	fi, err = os.Stat(d2)
	c.Assert(err, IsNil)
	c.Check(fi.IsDir(), Equals, true)
	c.Check(fi.Mode().Perm(), Equals, os.FileMode(0750))
}

// secure-mkdir-all

// Check that we can actually create directories.
// This doesn't test the chown logic as that requires root.
func (s *realSystemSuite) TestSecureMkdirAllForReal(c *C) {
	d := c.MkDir()

	// Create d (which already exists) with mode 0777 (but c.MkDir() used 0700
	// internally and since we are not creating the directory we should not be
	// changing that.
	c.Assert(update.MkdirAll(d, 0777, sys.FlagID, sys.FlagID, nil), IsNil)
	fi, err := os.Stat(d)
	c.Assert(err, IsNil)
	c.Check(fi.IsDir(), Equals, true)
	c.Check(fi.Mode().Perm(), Equals, os.FileMode(0700))

	// Create d1, which is a simple subdirectory, with a distinct mode and
	// check that it was applied. Note that default umask 022 is subtracted so
	// effective directory has different permissions.
	d1 := filepath.Join(d, "subdir")
	c.Assert(update.MkdirAll(d1, 0707, sys.FlagID, sys.FlagID, nil), IsNil)
	fi, err = os.Stat(d1)
	c.Assert(err, IsNil)
	c.Check(fi.IsDir(), Equals, true)
	c.Check(fi.Mode().Perm(), Equals, os.FileMode(0705))

	// Create d2, which is a deeper subdirectory, with another distinct mode
	// and check that it was applied.
	d2 := filepath.Join(d, "subdir/subdir/subdir")
	c.Assert(update.MkdirAll(d2, 0750, sys.FlagID, sys.FlagID, nil), IsNil)
	fi, err = os.Stat(d2)
	c.Assert(err, IsNil)
	c.Check(fi.IsDir(), Equals, true)
	c.Check(fi.Mode().Perm(), Equals, os.FileMode(0750))
}

// secure-mkfile-all

// Ensure that we reject unclean paths.
func (s *utilsSuite) TestSecureMkfileAllUnclean(c *C) {
	err := update.MkfileAll("/unclean//path", 0755, 123, 456, nil)
	c.Assert(err, ErrorMatches, `cannot split unclean path .*`)
	c.Assert(s.sys.RCalls(), HasLen, 0)
}

// Ensure that we refuse to create a file with an relative path.
func (s *utilsSuite) TestSecureMkfileAllRelative(c *C) {
	err := update.MkfileAll("rel/path", 0755, 123, 456, nil)
	c.Assert(err, ErrorMatches, `cannot create file with relative path: "rel/path"`)
	c.Assert(s.sys.RCalls(), HasLen, 0)
}

// Ensure that we refuse creating the root directory as a file.
func (s *utilsSuite) TestSecureMkfileAllLevel0(c *C) {
	err := update.MkfileAll("/", 0755, 123, 456, nil)
	c.Assert(err, ErrorMatches, `cannot create non-file path: "/"`)
	c.Assert(s.sys.RCalls(), HasLen, 0)
}

// Ensure that we can create a file in the top-level directory.
func (s *utilsSuite) TestSecureMkfileAllLevel1(c *C) {
	c.Assert(update.MkfileAll("/path", 0755, 123, 456, nil), IsNil)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `openat 3 "path" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`, R: 4},
		{C: `fchown 4 123 456`},
		{C: `close 4`},
		{C: `close 3`},
	})
}

// Ensure that we can create a file two levels from the top-level directory.
func (s *utilsSuite) TestSecureMkfileAllLevel2(c *C) {
	c.Assert(update.MkfileAll("/path/to", 0755, 123, 456, nil), IsNil)
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
func (s *utilsSuite) TestSecureMkfileAllLevel3(c *C) {
	c.Assert(update.MkfileAll("/path/to/something", 0755, 123, 456, nil), IsNil)
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
func (s *utilsSuite) TestSecureMkfileAllROFS(c *C) {
	s.sys.InsertFault(`mkdirat 3 "rofs" 0755`, syscall.EEXIST) // just realistic
	s.sys.InsertFault(`openat 4 "path" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`, syscall.EROFS)
	err := update.MkfileAll("/rofs/path", 0755, 123, 456, nil)
	c.Check(err, ErrorMatches, `cannot operate on read-only filesystem at /rofs`)
	c.Assert(err.(*update.ReadOnlyFsError).Path, Equals, "/rofs")
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
func (s *utilsSuite) TestSecureMkfileAllExistingDirsDontChown(c *C) {
	s.sys.InsertFault(`mkdirat 3 "abs" 0755`, syscall.EEXIST)
	s.sys.InsertFault(`openat 4 "path" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`, syscall.EEXIST)
	err := update.MkfileAll("/abs/path", 0755, 123, 456, nil)
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
func (s *utilsSuite) TestSecureMkfileAllOpenat2ndError(c *C) {
	s.sys.InsertFault(`openat 3 "abs" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`, syscall.EEXIST)
	s.sys.InsertFault(`openat 3 "abs" O_NOFOLLOW|O_CLOEXEC 0`, errTesting)
	err := update.MkfileAll("/abs", 0755, 123, 456, nil)
	c.Assert(err, ErrorMatches, `cannot open file "/abs": testing`)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `openat 3 "abs" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`, E: syscall.EEXIST},
		{C: `openat 3 "abs" O_NOFOLLOW|O_CLOEXEC 0`, E: errTesting},
		{C: `close 3`},
	})
}

// Ensure that we we close everything when openat (non-exclusive) fails.
func (s *utilsSuite) TestSecureMkfileAllOpenatError(c *C) {
	s.sys.InsertFault(`openat 3 "abs" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`, errTesting)
	err := update.MkfileAll("/abs", 0755, 123, 456, nil)
	c.Assert(err, ErrorMatches, `cannot open file "/abs": testing`)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `openat 3 "abs" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`, E: errTesting},
		{C: `close 3`},
	})
}

// Ensure that we we close everything when fchown fails.
func (s *utilsSuite) TestSecureMkfileAllFchownError(c *C) {
	s.sys.InsertFault(`fchown 4 123 456`, errTesting)
	err := update.MkfileAll("/path", 0755, 123, 456, nil)
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
func (s *utilsSuite) TestSecureMkfileAllOpenRootError(c *C) {
	s.sys.InsertFault(`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, errTesting)
	err := update.MkfileAll("/abs/path", 0755, 123, 456, nil)
	c.Assert(err, ErrorMatches, "cannot open root directory: testing")
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, E: errTesting},
	})
}

// Check error path when we cannot open non-root directory.
func (s *utilsSuite) TestSecureMkfileAllOpenError(c *C) {
	s.sys.InsertFault(`openat 3 "abs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, errTesting)
	err := update.MkfileAll("/abs/path", 0755, 123, 456, nil)
	c.Assert(err, ErrorMatches, `cannot open directory "/abs": testing`)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "abs" 0755`},
		{C: `openat 3 "abs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, E: errTesting},
		{C: `close 3`},
	})
}

// We want to create a symlink in $SNAP_DATA and that's fine.
func (s *utilsSuite) TestSecureMksymlinkAllInSnapData(c *C) {
	s.sys.InsertFault(`mkdirat 3 "var" 0755`, syscall.EEXIST)
	s.sys.InsertFault(`mkdirat 4 "snap" 0755`, syscall.EEXIST)
	s.sys.InsertFault(`mkdirat 5 "foo" 0755`, syscall.EEXIST)
	s.sys.InsertFault(`mkdirat 6 "42" 0755`, syscall.EEXIST)

	err := update.MksymlinkAll("/var/snap/foo/42/symlink", 0755, 0, 0, "/oldname", nil)
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

// We want to create a symlink in /etc but the host filesystem would be affected.
func (s *utilsSuite) TestSecureMksymlinkAllInEtc(c *C) {
	s.sys.InsertFstatfsResult(`fstatfs 3 <ptr>`, syscall.Statfs_t{Type: update.SquashfsMagic})
	s.sys.InsertFstatResult(`fstat 3 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFstatfsResult(`fstatfs 4 <ptr>`, syscall.Statfs_t{Type: update.Ext4Magic})
	s.sys.InsertFstatResult(`fstat 4 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFault(`mkdirat 3 "etc" 0755`, syscall.EEXIST)
	rs := s.as.RestrictionsFor("/etc/symlink")
	err := update.MksymlinkAll("/etc/symlink", 0755, 0, 0, "/oldname", rs)
	c.Assert(err, ErrorMatches, `cannot write to "/etc/symlink" because it would affect the host in "/etc"`)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `fstatfs 3 <ptr>`, R: syscall.Statfs_t{Type: update.SquashfsMagic}},
		{C: `fstat 3 <ptr>`, R: syscall.Stat_t{}},
		{C: `mkdirat 3 "etc" 0755`, E: syscall.EEXIST},
		{C: `openat 3 "etc" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `close 3`},
		{C: `fstatfs 4 <ptr>`, R: syscall.Statfs_t{Type: update.Ext4Magic}},
		{C: `fstat 4 <ptr>`, R: syscall.Stat_t{}},
		{C: `close 4`},
	})
}

// We want to create a symlink deep in /etc but the host filesystem would be affected.
// This just shows that we pick the right place to construct the mimic
func (s *utilsSuite) TestSecureMksymlinkAllDeepInEtc(c *C) {
	s.sys.InsertFstatfsResult(`fstatfs 3 <ptr>`, syscall.Statfs_t{Type: update.SquashfsMagic})
	s.sys.InsertFstatResult(`fstat 3 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFstatfsResult(`fstatfs 4 <ptr>`, syscall.Statfs_t{Type: update.Ext4Magic})
	s.sys.InsertFstatResult(`fstat 4 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFault(`mkdirat 3 "etc" 0755`, syscall.EEXIST)
	rs := s.as.RestrictionsFor("/etc/some/other/stuff/symlink")
	err := update.MksymlinkAll("/etc/some/other/stuff/symlink", 0755, 0, 0, "/oldname", rs)
	c.Assert(err, ErrorMatches, `cannot write to "/etc/some/other/stuff/symlink" because it would affect the host in "/etc"`)
	c.Assert(err.(*update.TrespassingError).ViolatedPath, Equals, "/etc")
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `fstatfs 3 <ptr>`, R: syscall.Statfs_t{Type: update.SquashfsMagic}},
		{C: `fstat 3 <ptr>`, R: syscall.Stat_t{}},
		{C: `mkdirat 3 "etc" 0755`, E: syscall.EEXIST},
		{C: `openat 3 "etc" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `fstatfs 4 <ptr>`, R: syscall.Statfs_t{Type: update.Ext4Magic}},
		{C: `fstat 4 <ptr>`, R: syscall.Stat_t{}},
		{C: `close 4`},
		{C: `close 3`},
	})
}

// We want to create a file in /etc but the host filesystem would be affected.
func (s *utilsSuite) TestSecureMkfileAllInEtc(c *C) {
	s.sys.InsertFstatfsResult(`fstatfs 3 <ptr>`, syscall.Statfs_t{Type: update.SquashfsMagic})
	s.sys.InsertFstatResult(`fstat 3 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFstatfsResult(`fstatfs 4 <ptr>`, syscall.Statfs_t{Type: update.Ext4Magic})
	s.sys.InsertFstatResult(`fstat 4 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFault(`mkdirat 3 "etc" 0755`, syscall.EEXIST)
	rs := s.as.RestrictionsFor("/etc/file")
	err := update.MkfileAll("/etc/file", 0755, 0, 0, rs)
	c.Assert(err, ErrorMatches, `cannot write to "/etc/file" because it would affect the host in "/etc"`)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `fstatfs 3 <ptr>`, R: syscall.Statfs_t{Type: update.SquashfsMagic}},
		{C: `fstat 3 <ptr>`, R: syscall.Stat_t{}},
		{C: `mkdirat 3 "etc" 0755`, E: syscall.EEXIST},
		{C: `openat 3 "etc" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `close 3`},
		{C: `fstatfs 4 <ptr>`, R: syscall.Statfs_t{Type: update.Ext4Magic}},
		{C: `fstat 4 <ptr>`, R: syscall.Stat_t{}},
		{C: `close 4`},
	})
}

// We want to create a directory in /etc but the host filesystem would be affected.
func (s *utilsSuite) TestSecureMkdirAllInEtc(c *C) {
	s.sys.InsertFstatfsResult(`fstatfs 3 <ptr>`, syscall.Statfs_t{Type: update.SquashfsMagic})
	s.sys.InsertFstatResult(`fstat 3 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFstatfsResult(`fstatfs 4 <ptr>`, syscall.Statfs_t{Type: update.Ext4Magic})
	s.sys.InsertFstatResult(`fstat 4 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFault(`mkdirat 3 "etc" 0755`, syscall.EEXIST)
	rs := s.as.RestrictionsFor("/etc/dir")
	err := update.MkdirAll("/etc/dir", 0755, 0, 0, rs)
	c.Assert(err, ErrorMatches, `cannot write to "/etc/dir" because it would affect the host in "/etc"`)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `fstatfs 3 <ptr>`, R: syscall.Statfs_t{Type: update.SquashfsMagic}},
		{C: `fstat 3 <ptr>`, R: syscall.Stat_t{}},
		{C: `mkdirat 3 "etc" 0755`, E: syscall.EEXIST},
		{C: `openat 3 "etc" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `close 3`},
		{C: `fstatfs 4 <ptr>`, R: syscall.Statfs_t{Type: update.Ext4Magic}},
		{C: `fstat 4 <ptr>`, R: syscall.Stat_t{}},
		{C: `close 4`},
	})
}

// We want to create a directory in /snap/foo/42/dir and want to know what happens.
func (s *utilsSuite) TestSecureMkdirAllInSNAP(c *C) {
	// Allow creating directories under /snap/ related to this snap ("foo").
	// This matches what is done inside main().
	restore := s.as.MockUnrestrictedPaths("/snap/foo")
	defer restore()

	s.sys.InsertFault(`mkdirat 3 "snap" 0755`, syscall.EEXIST)
	s.sys.InsertFault(`mkdirat 4 "foo" 0755`, syscall.EEXIST)
	s.sys.InsertFault(`mkdirat 5 "42" 0755`, syscall.EEXIST)

	rs := s.as.RestrictionsFor("/snap/foo/42/dir")
	err := update.MkdirAll("/snap/foo/42/dir", 0755, 0, 0, rs)
	c.Assert(err, IsNil)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "snap" 0755`, E: syscall.EEXIST},
		{C: `openat 3 "snap" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `mkdirat 4 "foo" 0755`, E: syscall.EEXIST},
		{C: `openat 4 "foo" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 5},
		{C: `mkdirat 5 "42" 0755`, E: syscall.EEXIST},
		{C: `openat 5 "42" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 6},
		{C: `close 5`},
		{C: `close 4`},
		{C: `close 3`},
		{C: `mkdirat 6 "dir" 0755`},
		{C: `openat 6 "dir" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `fchown 3 0 0`},
		{C: `close 3`},
		{C: `close 6`},
	})
}

// We want to create a symlink in /etc which is a tmpfs that we mounted so that is ok.
func (s *utilsSuite) TestSecureMksymlinkAllInEtcAfterMimic(c *C) {
	// Because /etc is not on a list of unrestricted paths the write to
	// /etc/symlink must be validated with step-by-step operation.
	rootStatfs := syscall.Statfs_t{Type: update.SquashfsMagic, Flags: update.StReadOnly}
	rootStat := syscall.Stat_t{}
	etcStatfs := syscall.Statfs_t{Type: update.TmpfsMagic}
	etcStat := syscall.Stat_t{}
	s.as.AddChange(&update.Change{Action: update.Mount, Entry: osutil.MountEntry{Dir: "/etc", Type: "tmpfs", Name: "tmpfs"}})
	s.sys.InsertFstatfsResult(`fstatfs 3 <ptr>`, rootStatfs)
	s.sys.InsertFstatResult(`fstat 3 <ptr>`, rootStat)
	s.sys.InsertFault(`mkdirat 3 "etc" 0755`, syscall.EEXIST)
	s.sys.InsertFstatfsResult(`fstatfs 4 <ptr>`, etcStatfs)
	s.sys.InsertFstatResult(`fstat 4 <ptr>`, etcStat)
	rs := s.as.RestrictionsFor("/etc/symlink")
	err := update.MksymlinkAll("/etc/symlink", 0755, 0, 0, "/oldname", rs)
	c.Assert(err, IsNil)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `fstatfs 3 <ptr>`, R: rootStatfs},
		{C: `fstat 3 <ptr>`, R: rootStat},
		{C: `mkdirat 3 "etc" 0755`, E: syscall.EEXIST},
		{C: `openat 3 "etc" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `close 3`},
		{C: `fstatfs 4 <ptr>`, R: etcStatfs},
		{C: `fstat 4 <ptr>`, R: etcStat},
		{C: `symlinkat "/oldname" 4 "symlink"`},
		{C: `close 4`},
	})
}

// We want to create a file in /etc which is a tmpfs created by snapd so that's okay.
func (s *utilsSuite) TestSecureMkfileAllInEtcAfterMimic(c *C) {
	s.sys.InsertFstatfsResult(`fstatfs 3 <ptr>`, syscall.Statfs_t{Type: update.SquashfsMagic})
	s.sys.InsertFstatResult(`fstat 3 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFstatfsResult(`fstatfs 4 <ptr>`, syscall.Statfs_t{Type: update.TmpfsMagic})
	s.sys.InsertFstatResult(`fstat 4 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFault(`mkdirat 3 "etc" 0755`, syscall.EEXIST)
	s.as.AddChange(&update.Change{Action: update.Mount, Entry: osutil.MountEntry{Dir: "/etc", Type: "tmpfs", Name: "tmpfs"}})
	rs := s.as.RestrictionsFor("/etc/file")
	err := update.MkfileAll("/etc/file", 0755, 0, 0, rs)
	c.Assert(err, IsNil)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `fstatfs 3 <ptr>`, R: syscall.Statfs_t{Type: update.SquashfsMagic}},
		{C: `fstat 3 <ptr>`, R: syscall.Stat_t{}},
		{C: `mkdirat 3 "etc" 0755`, E: syscall.EEXIST},
		{C: `openat 3 "etc" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `close 3`},
		{C: `fstatfs 4 <ptr>`, R: syscall.Statfs_t{Type: update.TmpfsMagic}},
		{C: `fstat 4 <ptr>`, R: syscall.Stat_t{}},
		{C: `openat 4 "file" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`, R: 3},
		{C: `fchown 3 0 0`},
		{C: `close 3`},
		{C: `close 4`},
	})
}

// We want to create a directory in /etc which is a tmpfs created by snapd so that is ok.
func (s *utilsSuite) TestSecureMkdirAllInEtcAfterMimic(c *C) {
	s.sys.InsertFstatfsResult(`fstatfs 3 <ptr>`, syscall.Statfs_t{Type: update.SquashfsMagic})
	s.sys.InsertFstatResult(`fstat 3 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFstatfsResult(`fstatfs 4 <ptr>`, syscall.Statfs_t{Type: update.TmpfsMagic})
	s.sys.InsertFstatResult(`fstat 4 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFault(`mkdirat 3 "etc" 0755`, syscall.EEXIST)
	s.as.AddChange(&update.Change{Action: update.Mount, Entry: osutil.MountEntry{Dir: "/etc", Type: "tmpfs", Name: "tmpfs"}})
	rs := s.as.RestrictionsFor("/etc/dir")
	err := update.MkdirAll("/etc/dir", 0755, 0, 0, rs)
	c.Assert(err, IsNil)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `fstatfs 3 <ptr>`, R: syscall.Statfs_t{Type: update.SquashfsMagic}},
		{C: `fstat 3 <ptr>`, R: syscall.Stat_t{}},
		{C: `mkdirat 3 "etc" 0755`, E: syscall.EEXIST},
		{C: `openat 3 "etc" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `close 3`},
		{C: `fstatfs 4 <ptr>`, R: syscall.Statfs_t{Type: update.TmpfsMagic}},
		{C: `fstat 4 <ptr>`, R: syscall.Stat_t{}},
		{C: `mkdirat 4 "dir" 0755`},
		{C: `openat 4 "dir" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `fchown 3 0 0`},
		{C: `close 3`},
		{C: `close 4`},
	})
}

// Check that we can actually create files.
// This doesn't test the chown logic as that requires root.
func (s *realSystemSuite) TestSecureMkfileAllForReal(c *C) {
	d := c.MkDir()

	// Create f1, which is a simple subdirectory, with a distinct mode and
	// check that it was applied. Note that default umask 022 is subtracted so
	// effective directory has different permissions.
	f1 := filepath.Join(d, "file")
	c.Assert(update.MkfileAll(f1, 0707, sys.FlagID, sys.FlagID, nil), IsNil)
	fi, err := os.Stat(f1)
	c.Assert(err, IsNil)
	c.Check(fi.Mode().IsRegular(), Equals, true)
	c.Check(fi.Mode().Perm(), Equals, os.FileMode(0705))

	// Create f2, which is a deeper subdirectory, with another distinct mode
	// and check that it was applied.
	f2 := filepath.Join(d, "subdir/subdir/file")
	c.Assert(update.MkfileAll(f2, 0750, sys.FlagID, sys.FlagID, nil), IsNil)
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
	err := update.MksymlinkAll(f1, 0755, sys.FlagID, sys.FlagID, "oldname", nil)
	c.Assert(err, IsNil)
	fi, err := os.Lstat(f1)
	c.Assert(err, IsNil)
	c.Check(fi.Mode()&os.ModeSymlink, Equals, os.ModeSymlink)
	c.Check(fi.Mode().Perm(), Equals, os.FileMode(0777))

	target, err := os.Readlink(f1)
	c.Assert(err, IsNil)
	c.Check(target, Equals, "oldname")

	// Create an identical symlink to see that it doesn't fail.
	err = update.MksymlinkAll(f1, 0755, sys.FlagID, sys.FlagID, "oldname", nil)
	c.Assert(err, IsNil)

	// Create a different symlink and see that it fails now
	err = update.MksymlinkAll(f1, 0755, sys.FlagID, sys.FlagID, "other", nil)
	c.Assert(err, ErrorMatches, `cannot create symbolic link ".*/symlink": existing symbolic link in the way`)

	// Create an file and check that it clashes with a symlink we attempt to create.
	f2 := filepath.Join(d, "file")
	err = update.MkfileAll(f2, 0755, sys.FlagID, sys.FlagID, nil)
	c.Assert(err, IsNil)
	err = update.MksymlinkAll(f2, 0755, sys.FlagID, sys.FlagID, "oldname", nil)
	c.Assert(err, ErrorMatches, `cannot create symbolic link ".*/file": existing file in the way`)

	// Create an file and check that it clashes with a symlink we attempt to create.
	f3 := filepath.Join(d, "dir")
	err = update.MkdirAll(f3, 0755, sys.FlagID, sys.FlagID, nil)
	c.Assert(err, IsNil)
	err = update.MksymlinkAll(f3, 0755, sys.FlagID, sys.FlagID, "oldname", nil)
	c.Assert(err, ErrorMatches, `cannot create symbolic link ".*/dir": existing file in the way`)

	err = update.MksymlinkAll("/", 0755, sys.FlagID, sys.FlagID, "oldname", nil)
	c.Assert(err, ErrorMatches, `cannot create non-file path: "/"`)
}

func (s *utilsSuite) TestCleanTrailingSlash(c *C) {
	// This is a validity test for the use of filepath.Clean in secureMk{dir,file}All
	c.Assert(filepath.Clean("/path/"), Equals, "/path")
	c.Assert(filepath.Clean("path/"), Equals, "path")
	c.Assert(filepath.Clean("path/."), Equals, "path")
	c.Assert(filepath.Clean("path/.."), Equals, ".")
	c.Assert(filepath.Clean("other/path/.."), Equals, "other")
}

// secure-open-path

func (s *utilsSuite) TestSecureOpenPath(c *C) {
	stat := syscall.Stat_t{Mode: syscall.S_IFDIR}
	s.sys.InsertFstatResult("fstat 5 <ptr>", stat)
	fd, err := update.OpenPath("/foo/bar")
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

func (s *utilsSuite) TestSecureOpenPathSingleSegment(c *C) {
	stat := syscall.Stat_t{Mode: syscall.S_IFDIR}
	s.sys.InsertFstatResult("fstat 4 <ptr>", stat)
	fd, err := update.OpenPath("/foo")
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

func (s *utilsSuite) TestSecureOpenPathRoot(c *C) {
	stat := syscall.Stat_t{Mode: syscall.S_IFDIR}
	s.sys.InsertFstatResult("fstat 3 <ptr>", stat)
	fd, err := update.OpenPath("/")
	c.Assert(err, IsNil)
	defer s.sys.Close(fd)
	c.Assert(fd, Equals, 3)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `fstat 3 <ptr>`, R: stat},
	})
}

func (s *realSystemSuite) TestSecureOpenPathDirectory(c *C) {
	path := filepath.Join(c.MkDir(), "test")
	c.Assert(os.Mkdir(path, 0755), IsNil)

	fd, err := update.OpenPath(path)
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
	fd, err := update.OpenPath("relative/path")
	c.Check(fd, Equals, -1)
	c.Check(err, ErrorMatches, "path .* is not absolute")
}

func (s *realSystemSuite) TestSecureOpenPathUncleanPath(c *C) {
	base := c.MkDir()
	path := filepath.Join(base, "test")
	c.Assert(os.Mkdir(path, 0755), IsNil)

	fd, err := update.OpenPath(base + "//test")
	c.Check(fd, Equals, -1)
	c.Check(err, ErrorMatches, `cannot open path: cannot iterate over unclean path ".*//test"`)

	fd, err = update.OpenPath(base + "/./test")
	c.Check(fd, Equals, -1)
	c.Check(err, ErrorMatches, `cannot open path: cannot iterate over unclean path ".*/./test"`)

	fd, err = update.OpenPath(base + "/test/../test")
	c.Check(fd, Equals, -1)
	c.Check(err, ErrorMatches, `cannot open path: cannot iterate over unclean path ".*/test/../test"`)
}

func (s *realSystemSuite) TestSecureOpenPathFile(c *C) {
	path := filepath.Join(c.MkDir(), "file.txt")
	c.Assert(os.WriteFile(path, []byte("hello"), 0644), IsNil)

	fd, err := update.OpenPath(path)
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

	fd, err := update.OpenPath(path)
	c.Check(fd, Equals, -1)
	c.Check(err, ErrorMatches, "no such file or directory")
}

func (s *realSystemSuite) TestSecureOpenPathSymlink(c *C) {
	base := c.MkDir()
	dir := filepath.Join(base, "test")
	c.Assert(os.Mkdir(dir, 0755), IsNil)

	symlink := filepath.Join(base, "symlink")
	c.Assert(os.Symlink(dir, symlink), IsNil)

	fd, err := update.OpenPath(symlink)
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

	fd, err := update.OpenPath(symlinkedPath)
	c.Check(fd, Equals, -1)
	c.Check(err, ErrorMatches, "not a directory")
}
