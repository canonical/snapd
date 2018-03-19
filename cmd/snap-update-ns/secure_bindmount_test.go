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

package main_test

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"syscall"

	. "gopkg.in/check.v1"

	update "github.com/snapcore/snapd/cmd/snap-update-ns"
	"github.com/snapcore/snapd/testutil"
)

type openNoFollowSuite struct{}

var _ = Suite(&openNoFollowSuite{})

func (s *openNoFollowSuite) TestDirectory(c *C) {
	path := filepath.Join(c.MkDir(), "test")
	c.Assert(os.Mkdir(path, 0755), IsNil)

	fd, err := update.OpenNoFollow(path)
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

func (s *openNoFollowSuite) TestRelativePath(c *C) {
	fd, err := update.OpenNoFollow("relative/path")
	c.Check(fd, Equals, -1)
	c.Check(err, ErrorMatches, "path .* is not absolute")
}

func (s *openNoFollowSuite) TestUncleanPath(c *C) {
	base := c.MkDir()
	path := filepath.Join(base, "test")
	c.Assert(os.Mkdir(path, 0755), IsNil)

	fd, err := update.OpenNoFollow(base + "//test")
	c.Check(fd, Equals, -1)
	c.Check(err, ErrorMatches, "cannot split unclean path .*")

	fd, err = update.OpenNoFollow(base + "/./test")
	c.Check(fd, Equals, -1)
	c.Check(err, ErrorMatches, "cannot split unclean path .*")

	fd, err = update.OpenNoFollow(base + "/test/../test")
	c.Check(fd, Equals, -1)
	c.Check(err, ErrorMatches, "cannot split unclean path .*")
}

func (s *openNoFollowSuite) TestFile(c *C) {
	path := filepath.Join(c.MkDir(), "file.txt")
	c.Assert(ioutil.WriteFile(path, []byte("hello"), 0644), IsNil)

	fd, err := update.OpenNoFollow(path)
	c.Check(fd, Equals, -1)
	c.Check(err, ErrorMatches, "not a directory")
}

func (s *openNoFollowSuite) TestNotFound(c *C) {
	path := filepath.Join(c.MkDir(), "test")

	fd, err := update.OpenNoFollow(path)
	c.Check(fd, Equals, -1)
	c.Check(err, ErrorMatches, "no such file or directory")
}

func (s *openNoFollowSuite) TestSymlink(c *C) {
	base := c.MkDir()
	dir := filepath.Join(base, "test")
	c.Assert(os.Mkdir(dir, 0755), IsNil)

	symlink := filepath.Join(base, "symlink")
	c.Assert(os.Symlink(dir, symlink), IsNil)

	fd, err := update.OpenNoFollow(symlink)
	c.Check(fd, Equals, -1)
	c.Check(err, ErrorMatches, "not a directory")
}

func (s *openNoFollowSuite) TestSymlinkedParent(c *C) {
	base := c.MkDir()
	dir := filepath.Join(base, "dir1")
	symlink := filepath.Join(base, "symlink")

	path := filepath.Join(dir, "dir2")
	symlinkedPath := filepath.Join(symlink, "dir2")

	c.Assert(os.Mkdir(dir, 0755), IsNil)
	c.Assert(os.Symlink(dir, symlink), IsNil)
	c.Assert(os.Mkdir(path, 0755), IsNil)

	fd, err := update.OpenNoFollow(symlinkedPath)
	c.Check(fd, Equals, -1)
	c.Check(err, ErrorMatches, "not a directory")
}

type secureBindMountSuite struct {
	testutil.BaseTest
	sys *testutil.SyscallRecorder
}

var _ = Suite(&secureBindMountSuite{})

func (s *secureBindMountSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.sys = &testutil.SyscallRecorder{}
	s.BaseTest.AddCleanup(update.MockSystemCalls(s.sys))
}

func (s *secureBindMountSuite) TearDownTest(c *C) {
	s.sys.CheckForStrayDescriptors(c)
	s.BaseTest.TearDownTest(c)
}

func (s *secureBindMountSuite) TestMount(c *C) {
	err := update.SecureBindMount("/source/dir", "/target/dir", 0, "/stash")
	c.Assert(err, IsNil)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_NOFOLLOW|O_DIRECTORY|O_PATH 0`,
		`openat 3 "source" O_NOFOLLOW|O_DIRECTORY|O_PATH 0`,
		`close 3`,
		`openat 4 "dir" O_NOFOLLOW|O_DIRECTORY|O_PATH 0`,
		`close 4`,
		`open "/" O_NOFOLLOW|O_DIRECTORY|O_PATH 0`,
		`openat 4 "target" O_NOFOLLOW|O_DIRECTORY|O_PATH 0`,
		`close 4`,
		`openat 5 "dir" O_NOFOLLOW|O_DIRECTORY|O_PATH 0`,
		`close 5`,
		`fchdir 3`,
		`mount "." "/stash" "" MS_BIND ""`,
		`fchdir 4`,
		`mount "/stash" "." "" MS_BIND ""`,
		`unmount "/stash" MNT_DETACH`,
		`close 4`,
		`close 3`,
	})
}

func (s *secureBindMountSuite) TestMountRecursive(c *C) {
	err := update.SecureBindMount("/source/dir", "/target/dir", syscall.MS_REC, "/stash")
	c.Assert(err, IsNil)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_NOFOLLOW|O_DIRECTORY|O_PATH 0`,
		`openat 3 "source" O_NOFOLLOW|O_DIRECTORY|O_PATH 0`,
		`close 3`,
		`openat 4 "dir" O_NOFOLLOW|O_DIRECTORY|O_PATH 0`,
		`close 4`,
		`open "/" O_NOFOLLOW|O_DIRECTORY|O_PATH 0`,
		`openat 4 "target" O_NOFOLLOW|O_DIRECTORY|O_PATH 0`,
		`close 4`,
		`openat 5 "dir" O_NOFOLLOW|O_DIRECTORY|O_PATH 0`,
		`close 5`,
		`fchdir 3`,
		`mount "." "/stash" "" MS_BIND|MS_REC ""`,
		`fchdir 4`,
		`mount "/stash" "." "" MS_BIND|MS_REC ""`,
		`unmount "/stash" MNT_DETACH`,
		`close 4`,
		`close 3`,
	})
}

