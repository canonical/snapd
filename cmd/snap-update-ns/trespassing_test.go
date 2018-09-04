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
	fd, err := s.sys.Open(path, syscall.O_DIRECTORY, 0)
	c.Assert(err, IsNil)
	defer s.sys.Close(fd)

	s.sys.InsertFstatfsResult(`fstatfs 3 <ptr>`, syscall.Statfs_t{Type: update.Ext4Magic})

	ok, err := a.CanWriteToDirectory(fd, path)
	c.Assert(err, IsNil)
	c.Assert(ok, Equals, false)
}

// We are allowed to write to ext4 that was mounted read-only.
func (s *trespassingSuite) TestCanWriteToDirectoryReadOnlyExt4(c *C) {
	a := &update.Assumptions{}

	path := "/etc"
	fd, err := s.sys.Open(path, syscall.O_DIRECTORY, 0)
	c.Assert(err, IsNil)
	defer s.sys.Close(fd)

	s.sys.InsertFstatfsResult(`fstatfs 3 <ptr>`, syscall.Statfs_t{Type: update.Ext4Magic, Flags: update.StReadOnly})

	ok, err := a.CanWriteToDirectory(fd, path)
	c.Assert(err, IsNil)
	c.Assert(ok, Equals, true)
}

// We are not allowed to write to tmpfs.
func (s *trespassingSuite) TestCanWriteToDirectoryTmpfs(c *C) {
	a := &update.Assumptions{}

	path := "/etc"
	fd, err := s.sys.Open(path, syscall.O_DIRECTORY, 0)
	c.Assert(err, IsNil)
	defer s.sys.Close(fd)

	s.sys.InsertFstatfsResult(`fstatfs 3 <ptr>`, syscall.Statfs_t{Type: update.TmpfsMagic})

	ok, err := a.CanWriteToDirectory(fd, path)
	c.Assert(err, IsNil)
	c.Assert(ok, Equals, false)
}

// We allowed allowed to write to tmpfs that was mounted by snapd.
func (s *trespassingSuite) TestCanWriteToDirectoryTmpfsMountedBySnapd(c *C) {
	a := &update.Assumptions{}

	path := "/etc"
	fd, err := s.sys.Open(path, syscall.O_DIRECTORY, 0)
	c.Assert(err, IsNil)
	defer s.sys.Close(fd)

	s.sys.InsertFstatfsResult(`fstatfs 3 <ptr>`, syscall.Statfs_t{Type: update.TmpfsMagic})

	a.AddChange(&update.Change{
		Action: update.Mount,
		Entry:  osutil.MountEntry{Type: "tmpfs", Dir: path}})

	ok, err := a.CanWriteToDirectory(fd, path)
	c.Assert(err, IsNil)
	c.Assert(ok, Equals, true)
}

// We allowed allowed to write to an unrestricted path.
func (s *trespassingSuite) TestCanWriteToDirectoryUnrestricted(c *C) {
	a := &update.Assumptions{}

	path := "/var/snap/foo/common"
	fd, err := s.sys.Open(path, syscall.O_DIRECTORY, 0)
	c.Assert(err, IsNil)
	defer s.sys.Close(fd)

	s.sys.InsertFstatfsResult(`fstatfs 3 <ptr>`, syscall.Statfs_t{Type: update.Ext4Magic})

	a.AddUnrestrictedPaths(path)

	ok, err := a.CanWriteToDirectory(fd, path)
	c.Assert(err, IsNil)
	c.Assert(ok, Equals, true)
}

// Errors are propagated to the caller.
func (s *trespassingSuite) TestCanWriteToDirectoryErrors(c *C) {
	a := &update.Assumptions{}

	path := "/etc"
	fd, err := s.sys.Open(path, syscall.O_DIRECTORY, 0)
	c.Assert(err, IsNil)
	defer s.sys.Close(fd)

	s.sys.InsertFault(`fstatfs 3 <ptr>`, errTesting)

	ok, err := a.CanWriteToDirectory(fd, path)
	c.Assert(err, ErrorMatches, `cannot fstatfs "/etc": testing`)
	c.Assert(ok, Equals, false)
}

// RestrictionsFor, Check and LiftRestrictions

