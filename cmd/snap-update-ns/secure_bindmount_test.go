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
	"syscall"

	. "gopkg.in/check.v1"

	update "github.com/snapcore/snapd/cmd/snap-update-ns"
	"github.com/snapcore/snapd/testutil"
)

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
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`,          // -> 3
		`openat 3 "source" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, // -> 4
		`openat 4 "dir" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`,    // -> 5
		`close 4`, // "/source"
		`close 3`, // "/"
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`,          // -> 3
		`openat 3 "target" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, // -> 4
		`openat 4 "dir" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`,    // -> 6
		`close 4`,  // "/target"
		`close 3`,  // "/"
		`fchdir 5`, // "/source/dir"
		`mount "." "/stash" "" MS_BIND ""`,
		`fchdir 6`, // "/target/dir"
		`mount "/stash" "." "" MS_BIND ""`,
		`unmount "/stash" UMOUNT_NOFOLLOW|MNT_DETACH`,
		`close 6`, // "/target/dir"
		`close 5`, // "/source/dir"
	})
}

func (s *secureBindMountSuite) TestMountRecursive(c *C) {
	err := update.SecureBindMount("/source/dir", "/target/dir", syscall.MS_REC, "/stash")
	c.Assert(err, IsNil)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`,          // -> 3
		`openat 3 "source" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, // -> 4
		`openat 4 "dir" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`,    // -> 5
		`close 4`, // "/source"
		`close 3`, // "/"
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`,          // -> 3
		`openat 3 "target" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, // -> 4
		`openat 4 "dir" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`,    // -> 6
		`close 4`,  // "/target"
		`close 3`,  // "/"
		`fchdir 5`, // "/source/dir"
		`mount "." "/stash" "" MS_BIND|MS_REC ""`,
		`fchdir 6`, // "/target/dir"
		`mount "/stash" "." "" MS_BIND|MS_REC ""`,
		`unmount "/stash" UMOUNT_NOFOLLOW|MNT_DETACH`,
		`close 6`, // "/target/dir"
		`close 5`, // "/source/dir"
	})
}

func (s *secureBindMountSuite) TestMountReadOnly(c *C) {
	err := update.SecureBindMount("/source/dir", "/target/dir", syscall.MS_RDONLY, "/stash")
	c.Assert(err, IsNil)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`,          // -> 3
		`openat 3 "source" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, // -> 4
		`openat 4 "dir" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`,    // -> 5
		`close 4`, // "/source"
		`close 3`, // "/"
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`,          // -> 3
		`openat 3 "target" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, // -> 4
		`openat 4 "dir" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`,    // -> 6
		`close 4`,  // "/target"
		`close 3`,  // "/"
		`fchdir 5`, // "/source/dir"
		`mount "." "/stash" "" MS_BIND ""`,
		`mount "none" "/stash" "" MS_REMOUNT|MS_BIND|MS_RDONLY ""`,
		`fchdir 6`, // "/target/dir"
		`mount "/stash" "." "" MS_BIND ""`,
		`unmount "/stash" UMOUNT_NOFOLLOW|MNT_DETACH`,
		`close 6`, // "/target/dir"
		`close 5`, // "/source/dir"
	})
}

func (s *secureBindMountSuite) TestMountReadOnlyRecursive(c *C) {
	err := update.SecureBindMount("/source/dir", "/target/dir", syscall.MS_RDONLY|syscall.MS_REC, "/stash")
	c.Assert(err, ErrorMatches, "cannot use MS_RDONLY and MS_REC together")
	c.Check(s.sys.Calls(), DeepEquals, []string(nil))
}

func (s *secureBindMountSuite) TestChdirSourceFails(c *C) {
	s.sys.InsertFault(`fchdir 5`, errTesting)
	err := update.SecureBindMount("/source/dir", "/target/dir", syscall.MS_RDONLY, "/stash")
	c.Assert(err, ErrorMatches, "testing")
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`,          // -> 3
		`openat 3 "source" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, // -> 4
		`openat 4 "dir" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`,    // -> 5
		`close 4`, // "/source"
		`close 3`, // "/"
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`,          // -> 3
		`openat 3 "target" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, // -> 4
		`openat 4 "dir" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`,    // -> 6
		`close 4`,  // "/target"
		`close 3`,  // "/"
		`fchdir 5`, // "/source/dir"
		`close 6`,  // "/target/dir"
		`close 5`,  // "/source/dir"
	})
}

