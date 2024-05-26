// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017-2018 Canonical Ltd
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
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/testutil"
)

type trespassingSuite struct {
	testutil.BaseTest
	sys *testutil.SyscallRecorder
}

var _ = Suite(&trespassingSuite{})

func (s *trespassingSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.sys = &testutil.SyscallRecorder{}
	s.BaseTest.AddCleanup(update.MockSystemCalls(s.sys))
}

func (s *trespassingSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
	s.sys.CheckForStrayDescriptors(c)
}

// AddUnrestrictedPaths and IsRestricted

func (s *trespassingSuite) TestAddUnrestrictedPaths(c *C) {
	a := &update.Assumptions{}
	c.Assert(a.IsRestricted("/etc/test.conf"), Equals, true)

	a.AddUnrestrictedPaths("/etc")
	c.Assert(a.IsRestricted("/etc/test.conf"), Equals, false)
	c.Assert(a.IsRestricted("/etc/"), Equals, false)
	c.Assert(a.IsRestricted("/etc"), Equals, false)
	c.Assert(a.IsRestricted("/etc2"), Equals, true)

	a.AddUnrestrictedPaths("/")
	c.Assert(a.IsRestricted("/foo"), Equals, false)
}

func (s *trespassingSuite) TestMockUnrestrictedPaths(c *C) {
	a := &update.Assumptions{}
	c.Assert(a.IsRestricted("/etc/test.conf"), Equals, true)
	restore := a.MockUnrestrictedPaths("/etc/")
	c.Assert(a.IsRestricted("/etc/test.conf"), Equals, false)
	restore()
	c.Assert(a.IsRestricted("/etc/test.conf"), Equals, true)
}

// canWriteToDirectory and AddChange

// We are not allowed to write to ext4.
func (s *trespassingSuite) TestCanWriteToDirectoryWritableExt4(c *C) {
	a := &update.Assumptions{}

	path := "/etc"
	fd := mylog.Check2(s.sys.Open(path, syscall.O_DIRECTORY, 0))

	defer s.sys.Close(fd)

	s.sys.InsertFstatfsResult(`fstatfs 3 <ptr>`, syscall.Statfs_t{Type: update.Ext4Magic})
	s.sys.InsertFstatResult(`fstat 3 <ptr>`, syscall.Stat_t{})

	ok := mylog.Check2(a.CanWriteToDirectory(fd, path))

	c.Assert(ok, Equals, false)
}

// We are allowed to write to ext4 that was mounted read-only.
func (s *trespassingSuite) TestCanWriteToDirectoryReadOnlyExt4(c *C) {
	a := &update.Assumptions{}

	path := "/etc"
	fd := mylog.Check2(s.sys.Open(path, syscall.O_DIRECTORY, 0))

	defer s.sys.Close(fd)

	s.sys.InsertFstatfsResult(`fstatfs 3 <ptr>`, syscall.Statfs_t{Type: update.Ext4Magic, Flags: update.StReadOnly})
	s.sys.InsertFstatResult(`fstat 3 <ptr>`, syscall.Stat_t{})

	ok := mylog.Check2(a.CanWriteToDirectory(fd, path))

	c.Assert(ok, Equals, true)
}

// We are not allowed to write to tmpfs.
func (s *trespassingSuite) TestCanWriteToDirectoryTmpfs(c *C) {
	a := &update.Assumptions{}

	path := "/etc"
	fd := mylog.Check2(s.sys.Open(path, syscall.O_DIRECTORY, 0))

	defer s.sys.Close(fd)

	s.sys.InsertFstatfsResult(`fstatfs 3 <ptr>`, syscall.Statfs_t{Type: update.TmpfsMagic})
	s.sys.InsertFstatResult(`fstat 3 <ptr>`, syscall.Stat_t{})

	ok := mylog.Check2(a.CanWriteToDirectory(fd, path))

	c.Assert(ok, Equals, false)
}

