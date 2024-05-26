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

	"github.com/ddkwork/golibrary/mylog"
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
	s.sys.InsertFstatResult(`fstat 5 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFstatResult(`fstat 6 <ptr>`, syscall.Stat_t{})
	mylog.Check(update.BindMount("/source/dir", "/target/dir", syscall.MS_BIND))

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
	mylog.Check(update.BindMount("/source/dir", "/target/dir", syscall.MS_BIND|syscall.MS_REC))

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
	mylog.Check(update.BindMount("/source/dir", "/target/dir", syscall.MS_BIND|syscall.MS_RDONLY))

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
	mylog.Check(update.BindMount("/source/dir", "/target/dir", syscall.MS_REC))
	c.Assert(err, ErrorMatches, "cannot perform non-bind mount operation")
	c.Check(s.sys.RCalls(), HasLen, 0)
}

func (s *secureBindMountSuite) TestMountReadOnlyRecursive(c *C) {
	mylog.Check(update.BindMount("/source/dir", "/target/dir", syscall.MS_BIND|syscall.MS_RDONLY|syscall.MS_REC))
	c.Assert(err, ErrorMatches, "cannot use MS_RDONLY and MS_REC together")
	c.Check(s.sys.RCalls(), HasLen, 0)
}

func (s *secureBindMountSuite) TestBindMountFails(c *C) {
	s.sys.InsertFstatResult(`fstat 5 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFstatResult(`fstat 6 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFault(`mount "/proc/self/fd/5" "/proc/self/fd/6" "" MS_BIND ""`, errTesting)
	mylog.Check(update.BindMount("/source/dir", "/target/dir", syscall.MS_BIND|syscall.MS_RDONLY))
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
	mylog.Check(update.BindMount("/source/dir", "/target/dir", syscall.MS_BIND|syscall.MS_RDONLY))
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