func (s *secureBindMountSuite) TestBindMountSourceFails(c *C) {
	s.sys.InsertFault(`mount "." "/stash" "" MS_BIND ""`, errTesting)
	err := update.SecureBindMount("/source/dir", "/target/dir", syscall.MS_RDONLY, "/stash")
	c.Assert(err, ErrorMatches, "testing")
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`,          // -> 3
		`openat 3 "source" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, // -> 4
		`openat 4 "dir" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`,    // -> 5
		`close 4`, // "/source"
		`close 3`, // "/"
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`,          // -> 3
		`openat 3 "target" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, // -> 4
		`openat 4 "dir" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`,    // -> 6
		`close 4`,  // "/target"
		`close 3`,  // "/"
		`fchdir 5`, // "/source/dir"
		`mount "." "/stash" "" MS_BIND ""`,
		`close 6`, // "/target/dir"
		`close 5`, // "/source/dir"
	})
}

func (s *secureBindMountSuite) TestRemountStashFails(c *C) {
	s.sys.InsertFault(`mount "none" "/stash" "" MS_REMOUNT|MS_BIND|MS_RDONLY ""`, errTesting)
	err := update.SecureBindMount("/source/dir", "/target/dir", syscall.MS_RDONLY, "/stash")
	c.Assert(err, ErrorMatches, "testing")
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`,          // -> 3
		`openat 3 "source" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, // -> 4
		`openat 4 "dir" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`,    // -> 5
		`close 4`, // "/source"
		`close 3`, // "/"
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`,          // -> 3
		`openat 3 "target" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, // -> 4
		`openat 4 "dir" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`,    // -> 6
		`close 4`,  // "/target"
		`close 3`,  // "/"
		`fchdir 5`, // "/source/dir"
		`mount "." "/stash" "" MS_BIND ""`,
		`mount "none" "/stash" "" MS_REMOUNT|MS_BIND|MS_RDONLY ""`,
		`unmount "/stash" UMOUNT_NOFOLLOW|MNT_DETACH`,
		`close 6`, // "/target/dir"
		`close 5`, // "/source/dir"
	})
}

func (s *secureBindMountSuite) TestChdirTargetFails(c *C) {
	s.sys.InsertFault(`fchdir 6`, errTesting)
	err := update.SecureBindMount("/source/dir", "/target/dir", syscall.MS_RDONLY, "/stash")
	c.Assert(err, ErrorMatches, "testing")
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`,          // -> 3
		`openat 3 "source" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, // -> 4
		`openat 4 "dir" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`,    // -> 5
		`close 4`, // "/source"
		`close 3`, // "/"
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`,          // -> 3
		`openat 3 "target" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, // -> 4
		`openat 4 "dir" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`,    // -> 6
		`close 4`,  // "/target"
		`close 3`,  // "/"
		`fchdir 5`, // "/source/dir"
		`mount "." "/stash" "" MS_BIND ""`,
		`mount "none" "/stash" "" MS_REMOUNT|MS_BIND|MS_RDONLY ""`,
		`fchdir 6`, // "/target/dir"
		`unmount "/stash" UMOUNT_NOFOLLOW|MNT_DETACH`,
		`close 6`, // "/target/dir"
		`close 5`, // "/source/dir"
	})
}

func (s *secureBindMountSuite) TestBindMountTargetFails(c *C) {
	s.sys.InsertFault(`mount "/stash" "." "" MS_BIND ""`, errTesting)
	err := update.SecureBindMount("/source/dir", "/target/dir", syscall.MS_RDONLY, "/stash")
	c.Assert(err, ErrorMatches, "testing")
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`,          // -> 3
		`openat 3 "source" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, // -> 4
		`openat 4 "dir" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`,    // -> 5
		`close 4`, // "/source"
		`close 3`, // "/"
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`,          // -> 3
		`openat 3 "target" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, // -> 4
		`openat 4 "dir" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`,    // -> 6
		`close 4`,  // "/target"
		`close 3`,  // "/"
		`fchdir 5`, // "/source/dir"
		`mount "." "/stash" "" MS_BIND ""`,
		`mount "none" "/stash" "" MS_REMOUNT|MS_BIND|MS_RDONLY ""`,
		`fchdir 6`, // "/target/dir"
		`mount "/stash" "." "" MS_BIND ""`,
		`unmount "/stash" UMOUNT_NOFOLLOW|MNT_DETACH`,
		`close 6`, // "/target/dir"
		`close 5`, // "/source/dir"
	})
}