// We are allowed to write to tmpfs that was mounted by snapd.
func (s *trespassingSuite) TestCanWriteToDirectoryTmpfsMountedBySnapd(c *C) {
	a := &update.Assumptions{}

	path := "/etc"
	fd := mylog.Check2(s.sys.Open(path, syscall.O_DIRECTORY, 0))

	defer s.sys.Close(fd)

	s.sys.InsertFstatfsResult(`fstatfs 3 <ptr>`, syscall.Statfs_t{Type: update.TmpfsMagic})
	s.sys.InsertFstatResult(`fstat 3 <ptr>`, syscall.Stat_t{})

	a.AddChange(&update.Change{
		Action: update.Mount,
		Entry:  osutil.MountEntry{Type: "tmpfs", Dir: path},
	})

	ok := mylog.Check2(a.CanWriteToDirectory(fd, path))

	c.Assert(ok, Equals, true)
}

// We are allowed to write to tmpfs that was mounted by snapd in another run.
func (s *trespassingSuite) TestCanWriteToDirectoryTmpfsMountedBySnapdEarlier(c *C) {
	a := &update.Assumptions{}

	path := "/etc"
	fd := mylog.Check2(s.sys.Open(path, syscall.O_DIRECTORY, 0))

	defer s.sys.Close(fd)

	s.sys.InsertFstatfsResult(`fstatfs 3 <ptr>`, syscall.Statfs_t{Type: update.TmpfsMagic})
	s.sys.InsertFstatResult(`fstat 3 <ptr>`, syscall.Stat_t{})

	a.AddChange(&update.Change{
		Action: update.Keep,
		Entry:  osutil.MountEntry{Type: "tmpfs", Dir: path},
	})

	ok := mylog.Check2(a.CanWriteToDirectory(fd, path))

	c.Assert(ok, Equals, true)
}

// We are allowed to write to directory beneath a tmpfs that was mounted by snapd.
func (s *trespassingSuite) TestCanWriteToDirectoryUnderTmpfsMountedBySnapd(c *C) {
	a := &update.Assumptions{}

	fd := mylog.Check2(s.sys.Open("/etc", syscall.O_DIRECTORY, 0))

	defer s.sys.Close(fd)

	s.sys.InsertFstatfsResult(`fstatfs 3 <ptr>`, syscall.Statfs_t{Type: update.TmpfsMagic})
	s.sys.InsertFstatResult(`fstat 3 <ptr>`, syscall.Stat_t{Dev: 0x42})

	a.AddChange(&update.Change{
		Action: update.Mount,
		Entry:  osutil.MountEntry{Type: "tmpfs", Dir: "/etc"},
	})

	ok := mylog.Check2(a.CanWriteToDirectory(fd, "/etc"))

	c.Assert(ok, Equals, true)

	// Now we have primed the assumption state with knowledge of 0x42 device as
	// a verified tmpfs.  We can now exploit it by trying to write to
	// /etc/conf.d and seeing that is allowed even though /etc/conf.d itself is
	// not a mount point representing tmpfs.

	fd2 := mylog.Check2(s.sys.Open("/etc/conf.d", syscall.O_DIRECTORY, 0))

	defer s.sys.Close(fd2)

	s.sys.InsertFstatfsResult(`fstatfs 4 <ptr>`, syscall.Statfs_t{Type: update.TmpfsMagic})
	s.sys.InsertFstatResult(`fstat 4 <ptr>`, syscall.Stat_t{Dev: 0x42})

	ok = mylog.Check2(a.CanWriteToDirectory(fd2, "/etc/conf.d"))

	c.Assert(ok, Equals, true)
}