func (s *secureBindMountSuite) TestMountReadOnly(c *C) {
	err := update.SecureBindMount("/source/dir", "/target/dir", syscall.MS_RDONLY, "/stash")
	c.Assert(err, IsNil)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_NOFOLLOW|O_DIRECTORY|O_PATH 0`,
		`openat 3 "source" O_NOFOLLOW|O_DIRECTORY|O_PATH 0`,
		`close 3`,
		`openat 4 "dir" O_NOFOLLOW|O_DIRECTORY|O_PATH 0`,
		`close 4`,
		`open "/" O_NOFOLLOW|O_DIRECTORY|O_PATH 0`,
		`openat 4 "target" O_NOFOLLOW|O_DIRECTORY|O_PATH 0`,
		`close 4`,
		`openat 5 "dir" O_NOFOLLOW|O_DIRECTORY|O_PATH 0`,
		`close 5`,
		`fchdir 3`,
		`mount "." "/stash" "" MS_BIND ""`,
		`mount "none" "/stash" "" MS_REMOUNT|MS_BIND|MS_RDONLY ""`,
		`fchdir 4`,
		`mount "/stash" "." "" MS_BIND ""`,
		`unmount "/stash" MNT_DETACH`,
		`close 4`,
		`close 3`,
	})
}

func (s *secureBindMountSuite) TestChdirSourceFails(c *C) {
	s.sys.InsertFault(`fchdir 3`, errTesting)
	err := update.SecureBindMount("/source/dir", "/target/dir", syscall.MS_RDONLY, "/stash")
	c.Assert(err, ErrorMatches, "testing")
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_NOFOLLOW|O_DIRECTORY|O_PATH 0`,
		`openat 3 "source" O_NOFOLLOW|O_DIRECTORY|O_PATH 0`,
		`close 3`,
		`openat 4 "dir" O_NOFOLLOW|O_DIRECTORY|O_PATH 0`,
		`close 4`,
		`open "/" O_NOFOLLOW|O_DIRECTORY|O_PATH 0`,
		`openat 4 "target" O_NOFOLLOW|O_DIRECTORY|O_PATH 0`,
		`close 4`,
		`openat 5 "dir" O_NOFOLLOW|O_DIRECTORY|O_PATH 0`,
		`close 5`,
		`fchdir 3`,
		`close 4`,
		`close 3`,
	})
}