func (s *trespassingSuite) TestRestrictionsForEtc(c *C) {
	a := &update.Assumptions{}

	// There are restrictions for writing in /etc.
	rs := a.RestrictionsFor("/etc/test.conf")
	c.Assert(rs, NotNil)

	fd, err := s.sys.Open("/etc", syscall.O_DIRECTORY, 0)
	c.Assert(err, IsNil)
	defer s.sys.Close(fd)
	s.sys.InsertFstatfsResult(`fstatfs 3 <ptr>`, syscall.Statfs_t{Type: update.Ext4Magic})

	// Check reports trespassing error, restrictions may be lifted though.
	err = rs.Check(fd, "/etc")
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

	fd, err := s.sys.Open("/etc", syscall.O_DIRECTORY, 0)
	c.Assert(err, IsNil)
	defer s.sys.Close(fd)
	s.sys.InsertFault(`fstatfs 3 <ptr>`, errTesting)

	err = rs.Check(fd, "/etc")
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

func (s *trespassingSuite) TestRestrictionsForRootfsEntries(c *C) {
	a := &update.Assumptions{}

	// The root directory is special, it's not a trespassing error we can
	// recover from because we cannot construct a writable mimic for the root
	// directory today.
	rs := a.RestrictionsFor("/foo.conf")

	fd, err := s.sys.Open("/", syscall.O_DIRECTORY, 0)
	c.Assert(err, IsNil)
	defer s.sys.Close(fd)
	s.sys.InsertFstatfsResult(`fstatfs 3 <ptr>`, syscall.Statfs_t{Type: update.Ext4Magic})

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

func (s *trespassingSuite) TestIsSnapdCreatedPrivateTmpfsNotATmpfs(c *C) {
	path := "/some/path"
	// An ext4 (which is not a tmpfs) is not a private tmpfs.
	statfs := &syscall.Statfs_t{Type: update.Ext4Magic}
	result := update.IsSnapdCreatedPrivateTmpfs(path, statfs, nil)
	c.Assert(result, Equals, false)
}

func (s *trespassingSuite) TestIsSnapdCreatedPrivateTmpfsNotTrusted(c *C) {
	path := "/some/path"
	// A tmpfs is not private if it doesn't come from a change we made.
	statfs := &syscall.Statfs_t{Type: update.TmpfsMagic}
	result := update.IsSnapdCreatedPrivateTmpfs(path, statfs, nil)
	c.Assert(result, Equals, false)
}

func (s *trespassingSuite) TestIsSnapdCreatedPrivateTmpfsViaChanges(c *C) {
	path := "/some/path"
	// A tmpfs is private because it was mounted by snap-update-ns.
	statfs := &syscall.Statfs_t{Type: update.TmpfsMagic}

	// A tmpfs was mounted in the past so it is private.
	result := update.IsSnapdCreatedPrivateTmpfs(path, statfs, []*update.Change{
		{Action: update.Mount, Entry: osutil.MountEntry{Name: "tmpfs", Dir: path, Type: "tmpfs"}},
	})
	c.Assert(result, Equals, true)

	// A tmpfs was mounted but then it was unmounted so it is not private anymore.
	result = update.IsSnapdCreatedPrivateTmpfs(path, statfs, []*update.Change{
		{Action: update.Mount, Entry: osutil.MountEntry{Name: "tmpfs", Dir: path, Type: "tmpfs"}},
		{Action: update.Unmount, Entry: osutil.MountEntry{Name: "tmpfs", Dir: path, Type: "tmpfs"}},
	})
	c.Assert(result, Equals, false)

	// Finally, after the mounting and unmounting the tmpfs was mounted again.
	result = update.IsSnapdCreatedPrivateTmpfs(path, statfs, []*update.Change{
		{Action: update.Mount, Entry: osutil.MountEntry{Name: "tmpfs", Dir: path, Type: "tmpfs"}},
		{Action: update.Unmount, Entry: osutil.MountEntry{Name: "tmpfs", Dir: path, Type: "tmpfs"}},
		{Action: update.Mount, Entry: osutil.MountEntry{Name: "tmpfs", Dir: path, Type: "tmpfs"}},
	})
	c.Assert(result, Equals, true)
}

func (s *trespassingSuite) TestIsSnapdCreatedPrivateTmpfsDeeper(c *C) {
	path := "/some/path/below"
	// A tmpfs is not private beyond the exact mount point from a change.
	// That is, sub-directories of a private tmpfs are not recognized as private.
	statfs := &syscall.Statfs_t{Type: update.TmpfsMagic}
	result := update.IsSnapdCreatedPrivateTmpfs(path, statfs, []*update.Change{
		{Action: update.Mount, Entry: osutil.MountEntry{Name: "tmpfs", Dir: "/some/path", Type: "tmpfs"}},
	})
	c.Assert(result, Equals, false)
}

func (s *trespassingSuite) TestIsSnapdCreatedPrivateTmpfsViaVarLib(c *C) {
	path := "/var/lib"
	// A tmpfs in /var/lib is private because it is a special
	// quirk applied by snap-confine, without having a change record.
	statfs := &syscall.Statfs_t{Type: update.TmpfsMagic}
	result := update.IsSnapdCreatedPrivateTmpfs(path, statfs, nil)
	c.Assert(result, Equals, true)
}