// We are allowed to write to directory which is a bind mount of something, beneath a tmpfs that was mounted by snapd.
func (s *trespassingSuite) TestCanWriteToDirectoryUnderReboundTmpfsMountedBySnapd(c *C) {
	a := &update.Assumptions{}

	fd := mylog.Check2(s.sys.Open("/etc", syscall.O_DIRECTORY, 0))

	c.Assert(fd, Equals, 3)
	defer s.sys.Close(fd)

	s.sys.InsertFstatfsResult(`fstatfs 3 <ptr>`, syscall.Statfs_t{Type: update.TmpfsMagic})
	s.sys.InsertFstatResult(`fstat 3 <ptr>`, syscall.Stat_t{Dev: 0x42})

	a.AddChange(&update.Change{
		Action: update.Mount,
		Entry:  osutil.MountEntry{Type: "tmpfs", Dir: "/etc"},
	})

	ok := mylog.Check2(a.CanWriteToDirectory(fd, "/etc"))

	c.Assert(ok, Equals, true)

	// Now we have primed the assumption state with knowledge of 0x42 device as
	// a verified tmpfs. Unlike in the test above though the directory
	// /etc/conf.d is a bind mount from another tmpfs that we know nothing
	// about.
	fd2 := mylog.Check2(s.sys.Open("/etc/conf.d", syscall.O_DIRECTORY, 0))

	c.Assert(fd2, Equals, 4)
	defer s.sys.Close(fd2)

	s.sys.InsertFstatfsResult(`fstatfs 4 <ptr>`, syscall.Statfs_t{Type: update.TmpfsMagic})
	s.sys.InsertFstatResult(`fstat 4 <ptr>`, syscall.Stat_t{Dev: 0xdeadbeef})

	ok = mylog.Check2(a.CanWriteToDirectory(fd2, "/etc/conf.d"))

	c.Assert(ok, Equals, false)
}

// We are allowed to write to an unrestricted path.
func (s *trespassingSuite) TestCanWriteToDirectoryUnrestricted(c *C) {
	a := &update.Assumptions{}

	path := "/var/snap/foo/common"
	fd := mylog.Check2(s.sys.Open(path, syscall.O_DIRECTORY, 0))

	defer s.sys.Close(fd)

	s.sys.InsertFstatfsResult(`fstatfs 3 <ptr>`, syscall.Statfs_t{Type: update.Ext4Magic})
	s.sys.InsertFstatResult(`fstat 3 <ptr>`, syscall.Stat_t{})

	a.AddUnrestrictedPaths(path)

	ok := mylog.Check2(a.CanWriteToDirectory(fd, path))

	c.Assert(ok, Equals, true)
}

// Errors from fstatfs are propagated to the caller.
func (s *trespassingSuite) TestCanWriteToDirectoryErrorsFstatfs(c *C) {
	a := &update.Assumptions{}

	path := "/etc"
	fd := mylog.Check2(s.sys.Open(path, syscall.O_DIRECTORY, 0))

	defer s.sys.Close(fd)

	s.sys.InsertFault(`fstatfs 3 <ptr>`, errTesting)

	ok := mylog.Check2(a.CanWriteToDirectory(fd, path))
	c.Assert(err, ErrorMatches, `cannot fstatfs "/etc": testing`)
	c.Assert(ok, Equals, false)
}

// Errors from fstat are propagated to the caller.
func (s *trespassingSuite) TestCanWriteToDirectoryErrorsFstat(c *C) {
	a := &update.Assumptions{}

	path := "/etc"
	fd := mylog.Check2(s.sys.Open(path, syscall.O_DIRECTORY, 0))

	defer s.sys.Close(fd)

	s.sys.InsertFstatfsResult(`fstatfs 3 <ptr>`, syscall.Statfs_t{})
	s.sys.InsertFault(`fstat 3 <ptr>`, errTesting)

	ok := mylog.Check2(a.CanWriteToDirectory(fd, path))
	c.Assert(err, ErrorMatches, `cannot fstat "/etc": testing`)
	c.Assert(ok, Equals, false)
}

// RestrictionsFor, Check and LiftRestrictions

func (s *trespassingSuite) TestRestrictionsForEtc(c *C) {
	a := &update.Assumptions{}

	// There are restrictions for writing in /etc.
	rs := a.RestrictionsFor("/etc/test.conf")
	c.Assert(rs, NotNil)

	fd := mylog.Check2(s.sys.Open("/etc", syscall.O_DIRECTORY, 0))

	defer s.sys.Close(fd)
	s.sys.InsertFstatfsResult(`fstatfs 3 <ptr>`, syscall.Statfs_t{Type: update.Ext4Magic})
	s.sys.InsertFstatResult(`fstat 3 <ptr>`, syscall.Stat_t{})
	mylog.

		// Check reports trespassing error, restrictions may be lifted though.
		Check(rs.Check(fd, "/etc"))
	c.Assert(err, ErrorMatches, `cannot write to "/etc/test.conf" because it would affect the host in "/etc"`)
	c.Assert(err.(*update.TrespassingError).ViolatedPath, Equals, "/etc")
	c.Assert(err.(*update.TrespassingError).DesiredPath, Equals, "/etc/test.conf")

	rs.Lift()
	c.Assert(rs.Check(fd, "/etc"), IsNil)
}