func (s *secureBindMountSuite) TestBindMountSourceFails(c *C) {
	s.sys.InsertFault(`mount "." "/stash" "" MS_BIND ""`, errTesting)
	err := update.SecureBindMount("/source/dir", "/target/dir", syscall.MS_RDONLY, "/stash")
	c.Assert(err, ErrorMatches, "testing")
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_NOFOLLOW|O_DIRECTORY|O_PATH 0`,
		`openat 3 "source" O_NOFOLLOW|O_DIRECTORY|O_PATH 0`,
		`close 3`,
		`openat 4 "dir" O_NOFOLLOW|O_DIRECTORY|O_PATH 0`,
		`close 4`,
		`open "/" O_NOFOLLOW|O_DIRECTORY|O_PATH 0`,
		`openat 4 "target" O_NOFOLLOW|O_DIRECTORY|O_PATH 0`,
		`close 4`,
		`openat 5 "dir" O_NOFOLLOW|O_DIRECTORY|O_PATH 0`,
		`close 5`,
		`fchdir 3`,
		`mount "." "/stash" "" MS_BIND ""`,
		`close 4`,
		`close 3`,
	})
}

func (s *secureBindMountSuite) TestRemountStashFails(c *C) {
	s.sys.InsertFault(`mount "none" "/stash" "" MS_REMOUNT|MS_BIND|MS_RDONLY ""`, errTesting)
	err := update.SecureBindMount("/source/dir", "/target/dir", syscall.MS_RDONLY, "/stash")
	c.Assert(err, ErrorMatches, "testing")
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_NOFOLLOW|O_DIRECTORY|O_PATH 0`,
		`openat 3 "source" O_NOFOLLOW|O_DIRECTORY|O_PATH 0`,
		`close 3`,
		`openat 4 "dir" O_NOFOLLOW|O_DIRECTORY|O_PATH 0`,
		`close 4`,
		`open "/" O_NOFOLLOW|O_DIRECTORY|O_PATH 0`,
		`openat 4 "target" O_NOFOLLOW|O_DIRECTORY|O_PATH 0`,
		`close 4`,
		`openat 5 "dir" O_NOFOLLOW|O_DIRECTORY|O_PATH 0`,
		`close 5`,
		`fchdir 3`,
		`mount "." "/stash" "" MS_BIND ""`,
		`mount "none" "/stash" "" MS_REMOUNT|MS_BIND|MS_RDONLY ""`,
		`unmount "/stash" MNT_DETACH`,
		`close 4`,
		`close 3`,
	})
}

func (s *secureBindMountSuite) TestChdirTargetFails(c *C) {
	s.sys.InsertFault(`fchdir 4`, errTesting)
	err := update.SecureBindMount("/source/dir", "/target/dir", syscall.MS_RDONLY, "/stash")
	c.Assert(err, ErrorMatches, "testing")
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_NOFOLLOW|O_DIRECTORY|O_PATH 0`,
		`openat 3 "source" O_NOFOLLOW|O_DIRECTORY|O_PATH 0`,
		`close 3`,
		`openat 4 "dir" O_NOFOLLOW|O_DIRECTORY|O_PATH 0`,
		`close 4`,
		`open "/" O_NOFOLLOW|O_DIRECTORY|O_PATH 0`,
		`openat 4 "target" O_NOFOLLOW|O_DIRECTORY|O_PATH 0`,
		`close 4`,
		`openat 5 "dir" O_NOFOLLOW|O_DIRECTORY|O_PATH 0`,
		`close 5`,
		`fchdir 3`,
		`mount "." "/stash" "" MS_BIND ""`,
		`mount "none" "/stash" "" MS_REMOUNT|MS_BIND|MS_RDONLY ""`,
		`fchdir 4`,
		`unmount "/stash" MNT_DETACH`,
		`close 4`,
		`close 3`,
	})
}

func (s *secureBindMountSuite) TestBindMountTargetFails(c *C) {
	s.sys.InsertFault(`mount "/stash" "." "" MS_BIND ""`, errTesting)
	err := update.SecureBindMount("/source/dir", "/target/dir", syscall.MS_RDONLY, "/stash")
	c.Assert(err, ErrorMatches, "testing")
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_NOFOLLOW|O_DIRECTORY|O_PATH 0`,
		`openat 3 "source" O_NOFOLLOW|O_DIRECTORY|O_PATH 0`,
		`close 3`,
		`openat 4 "dir" O_NOFOLLOW|O_DIRECTORY|O_PATH 0`,
		`close 4`,
		`open "/" O_NOFOLLOW|O_DIRECTORY|O_PATH 0`,
		`openat 4 "target" O_NOFOLLOW|O_DIRECTORY|O_PATH 0`,
		`close 4`,
		`openat 5 "dir" O_NOFOLLOW|O_DIRECTORY|O_PATH 0`,
		`close 5`,
		`fchdir 3`,
		`mount "." "/stash" "" MS_BIND ""`,
		`mount "none" "/stash" "" MS_REMOUNT|MS_BIND|MS_RDONLY ""`,
		`fchdir 4`,
		`mount "/stash" "." "" MS_BIND ""`,
		`unmount "/stash" MNT_DETACH`,
		`close 4`,
		`close 3`,
	})
}