// Check returns errors from lower layers.
func (s *trespassingSuite) TestRestrictionsForErrors(c *C) {
	a := &update.Assumptions{}

	rs := a.RestrictionsFor("/etc/test.conf")
	c.Assert(rs, NotNil)

	fd := mylog.Check2(s.sys.Open("/etc", syscall.O_DIRECTORY, 0))

	defer s.sys.Close(fd)
	s.sys.InsertFault(`fstatfs 3 <ptr>`, errTesting)
	mylog.Check(rs.Check(fd, "/etc"))
	c.Assert(err, ErrorMatches, `cannot fstatfs "/etc": testing`)
}

func (s *trespassingSuite) TestRestrictionsForVarSnap(c *C) {
	a := &update.Assumptions{}
	a.AddUnrestrictedPaths("/var/snap")

	// There are no restrictions in $SNAP_COMMON.
	rs := a.RestrictionsFor("/var/snap/foo/common/test.conf")
	c.Assert(rs, IsNil)

	// Nil restrictions have working Check and Lift methods.
	c.Assert(rs.Check(3, "unused"), IsNil)
	rs.Lift()
}

func (s *trespassingSuite) TestRestrictionsForRunSystemd(c *C) {
	a := &update.Assumptions{}
	a.AddUnrestrictedPaths("/run/systemd")

	// There should be no restrictions under /run/systemd
	rs := a.RestrictionsFor("/run/systemd/journal")
	c.Assert(rs, IsNil)
	rs = a.RestrictionsFor("/run/systemd/journal.namespace")
	c.Assert(rs, IsNil)

	// however we should still disallow anything else under /run
	rs = a.RestrictionsFor("/run/test.txt")
	c.Assert(rs, NotNil)

	fd := mylog.Check2(s.sys.Open("/run", syscall.O_DIRECTORY, 0))

	defer s.sys.Close(fd)
	s.sys.InsertFstatfsResult(`fstatfs 3 <ptr>`, syscall.Statfs_t{Type: update.Ext4Magic})
	s.sys.InsertFstatResult(`fstat 3 <ptr>`, syscall.Stat_t{})
	mylog.Check(rs.Check(fd, "/run"))
	c.Assert(err, ErrorMatches, `cannot write to "/run/test.txt" because it would affect the host in "/run"`)
	c.Assert(err.(*update.TrespassingError).ViolatedPath, Equals, "/run")
	c.Assert(err.(*update.TrespassingError).DesiredPath, Equals, "/run/test.txt")

	rs.Lift()
	c.Assert(rs.Check(fd, "/run"), IsNil)
}

func (s *trespassingSuite) TestRestrictionsForRootfsEntries(c *C) {
	a := &update.Assumptions{}

	// The root directory is special, it's not a trespassing error we can
	// recover from because we cannot construct a writable mimic for the root
	// directory today.
	rs := a.RestrictionsFor("/foo.conf")

	fd := mylog.Check2(s.sys.Open("/", syscall.O_DIRECTORY, 0))

	defer s.sys.Close(fd)
	s.sys.InsertFstatfsResult(`fstatfs 3 <ptr>`, syscall.Statfs_t{Type: update.Ext4Magic})
	s.sys.InsertFstatResult(`fstat 3 <ptr>`, syscall.Stat_t{})

	// Nil restrictions have working Check and Lift methods.
	c.Assert(rs.Check(fd, "/"), ErrorMatches, `cannot recover from trespassing over /`)
}

// isReadOnly

func (s *trespassingSuite) TestIsReadOnlySquashfsMountedRo(c *C) {
	path := "/some/path"
	statfs := &syscall.Statfs_t{Type: update.SquashfsMagic, Flags: update.StReadOnly}
	result := update.IsReadOnly(path, statfs)
	c.Assert(result, Equals, true)
}

func (s *trespassingSuite) TestIsReadOnlySquashfsMountedRw(c *C) {
	path := "/some/path"
	statfs := &syscall.Statfs_t{Type: update.SquashfsMagic}
	result := update.IsReadOnly(path, statfs)
	c.Assert(result, Equals, true)
}

func (s *trespassingSuite) TestIsReadOnlyExt4MountedRw(c *C) {
	path := "/some/path"
	statfs := &syscall.Statfs_t{Type: update.Ext4Magic}
	result := update.IsReadOnly(path, statfs)
	c.Assert(result, Equals, false)
}

// isSnapdCreatedPrivateTmpfs

func (s *trespassingSuite) TestIsPrivateTmpfsCreatedBySnapdNotATmpfs(c *C) {
	path := "/some/path"
	// An ext4 (which is not a tmpfs) is not a private tmpfs.
	statfs := &syscall.Statfs_t{Type: update.Ext4Magic}
	stat := &syscall.Stat_t{}
	result := update.IsPrivateTmpfsCreatedBySnapd(path, statfs, stat, nil)
	c.Assert(result, Equals, false)
}

func (s *trespassingSuite) TestIsPrivateTmpfsCreatedBySnapdNotTrusted(c *C) {
	path := "/some/path"
	// A tmpfs is not private if it doesn't come from a change we made.
	statfs := &syscall.Statfs_t{Type: update.TmpfsMagic}
	stat := &syscall.Stat_t{}
	result := update.IsPrivateTmpfsCreatedBySnapd(path, statfs, stat, nil)
	c.Assert(result, Equals, false)
}

func (s *trespassingSuite) TestIsPrivateTmpfsCreatedBySnapdViaChanges(c *C) {
	path := "/some/path"
	// A tmpfs is private because it was mounted by snap-update-ns.
	statfs := &syscall.Statfs_t{Type: update.TmpfsMagic}
	stat := &syscall.Stat_t{}

	// A tmpfs was mounted in the past so it is private.
	result := update.IsPrivateTmpfsCreatedBySnapd(path, statfs, stat, []*update.Change{
		{Action: update.Mount, Entry: osutil.MountEntry{Name: "tmpfs", Dir: path, Type: "tmpfs"}},
	})
	c.Assert(result, Equals, true)

	// A tmpfs was mounted but then it was unmounted so it is not private anymore.
	result = update.IsPrivateTmpfsCreatedBySnapd(path, statfs, stat, []*update.Change{
		{Action: update.Mount, Entry: osutil.MountEntry{Name: "tmpfs", Dir: path, Type: "tmpfs"}},
		{Action: update.Unmount, Entry: osutil.MountEntry{Name: "tmpfs", Dir: path, Type: "tmpfs"}},
	})
	c.Assert(result, Equals, false)

	// Finally, after the mounting and unmounting the tmpfs was mounted again.
	result = update.IsPrivateTmpfsCreatedBySnapd(path, statfs, stat, []*update.Change{
		{Action: update.Mount, Entry: osutil.MountEntry{Name: "tmpfs", Dir: path, Type: "tmpfs"}},
		{Action: update.Unmount, Entry: osutil.MountEntry{Name: "tmpfs", Dir: path, Type: "tmpfs"}},
		{Action: update.Mount, Entry: osutil.MountEntry{Name: "tmpfs", Dir: path, Type: "tmpfs"}},
	})
	c.Assert(result, Equals, true)
}

func (s *trespassingSuite) TestIsPrivateTmpfsCreatedBySnapdDeeper(c *C) {
	path := "/some/path/below"
	// A tmpfs is not private beyond the exact mount point from a change.
	// That is, sub-directories of a private tmpfs are not recognized as private.
	statfs := &syscall.Statfs_t{Type: update.TmpfsMagic}
	stat := &syscall.Stat_t{}
	result := update.IsPrivateTmpfsCreatedBySnapd(path, statfs, stat, []*update.Change{
		{Action: update.Mount, Entry: osutil.MountEntry{Name: "tmpfs", Dir: "/some/path", Type: "tmpfs"}},
	})
	c.Assert(result, Equals, false)
}
