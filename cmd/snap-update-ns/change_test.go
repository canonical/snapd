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
	"errors"
	"os"
	"syscall"

	. "gopkg.in/check.v1"

	update "github.com/snapcore/snapd/cmd/snap-update-ns"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/testutil"
)

type changeSuite struct {
	testutil.BaseTest
	sys *testutil.SyscallRecorder
	sec *update.Secure
}

var (
	errTesting = errors.New("testing")
)

var _ = Suite(&changeSuite{})

func (s *changeSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	// Mock and record system interactions.
	s.sys = &testutil.SyscallRecorder{}
	s.BaseTest.AddCleanup(update.MockSystemCalls(s.sys))
	s.sec = &update.Secure{}
}

func (s *changeSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
	s.sys.CheckForStrayDescriptors(c)
}

func (s *changeSuite) TestFakeFileInfo(c *C) {
	c.Assert(testutil.FileInfoDir.IsDir(), Equals, true)
	c.Assert(testutil.FileInfoFile.IsDir(), Equals, false)
	c.Assert(testutil.FileInfoSymlink.IsDir(), Equals, false)
}

func (s *changeSuite) TestString(c *C) {
	change := update.Change{
		Entry:  osutil.MountEntry{Dir: "/a/b", Name: "/dev/sda1"},
		Action: update.Mount,
	}
	c.Assert(change.String(), Equals, "mount (/dev/sda1 /a/b none defaults 0 0)")
}

// When there are no profiles we don't do anything.
func (s *changeSuite) TestNeededChangesNoProfiles(c *C) {
	current := &osutil.MountProfile{}
	desired := &osutil.MountProfile{}
	changes := update.NeededChanges(current, desired)
	c.Assert(changes, IsNil)
}

// When the profiles are the same we don't do anything.
func (s *changeSuite) TestNeededChangesNoChange(c *C) {
	current := &osutil.MountProfile{Entries: []osutil.MountEntry{{Dir: "/common/stuff"}}}
	desired := &osutil.MountProfile{Entries: []osutil.MountEntry{{Dir: "/common/stuff"}}}
	changes := update.NeededChanges(current, desired)
	c.Assert(changes, DeepEquals, []*update.Change{
		{Entry: osutil.MountEntry{Dir: "/common/stuff"}, Action: update.Keep},
	})
}

// When the content interface is connected we should mount the new entry.
func (s *changeSuite) TestNeededChangesTrivialMount(c *C) {
	current := &osutil.MountProfile{}
	desired := &osutil.MountProfile{Entries: []osutil.MountEntry{{Dir: "/common/stuff"}}}
	changes := update.NeededChanges(current, desired)
	c.Assert(changes, DeepEquals, []*update.Change{
		{Entry: desired.Entries[0], Action: update.Mount},
	})
}

// When the content interface is disconnected we should unmount the mounted entry.
func (s *changeSuite) TestNeededChangesTrivialUnmount(c *C) {
	current := &osutil.MountProfile{Entries: []osutil.MountEntry{{Dir: "/common/stuff"}}}
	desired := &osutil.MountProfile{}
	changes := update.NeededChanges(current, desired)
	c.Assert(changes, DeepEquals, []*update.Change{
		{Entry: current.Entries[0], Action: update.Unmount},
	})
}

// When umounting we unmount children before parents.
func (s *changeSuite) TestNeededChangesUnmountOrder(c *C) {
	current := &osutil.MountProfile{Entries: []osutil.MountEntry{
		{Dir: "/common/stuff/extra"},
		{Dir: "/common/stuff"},
	}}
	desired := &osutil.MountProfile{}
	changes := update.NeededChanges(current, desired)
	c.Assert(changes, DeepEquals, []*update.Change{
		{Entry: osutil.MountEntry{Dir: "/common/stuff/extra"}, Action: update.Unmount},
		{Entry: osutil.MountEntry{Dir: "/common/stuff"}, Action: update.Unmount},
	})
}

// When mounting we mount the parents before the children.
func (s *changeSuite) TestNeededChangesMountOrder(c *C) {
	current := &osutil.MountProfile{}
	desired := &osutil.MountProfile{Entries: []osutil.MountEntry{
		{Dir: "/common/stuff/extra"},
		{Dir: "/common/stuff"},
	}}
	changes := update.NeededChanges(current, desired)
	c.Assert(changes, DeepEquals, []*update.Change{
		{Entry: osutil.MountEntry{Dir: "/common/stuff"}, Action: update.Mount},
		{Entry: osutil.MountEntry{Dir: "/common/stuff/extra"}, Action: update.Mount},
	})
}

// When parent changes we don't reuse its children
func (s *changeSuite) TestNeededChangesChangedParentSameChild(c *C) {
	current := &osutil.MountProfile{Entries: []osutil.MountEntry{
		{Dir: "/common/stuff", Name: "/dev/sda1"},
		{Dir: "/common/stuff/extra"},
		{Dir: "/common/unrelated"},
	}}
	desired := &osutil.MountProfile{Entries: []osutil.MountEntry{
		{Dir: "/common/stuff", Name: "/dev/sda2"},
		{Dir: "/common/stuff/extra"},
		{Dir: "/common/unrelated"},
	}}
	changes := update.NeededChanges(current, desired)
	c.Assert(changes, DeepEquals, []*update.Change{
		{Entry: osutil.MountEntry{Dir: "/common/unrelated"}, Action: update.Keep},
		{Entry: osutil.MountEntry{Dir: "/common/stuff/extra"}, Action: update.Unmount},
		{Entry: osutil.MountEntry{Dir: "/common/stuff", Name: "/dev/sda1"}, Action: update.Unmount},
		{Entry: osutil.MountEntry{Dir: "/common/stuff", Name: "/dev/sda2"}, Action: update.Mount},
		{Entry: osutil.MountEntry{Dir: "/common/stuff/extra"}, Action: update.Mount},
	})
}

// When child changes we don't touch the unchanged parent
func (s *changeSuite) TestNeededChangesSameParentChangedChild(c *C) {
	current := &osutil.MountProfile{Entries: []osutil.MountEntry{
		{Dir: "/common/stuff"},
		{Dir: "/common/stuff/extra", Name: "/dev/sda1"},
		{Dir: "/common/unrelated"},
	}}
	desired := &osutil.MountProfile{Entries: []osutil.MountEntry{
		{Dir: "/common/stuff"},
		{Dir: "/common/stuff/extra", Name: "/dev/sda2"},
		{Dir: "/common/unrelated"},
	}}
	changes := update.NeededChanges(current, desired)
	c.Assert(changes, DeepEquals, []*update.Change{
		{Entry: osutil.MountEntry{Dir: "/common/unrelated"}, Action: update.Keep},
		{Entry: osutil.MountEntry{Dir: "/common/stuff/extra", Name: "/dev/sda1"}, Action: update.Unmount},
		{Entry: osutil.MountEntry{Dir: "/common/stuff"}, Action: update.Keep},
		{Entry: osutil.MountEntry{Dir: "/common/stuff/extra", Name: "/dev/sda2"}, Action: update.Mount},
	})
}

// Unused bind mount farms are unmounted.
func (s *changeSuite) TestNeededChangesTmpfsBindMountFarmUnused(c *C) {
	current := &osutil.MountProfile{Entries: []osutil.MountEntry{{
		// The tmpfs that lets us write into immutable squashfs. We mock
		// x-snapd.needed-by to the last entry in the current profile (the bind
		// mount). Mark it synthetic since it is a helper mount that is needed
		// to facilitate the following mounts.
		Name:    "tmpfs",
		Dir:     "/snap/name/42/subdir",
		Type:    "tmpfs",
		Options: []string{"x-snapd.needed-by=/snap/name/42/subdir", "x-snapd.synthetic"},
	}, {
		// A bind mount to preserve a directory hidden by the tmpfs (the mount
		// point is created elsewhere). We mock x-snapd.needed-by to the
		// location of the bind mount below that is no longer desired.
		Name:    "/var/lib/snapd/hostfs/snap/name/42/subdir/existing",
		Dir:     "/snap/name/42/subdir/existing",
		Options: []string{"bind", "ro", "x-snapd.needed-by=/snap/name/42/subdir", "x-snapd.synthetic"},
	}, {
		// A bind mount to put some content from another snap. The bind mount
		// is nothing special but the fact that it is possible is the reason
		// the two entries above exist. The mount point (created) is created
		// elsewhere.
		Name:    "/snap/other/123/libs",
		Dir:     "/snap/name/42/subdir/created",
		Options: []string{"bind", "ro"},
	}}}

	desired := &osutil.MountProfile{}

	changes := update.NeededChanges(current, desired)

	c.Assert(changes, DeepEquals, []*update.Change{
		{Entry: osutil.MountEntry{
			Name:    "/var/lib/snapd/hostfs/snap/name/42/subdir/existing",
			Dir:     "/snap/name/42/subdir/existing",
			Options: []string{"bind", "ro", "x-snapd.needed-by=/snap/name/42/subdir", "x-snapd.synthetic"},
		}, Action: update.Unmount},
		{Entry: osutil.MountEntry{
			Name:    "/snap/other/123/libs",
			Dir:     "/snap/name/42/subdir/created",
			Options: []string{"bind", "ro"},
		}, Action: update.Unmount},
		{Entry: osutil.MountEntry{
			Name:    "tmpfs",
			Dir:     "/snap/name/42/subdir",
			Type:    "tmpfs",
			Options: []string{"x-snapd.needed-by=/snap/name/42/subdir", "x-snapd.synthetic"},
		}, Action: update.Unmount},
	})
}

func (s *changeSuite) TestNeededChangesTmpfsBindMountFarmUsed(c *C) {
	// NOTE: the current profile is the same as in the test
	// TestNeededChangesTmpfsBindMountFarmUnused written above.
	current := &osutil.MountProfile{Entries: []osutil.MountEntry{{
		Name:    "tmpfs",
		Dir:     "/snap/name/42/subdir",
		Type:    "tmpfs",
		Options: []string{"x-snapd.needed-by=/snap/name/42/subdir/created", "x-snapd.synthetic"},
	}, {
		Name:    "/var/lib/snapd/hostfs/snap/name/42/subdir/existing",
		Dir:     "/snap/name/42/subdir/existing",
		Options: []string{"bind", "ro", "x-snapd.needed-by=/snap/name/42/subdir/created", "x-snapd.synthetic"},
	}, {
		Name:    "/snap/other/123/libs",
		Dir:     "/snap/name/42/subdir/created",
		Options: []string{"bind", "ro"},
	}}}

	desired := &osutil.MountProfile{Entries: []osutil.MountEntry{{
		// This is the only entry that we explicitly want but in order to
		// support it we need to keep the remaining implicit entries.
		Name:    "/snap/other/123/libs",
		Dir:     "/snap/name/42/subdir/created",
		Options: []string{"bind", "ro"},
	}}}

	changes := update.NeededChanges(current, desired)

	c.Assert(changes, DeepEquals, []*update.Change{
		{Entry: osutil.MountEntry{
			Name:    "/var/lib/snapd/hostfs/snap/name/42/subdir/existing",
			Dir:     "/snap/name/42/subdir/existing",
			Options: []string{"bind", "ro", "x-snapd.needed-by=/snap/name/42/subdir/created", "x-snapd.synthetic"},
		}, Action: update.Keep},
		{Entry: osutil.MountEntry{
			Name:    "/snap/other/123/libs",
			Dir:     "/snap/name/42/subdir/created",
			Options: []string{"bind", "ro"},
		}, Action: update.Keep},
		{Entry: osutil.MountEntry{
			Name:    "tmpfs",
			Dir:     "/snap/name/42/subdir",
			Type:    "tmpfs",
			Options: []string{"x-snapd.needed-by=/snap/name/42/subdir/created", "x-snapd.synthetic"},
		}, Action: update.Keep},
	})
}

// cur = ['/a/b', '/a/b-1', '/a/b-1/3', '/a/b/c']
// des = ['/a/b', '/a/b-1', '/a/b/c'
//
// We are smart about comparing entries as directories. Here even though "/a/b"
// is a prefix of "/a/b-1" it is correctly reused.
func (s *changeSuite) TestNeededChangesSmartEntryComparison(c *C) {
	current := &osutil.MountProfile{Entries: []osutil.MountEntry{
		{Dir: "/a/b", Name: "/dev/sda1"},
		{Dir: "/a/b-1"},
		{Dir: "/a/b-1/3"},
		{Dir: "/a/b/c"},
	}}
	desired := &osutil.MountProfile{Entries: []osutil.MountEntry{
		{Dir: "/a/b", Name: "/dev/sda2"},
		{Dir: "/a/b-1"},
		{Dir: "/a/b/c"},
	}}
	changes := update.NeededChanges(current, desired)
	c.Assert(changes, DeepEquals, []*update.Change{
		{Entry: osutil.MountEntry{Dir: "/a/b/c"}, Action: update.Unmount},
		{Entry: osutil.MountEntry{Dir: "/a/b", Name: "/dev/sda1"}, Action: update.Unmount},
		{Entry: osutil.MountEntry{Dir: "/a/b-1/3"}, Action: update.Unmount},
		{Entry: osutil.MountEntry{Dir: "/a/b-1"}, Action: update.Keep},

		{Entry: osutil.MountEntry{Dir: "/a/b", Name: "/dev/sda2"}, Action: update.Mount},
		{Entry: osutil.MountEntry{Dir: "/a/b/c"}, Action: update.Mount},
	})
}

// ########################################
// Topic: mounting & unmounting filesystems
// ########################################

// Change.Perform returns errors from os.Lstat (apart from ErrNotExist)
func (s *changeSuite) TestPerformFilesystemMountLstatError(c *C) {
	s.sys.InsertFault(`lstat "/target"`, errTesting)
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "device", Dir: "/target", Type: "type"}}
	synth, err := chg.Perform(s.sec)
	c.Assert(err, ErrorMatches, `cannot inspect "/target": testing`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/target"`, E: errTesting},
	})
}

// Change.Perform wants to mount a filesystem.
func (s *changeSuite) TestPerformFilesystemMount(c *C) {
	s.sys.InsertOsLstatResult(`lstat "/target"`, testutil.FileInfoDir)
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "device", Dir: "/target", Type: "type"}}
	synth, err := chg.Perform(s.sec)
	c.Assert(err, IsNil)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/target"`, R: testutil.FileInfoDir},
		{C: `mount "device" "/target" "type" 0 ""`},
	})
}

// Change.Perform wants to mount a filesystem but it fails.
func (s *changeSuite) TestPerformFilesystemMountWithError(c *C) {
	s.sys.InsertOsLstatResult(`lstat "/target"`, testutil.FileInfoDir)
	s.sys.InsertFault(`mount "device" "/target" "type" 0 ""`, errTesting)
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "device", Dir: "/target", Type: "type"}}
	synth, err := chg.Perform(s.sec)
	c.Assert(err, Equals, errTesting)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/target"`, R: testutil.FileInfoDir},
		{C: `mount "device" "/target" "type" 0 ""`, E: errTesting},
	})
}

// Change.Perform wants to mount a filesystem but the mount point isn't there.
func (s *changeSuite) TestPerformFilesystemMountWithoutMountPoint(c *C) {
	s.sys.InsertFault(`lstat "/target"`, syscall.ENOENT)
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "device", Dir: "/target", Type: "type"}}
	synth, err := chg.Perform(s.sec)
	c.Assert(err, IsNil)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/target"`, E: syscall.ENOENT},
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "target" 0755`},
		{C: `openat 3 "target" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `fchown 4 0 0`},
		{C: `close 4`},
		{C: `close 3`},
		{C: `mount "device" "/target" "type" 0 ""`},
	})
}

// Change.Perform wants to create a filesystem but the mount point isn't there and cannot be created.
func (s *changeSuite) TestPerformFilesystemMountWithoutMountPointWithErrors(c *C) {
	s.sys.InsertFault(`lstat "/target"`, syscall.ENOENT)
	s.sys.InsertFault(`mkdirat 3 "target" 0755`, errTesting)
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "device", Dir: "/target", Type: "type"}}
	synth, err := chg.Perform(s.sec)
	c.Assert(err, ErrorMatches, `cannot create directory "/target": testing`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/target"`, E: syscall.ENOENT},
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "target" 0755`, E: errTesting},
		{C: `close 3`},
	})
}

// Change.Perform wants to mount a filesystem but the mount point isn't there and the parent is read-only.
func (s *changeSuite) TestPerformFilesystemMountWithoutMountPointAndReadOnlyBase(c *C) {
	s.sys.InsertFault(`lstat "/rofs/target"`, syscall.ENOENT)
	s.sys.InsertFault(`mkdirat 3 "rofs" 0755`, syscall.EEXIST)
	s.sys.InsertFault(`openat 3 "target" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, syscall.ENOENT, nil) // works on 2nd try
	s.sys.InsertFault(`mkdirat 4 "target" 0755`, syscall.EROFS, nil)                               // works on 2nd try
	s.sys.InsertSysLstatResult(`lstat "/rofs" <ptr>`, syscall.Stat_t{Uid: 0, Gid: 0, Mode: 0755})
	s.sys.InsertReadDirResult(`readdir "/rofs"`, nil) // pretend /rofs is empty.
	s.sys.InsertFault(`lstat "/tmp/.snap/rofs"`, syscall.ENOENT)
	s.sys.InsertOsLstatResult(`lstat "/rofs"`, testutil.FileInfoDir)
	s.sys.InsertFstatResult(`fstat 4 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFstatResult(`fstat 7 <ptr>`, syscall.Stat_t{})

	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "device", Dir: "/rofs/target", Type: "type"}}
	synth, err := chg.Perform(s.sec)
	c.Assert(err, IsNil)
	c.Assert(synth, DeepEquals, []*update.Change{
		{Action: update.Mount, Entry: osutil.MountEntry{
			Name: "tmpfs", Dir: "/rofs", Type: "tmpfs",
			Options: []string{"x-snapd.synthetic", "x-snapd.needed-by=/rofs/target", "mode=0755", "uid=0", "gid=0"}},
		},
	})
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		// sniff mount target
		{C: `lstat "/rofs/target"`, E: syscall.ENOENT},

		// /rofs/target is missing, create it
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "rofs" 0755`, E: syscall.EEXIST},
		{C: `openat 3 "rofs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `close 3`},
		{C: `mkdirat 4 "target" 0755`, E: syscall.EROFS},
		{C: `close 4`},

		// error, read only filesystem, create a mimic
		{C: `lstat "/rofs" <ptr>`, R: syscall.Stat_t{Uid: 0, Gid: 0, Mode: 0755}},
		{C: `readdir "/rofs"`, R: []os.FileInfo(nil)},
		{C: `lstat "/tmp/.snap/rofs"`, E: syscall.ENOENT},
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "tmp" 0755`},
		{C: `openat 3 "tmp" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `fchown 4 0 0`},
		{C: `mkdirat 4 ".snap" 0755`},
		{C: `openat 4 ".snap" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 5},
		{C: `fchown 5 0 0`},
		{C: `close 4`},
		{C: `close 3`},
		{C: `mkdirat 5 "rofs" 0755`},
		{C: `openat 5 "rofs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `fchown 3 0 0`},
		{C: `close 3`},
		{C: `close 5`},
		{C: `lstat "/rofs"`, R: testutil.FileInfoDir},

		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "rofs" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 4},
		{C: `fstat 4 <ptr>`, R: syscall.Stat_t{}},
		{C: `close 3`},
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "tmp" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 5},
		{C: `openat 5 ".snap" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 6},
		{C: `openat 6 "rofs" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 7},
		{C: `fstat 7 <ptr>`, R: syscall.Stat_t{}},
		{C: `close 6`},
		{C: `close 5`},
		{C: `close 3`},
		{C: `mount "/proc/self/fd/4" "/proc/self/fd/7" "" MS_BIND|MS_REC ""`},
		{C: `close 7`},
		{C: `close 4`},

		{C: `lstat "/rofs"`, R: testutil.FileInfoDir},
		{C: `mount "tmpfs" "/rofs" "tmpfs" 0 "mode=0755,uid=0,gid=0"`},
		{C: `unmount "/tmp/.snap/rofs" UMOUNT_NOFOLLOW|MNT_DETACH`},

		// mimic ready, re-try initial mkdir
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "rofs" 0755`, E: syscall.EEXIST},
		{C: `openat 3 "rofs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `close 3`},
		{C: `mkdirat 4 "target" 0755`},
		{C: `openat 4 "target" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `fchown 3 0 0`},
		{C: `close 3`},
		{C: `close 4`},

		// mount the filesystem
		{C: `mount "device" "/rofs/target" "type" 0 ""`},
	})
}

// Change.Perform wants to mount a filesystem but the mount point isn't there and the parent is read-only and mimic fails during planning.
func (s *changeSuite) TestPerformFilesystemMountWithoutMountPointAndReadOnlyBaseErrorWhilePlanning(c *C) {
	s.sys.InsertFault(`lstat "/rofs/target"`, syscall.ENOENT)
	s.sys.InsertFault(`mkdirat 3 "rofs" 0755`, syscall.EEXIST)
	s.sys.InsertFault(`openat 3 "target" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, syscall.ENOENT)
	s.sys.InsertFault(`mkdirat 4 "target" 0755`, syscall.EROFS)
	s.sys.InsertFault(`lstat "/tmp/.snap/rofs"`, syscall.ENOENT)
	s.sys.InsertOsLstatResult(`lstat "/rofs"`, testutil.FileInfoDir)
	s.sys.InsertSysLstatResult(`lstat "/rofs" <ptr>`, syscall.Stat_t{Uid: 0, Gid: 0, Mode: 0755})
	s.sys.InsertReadDirResult(`readdir "/rofs"`, nil)
	s.sys.InsertFault(`readdir "/rofs"`, errTesting) // make the writable mimic fail

	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "device", Dir: "/rofs/target", Type: "type"}}
	synth, err := chg.Perform(s.sec)
	c.Assert(err, ErrorMatches, `cannot create writable mimic over "/rofs": testing`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		// sniff mount target
		{C: `lstat "/rofs/target"`, E: syscall.ENOENT},

		// /rofs/target is missing, create it
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "rofs" 0755`, E: syscall.EEXIST},
		{C: `openat 3 "rofs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `close 3`},
		{C: `mkdirat 4 "target" 0755`, E: syscall.EROFS},
		{C: `close 4`},

		// error, read only filesystem, create a mimic
		{C: `lstat "/rofs" <ptr>`, R: syscall.Stat_t{Uid: 0, Gid: 0, Mode: 0755}},
		{C: `readdir "/rofs"`, E: errTesting},
		// cannot create mimic, that's it
	})
}

// Change.Perform wants to mount a filesystem but the mount point isn't there and the parent is read-only and mimic fails during execution.
func (s *changeSuite) TestPerformFilesystemMountWithoutMountPointAndReadOnlyBaseErrorWhileExecuting(c *C) {
	s.sys.InsertFault(`lstat "/rofs/target"`, syscall.ENOENT)
	s.sys.InsertFault(`mkdirat 3 "rofs" 0755`, syscall.EEXIST)
	s.sys.InsertFault(`openat 3 "target" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, syscall.ENOENT)
	s.sys.InsertFault(`mkdirat 4 "target" 0755`, syscall.EROFS)
	s.sys.InsertFault(`lstat "/tmp/.snap/rofs"`, syscall.ENOENT)
	s.sys.InsertOsLstatResult(`lstat "/rofs"`, testutil.FileInfoDir)
	s.sys.InsertSysLstatResult(`lstat "/rofs" <ptr>`, syscall.Stat_t{Uid: 0, Gid: 0, Mode: 0755})
	s.sys.InsertReadDirResult(`readdir "/rofs"`, nil)
	s.sys.InsertFault(`mkdirat 4 ".snap" 0755`, errTesting) // make the writable mimic fail

	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "device", Dir: "/rofs/target", Type: "type"}}
	synth, err := chg.Perform(s.sec)
	c.Assert(err, ErrorMatches, `cannot create writable mimic over "/rofs": cannot create directory "/tmp/.snap": testing`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		// sniff mount target
		{C: `lstat "/rofs/target"`, E: syscall.ENOENT},

		// /rofs/target is missing, create it
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "rofs" 0755`, E: syscall.EEXIST},
		{C: `openat 3 "rofs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `close 3`},
		{C: `mkdirat 4 "target" 0755`, E: syscall.EROFS},
		{C: `close 4`},

		// error, read only filesystem, create a mimic
		{C: `lstat "/rofs" <ptr>`, R: syscall.Stat_t{Uid: 0, Gid: 0, Mode: 0755}},
		{C: `readdir "/rofs"`, R: []os.FileInfo(nil)},
		{C: `lstat "/tmp/.snap/rofs"`, E: syscall.ENOENT},
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "tmp" 0755`},
		{C: `openat 3 "tmp" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `fchown 4 0 0`},
		{C: `mkdirat 4 ".snap" 0755`, E: errTesting},
		{C: `close 4`},
		{C: `close 3`},
		// cannot create mimic, that's it
	})
}

// Change.Perform wants to mount a filesystem but there's a symlink in mount point.
func (s *changeSuite) TestPerformFilesystemMountWithSymlinkInMountPoint(c *C) {
	s.sys.InsertOsLstatResult(`lstat "/target"`, testutil.FileInfoSymlink)
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "device", Dir: "/target", Type: "type"}}
	synth, err := chg.Perform(s.sec)
	c.Assert(err, ErrorMatches, `cannot use "/target" as mount point: not a directory`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/target"`, R: testutil.FileInfoSymlink},
	})
}

// Change.Perform wants to mount a filesystem but there's a file in mount point.
func (s *changeSuite) TestPerformFilesystemMountWithFileInMountPoint(c *C) {
	s.sys.InsertOsLstatResult(`lstat "/target"`, testutil.FileInfoFile)
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "device", Dir: "/target", Type: "type"}}
	synth, err := chg.Perform(s.sec)
	c.Assert(err, ErrorMatches, `cannot use "/target" as mount point: not a directory`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/target"`, R: testutil.FileInfoFile},
	})
}

// Change.Perform wants to unmount a filesystem.
func (s *changeSuite) TestPerformFilesystemUnmount(c *C) {
	chg := &update.Change{Action: update.Unmount, Entry: osutil.MountEntry{Name: "device", Dir: "/target", Type: "type"}}
	synth, err := chg.Perform(s.sec)
	c.Assert(err, IsNil)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `unmount "/target" UMOUNT_NOFOLLOW`},
	})
	c.Assert(synth, HasLen, 0)
}

// Change.Perform wants to detach a bind mount.
func (s *changeSuite) TestPerformFilesystemDetch(c *C) {
	chg := &update.Change{Action: update.Unmount, Entry: osutil.MountEntry{Name: "/something", Dir: "/target", Options: []string{"x-snapd.detach"}}}
	synth, err := chg.Perform(s.sec)
	c.Assert(err, IsNil)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `unmount "/target" UMOUNT_NOFOLLOW|MNT_DETACH`},
	})
	c.Assert(synth, HasLen, 0)
}

// Change.Perform wants to unmount a filesystem but it fails.
func (s *changeSuite) TestPerformFilesystemUnmountError(c *C) {
	s.sys.InsertFault(`unmount "/target" UMOUNT_NOFOLLOW`, errTesting)
	chg := &update.Change{Action: update.Unmount, Entry: osutil.MountEntry{Name: "device", Dir: "/target", Type: "type"}}
	synth, err := chg.Perform(s.sec)
	c.Assert(err, Equals, errTesting)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `unmount "/target" UMOUNT_NOFOLLOW`, E: errTesting},
	})
	c.Assert(synth, HasLen, 0)
}

// Change.Perform passes non-flag options to the kernel.
func (s *changeSuite) TestPerformFilesystemMountWithOptions(c *C) {
	s.sys.InsertOsLstatResult(`lstat "/target"`, testutil.FileInfoDir)
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "device", Dir: "/target", Type: "type", Options: []string{"ro", "funky"}}}
	synth, err := chg.Perform(s.sec)
	c.Assert(err, IsNil)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/target"`, R: testutil.FileInfoDir},
		{C: `mount "device" "/target" "type" MS_RDONLY "funky"`},
	})
}

// Change.Perform doesn't pass snapd-specific options to the kernel.
func (s *changeSuite) TestPerformFilesystemMountWithSnapdSpecificOptions(c *C) {
	s.sys.InsertOsLstatResult(`lstat "/target"`, testutil.FileInfoDir)
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "device", Dir: "/target", Type: "type", Options: []string{"ro", "x-snapd.funky"}}}
	synth, err := chg.Perform(s.sec)
	c.Assert(err, IsNil)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/target"`, R: testutil.FileInfoDir},
		{C: `mount "device" "/target" "type" MS_RDONLY ""`},
	})
}

// ###############################################
// Topic: bind-mounting and unmounting directories
// ###############################################

// Change.Perform wants to bind mount a directory but the target cannot be stat'ed.
func (s *changeSuite) TestPerformDirectoryBindMountTargetLstatError(c *C) {
	s.sys.InsertFault(`lstat "/target"`, errTesting)
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "/source", Dir: "/target", Options: []string{"bind"}}}
	synth, err := chg.Perform(s.sec)
	c.Assert(err, ErrorMatches, `cannot inspect "/target": testing`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/target"`, E: errTesting},
	})
}

// Change.Perform wants to bind mount a directory but the source cannot be stat'ed.
func (s *changeSuite) TestPerformDirectoryBindMountSourceLstatError(c *C) {
	s.sys.InsertOsLstatResult(`lstat "/target"`, testutil.FileInfoDir)
	s.sys.InsertFault(`lstat "/source"`, errTesting)
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "/source", Dir: "/target", Options: []string{"bind"}}}
	synth, err := chg.Perform(s.sec)
	c.Assert(err, ErrorMatches, `cannot inspect "/source": testing`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/target"`, R: testutil.FileInfoDir},
		{C: `lstat "/source"`, E: errTesting},
	})
}

// Change.Perform wants to bind mount a directory.
func (s *changeSuite) TestPerformDirectoryBindMount(c *C) {
	s.sys.InsertOsLstatResult(`lstat "/source"`, testutil.FileInfoDir)
	s.sys.InsertOsLstatResult(`lstat "/target"`, testutil.FileInfoDir)
	s.sys.InsertFstatResult(`fstat 4 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFstatResult(`fstat 5 <ptr>`, syscall.Stat_t{})
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "/source", Dir: "/target", Options: []string{"bind"}}}
	synth, err := chg.Perform(s.sec)
	c.Assert(err, IsNil)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/target"`, R: testutil.FileInfoDir},
		{C: `lstat "/source"`, R: testutil.FileInfoDir},
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "source" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 4},
		{C: `fstat 4 <ptr>`, R: syscall.Stat_t{}},
		{C: `close 3`},
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "target" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 5},
		{C: `fstat 5 <ptr>`, R: syscall.Stat_t{}},
		{C: `close 3`},
		{C: `mount "/proc/self/fd/4" "/proc/self/fd/5" "" MS_BIND ""`},
		{C: `close 5`},
		{C: `close 4`},
	})
}

// Change.Perform wants to bind mount a directory but it fails.
func (s *changeSuite) TestPerformDirectoryBindMountWithError(c *C) {
	s.sys.InsertOsLstatResult(`lstat "/target"`, testutil.FileInfoDir)
	s.sys.InsertOsLstatResult(`lstat "/source"`, testutil.FileInfoDir)
	s.sys.InsertFstatResult(`fstat 4 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFstatResult(`fstat 5 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFault(`mount "/proc/self/fd/4" "/proc/self/fd/5" "" MS_BIND ""`, errTesting)
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "/source", Dir: "/target", Options: []string{"bind"}}}
	synth, err := chg.Perform(s.sec)
	c.Assert(err, Equals, errTesting)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/target"`, R: testutil.FileInfoDir},
		{C: `lstat "/source"`, R: testutil.FileInfoDir},
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "source" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 4},
		{C: `fstat 4 <ptr>`, R: syscall.Stat_t{}},
		{C: `close 3`},
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "target" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 5},
		{C: `fstat 5 <ptr>`, R: syscall.Stat_t{}},
		{C: `close 3`},
		{C: `mount "/proc/self/fd/4" "/proc/self/fd/5" "" MS_BIND ""`, E: errTesting},
		{C: `close 5`},
		{C: `close 4`},
	})
}

// Change.Perform wants to bind mount a directory but the mount point isn't there.
func (s *changeSuite) TestPerformDirectoryBindMountWithoutMountPoint(c *C) {
	s.sys.InsertOsLstatResult(`lstat "/source"`, testutil.FileInfoDir)
	s.sys.InsertFault(`lstat "/target"`, syscall.ENOENT)
	s.sys.InsertFstatResult(`fstat 4 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFstatResult(`fstat 5 <ptr>`, syscall.Stat_t{})
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "/source", Dir: "/target", Options: []string{"bind"}}}
	synth, err := chg.Perform(s.sec)
	c.Assert(err, IsNil)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/target"`, E: syscall.ENOENT},
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "target" 0755`},
		{C: `openat 3 "target" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `fchown 4 0 0`},
		{C: `close 4`},
		{C: `close 3`},
		{C: `lstat "/source"`, R: testutil.FileInfoDir},
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "source" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 4},
		{C: `fstat 4 <ptr>`, R: syscall.Stat_t{}},
		{C: `close 3`},
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "target" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 5},
		{C: `fstat 5 <ptr>`, R: syscall.Stat_t{}},
		{C: `close 3`},
		{C: `mount "/proc/self/fd/4" "/proc/self/fd/5" "" MS_BIND ""`},
		{C: `close 5`},
		{C: `close 4`},
	})
}

// Change.Perform wants to bind mount a directory but the mount source isn't there.
func (s *changeSuite) TestPerformDirectoryBindMountWithoutMountSource(c *C) {
	s.sys.InsertFault(`lstat "/source"`, syscall.ENOENT)
	s.sys.InsertOsLstatResult(`lstat "/target"`, testutil.FileInfoDir)
	s.sys.InsertFstatResult(`fstat 4 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFstatResult(`fstat 5 <ptr>`, syscall.Stat_t{})
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "/source", Dir: "/target", Options: []string{"bind"}}}
	synth, err := chg.Perform(s.sec)
	c.Assert(err, IsNil)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/target"`, R: testutil.FileInfoDir},
		{C: `lstat "/source"`, E: syscall.ENOENT},
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "source" 0755`},
		{C: `openat 3 "source" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `fchown 4 0 0`},
		{C: `close 4`},
		{C: `close 3`},
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "source" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 4},
		{C: `fstat 4 <ptr>`, R: syscall.Stat_t{}},
		{C: `close 3`},
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "target" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 5},
		{C: `fstat 5 <ptr>`, R: syscall.Stat_t{}},
		{C: `close 3`},
		{C: `mount "/proc/self/fd/4" "/proc/self/fd/5" "" MS_BIND ""`},
		{C: `close 5`},
		{C: `close 4`},
	})
}

// Change.Perform wants to create a directory bind mount but the mount point isn't there and cannot be created.
func (s *changeSuite) TestPerformDirectoryBindMountWithoutMountPointWithErrors(c *C) {
	s.sys.InsertFault(`lstat "/target"`, syscall.ENOENT)
	s.sys.InsertFault(`mkdirat 3 "target" 0755`, errTesting)
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "/source", Dir: "/target", Options: []string{"bind"}}}
	synth, err := chg.Perform(s.sec)
	c.Assert(err, ErrorMatches, `cannot create directory "/target": testing`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/target"`, E: syscall.ENOENT},
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "target" 0755`, E: errTesting},
		{C: `close 3`},
	})
}

// Change.Perform wants to create a directory bind mount but the mount source isn't there and cannot be created.
func (s *changeSuite) TestPerformDirectoryBindMountWithoutMountSourceWithErrors(c *C) {
	s.sys.InsertFault(`lstat "/source"`, syscall.ENOENT)
	s.sys.InsertFault(`mkdirat 3 "source" 0755`, errTesting)
	s.sys.InsertOsLstatResult(`lstat "/target"`, testutil.FileInfoDir)
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "/source", Dir: "/target", Options: []string{"bind"}}}
	synth, err := chg.Perform(s.sec)
	c.Assert(err, ErrorMatches, `cannot create directory "/source": testing`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/target"`, R: testutil.FileInfoDir},
		{C: `lstat "/source"`, E: syscall.ENOENT},
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "source" 0755`, E: errTesting},
		{C: `close 3`},
	})
}

// Change.Perform wants to bind mount a directory but the mount point isn't there and the parent is read-only.
func (s *changeSuite) TestPerformDirectoryBindMountWithoutMountPointAndReadOnlyBase(c *C) {
	s.sys.InsertFault(`lstat "/rofs/target"`, syscall.ENOENT)
	s.sys.InsertFault(`mkdirat 3 "rofs" 0755`, syscall.EEXIST)
	s.sys.InsertFault(`openat 3 "target" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, syscall.ENOENT, nil) // works on 2nd try
	s.sys.InsertFault(`mkdirat 4 "target" 0755`, syscall.EROFS, nil)                               // works on 2nd try
	s.sys.InsertSysLstatResult(`lstat "/rofs" <ptr>`, syscall.Stat_t{Uid: 0, Gid: 0, Mode: 0755})
	s.sys.InsertReadDirResult(`readdir "/rofs"`, nil) // pretend /rofs is empty.
	s.sys.InsertFault(`lstat "/tmp/.snap/rofs"`, syscall.ENOENT)
	s.sys.InsertOsLstatResult(`lstat "/rofs"`, testutil.FileInfoDir)
	s.sys.InsertOsLstatResult(`lstat "/source"`, testutil.FileInfoDir)
	s.sys.InsertFstatResult(`fstat 4 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFstatResult(`fstat 7 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFstatResult(`fstat 6 <ptr>`, syscall.Stat_t{})

	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "/source", Dir: "/rofs/target", Options: []string{"bind"}}}
	synth, err := chg.Perform(s.sec)
	c.Assert(err, IsNil)
	c.Assert(synth, DeepEquals, []*update.Change{
		{Action: update.Mount, Entry: osutil.MountEntry{
			Name: "tmpfs", Dir: "/rofs", Type: "tmpfs",
			Options: []string{"x-snapd.synthetic", "x-snapd.needed-by=/rofs/target", "mode=0755", "uid=0", "gid=0"}},
		},
	})
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		// sniff mount target
		{C: `lstat "/rofs/target"`, E: syscall.ENOENT},

		// /rofs/target is missing, create it
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "rofs" 0755`, E: syscall.EEXIST},
		{C: `openat 3 "rofs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `close 3`},
		{C: `mkdirat 4 "target" 0755`, E: syscall.EROFS},
		{C: `close 4`},

		// error, read only filesystem, create a mimic
		{C: `lstat "/rofs" <ptr>`, R: syscall.Stat_t{Uid: 0, Gid: 0, Mode: 0755}},
		{C: `readdir "/rofs"`, R: []os.FileInfo(nil)},
		{C: `lstat "/tmp/.snap/rofs"`, E: syscall.ENOENT},
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "tmp" 0755`},
		{C: `openat 3 "tmp" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `fchown 4 0 0`},
		{C: `mkdirat 4 ".snap" 0755`},
		{C: `openat 4 ".snap" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 5},
		{C: `fchown 5 0 0`},
		{C: `close 4`},
		{C: `close 3`},
		{C: `mkdirat 5 "rofs" 0755`},
		{C: `openat 5 "rofs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `fchown 3 0 0`},
		{C: `close 3`},
		{C: `close 5`},
		{C: `lstat "/rofs"`, R: testutil.FileInfoDir},

		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "rofs" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 4},
		{C: `fstat 4 <ptr>`, R: syscall.Stat_t{}},
		{C: `close 3`},
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "tmp" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 5},
		{C: `openat 5 ".snap" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 6},
		{C: `openat 6 "rofs" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 7},
		{C: `fstat 7 <ptr>`, R: syscall.Stat_t{}},
		{C: `close 6`},
		{C: `close 5`},
		{C: `close 3`},
		{C: `mount "/proc/self/fd/4" "/proc/self/fd/7" "" MS_BIND|MS_REC ""`},
		{C: `close 7`},
		{C: `close 4`},

		{C: `lstat "/rofs"`, R: testutil.FileInfoDir},
		{C: `mount "tmpfs" "/rofs" "tmpfs" 0 "mode=0755,uid=0,gid=0"`},
		{C: `unmount "/tmp/.snap/rofs" UMOUNT_NOFOLLOW|MNT_DETACH`},

		// mimic ready, re-try initial mkdir
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "rofs" 0755`, E: syscall.EEXIST},
		{C: `openat 3 "rofs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `close 3`},
		{C: `mkdirat 4 "target" 0755`},
		{C: `openat 4 "target" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `fchown 3 0 0`},
		{C: `close 3`},
		{C: `close 4`},

		// sniff mount source
		{C: `lstat "/source"`, R: testutil.FileInfoDir},

		// mount the filesystem
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "source" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 4},
		{C: `fstat 4 <ptr>`, R: syscall.Stat_t{}},
		{C: `close 3`},
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "rofs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 5},
		{C: `openat 5 "target" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 6},
		{C: `fstat 6 <ptr>`, R: syscall.Stat_t{}},
		{C: `close 5`},
		{C: `close 3`},
		{C: `mount "/proc/self/fd/4" "/proc/self/fd/6" "" MS_BIND ""`},
		{C: `close 6`},
		{C: `close 4`},
	})
}

// Change.Perform wants to bind mount a directory but the mount source isn't there and the parent is read-only.
func (s *changeSuite) TestPerformDirectoryBindMountWithoutMountSourceAndReadOnlyBase(c *C) {
	s.sys.InsertOsLstatResult(`lstat "/rofs"`, testutil.FileInfoDir)
	s.sys.InsertFault(`lstat "/rofs/source"`, syscall.ENOENT)
	s.sys.InsertOsLstatResult(`lstat "/target"`, testutil.FileInfoDir)
	s.sys.InsertFault(`mkdirat 3 "rofs" 0755`, syscall.EEXIST)
	s.sys.InsertFault(`mkdirat 4 "source" 0755`, syscall.EROFS)

	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "/rofs/source", Dir: "/target", Options: []string{"bind"}}}
	synth, err := chg.Perform(s.sec)
	c.Assert(err, ErrorMatches, `cannot operate on read-only filesystem at /rofs`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/target"`, R: testutil.FileInfoDir},
		{C: `lstat "/rofs/source"`, E: syscall.ENOENT},
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "rofs" 0755`, E: syscall.EEXIST},
		{C: `openat 3 "rofs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `close 3`},
		{C: `mkdirat 4 "source" 0755`, E: syscall.EROFS},
		{C: `close 4`},
	})
}

// Change.Perform wants to bind mount a directory but the mount source isn't there and the parent is read-only but this is for a layout.
func (s *changeSuite) TestPerformDirectoryBindMountWithoutMountSourceAndReadOnlyBaseForLayout(c *C) {
	s.sys.InsertOsLstatResult(`lstat "/target"`, testutil.FileInfoDir)
	s.sys.InsertFault(`lstat "/rofs/source"`, syscall.ENOENT)
	s.sys.InsertFault(`mkdirat 3 "rofs" 0755`, syscall.EEXIST)
	s.sys.InsertFault(`openat 3 "source" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, syscall.ENOENT, nil) // works on 2nd try
	s.sys.InsertFault(`mkdirat 4 "source" 0755`, syscall.EROFS, nil)                               // works on 2nd try
	s.sys.InsertReadDirResult(`readdir "/rofs"`, nil)                                              // pretend /rofs is empty.
	s.sys.InsertFault(`lstat "/tmp/.snap/rofs"`, syscall.ENOENT)
	s.sys.InsertOsLstatResult(`lstat "/rofs"`, testutil.FileInfoDir)
	s.sys.InsertSysLstatResult(`lstat "/rofs" <ptr>`, syscall.Stat_t{Uid: 0, Gid: 0, Mode: 0755})
	s.sys.InsertFstatResult(`fstat 4 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFstatResult(`fstat 5 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFstatResult(`fstat 7 <ptr>`, syscall.Stat_t{})

	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "/rofs/source", Dir: "/target", Options: []string{"bind", "x-snapd.origin=layout"}}}
	synth, err := chg.Perform(s.sec)
	c.Assert(err, IsNil)
	c.Check(synth, DeepEquals, []*update.Change{
		{Action: update.Mount, Entry: osutil.MountEntry{
			Name: "tmpfs", Dir: "/rofs", Type: "tmpfs",
			Options: []string{"x-snapd.synthetic", "x-snapd.needed-by=/rofs/source", "mode=0755", "uid=0", "gid=0"}},
		},
	})
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		// sniff mount target and source
		{C: `lstat "/target"`, R: testutil.FileInfoDir},
		{C: `lstat "/rofs/source"`, E: syscall.ENOENT},

		// /rofs/source is missing, create it
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "rofs" 0755`, E: syscall.EEXIST},
		{C: `openat 3 "rofs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `close 3`},
		{C: `mkdirat 4 "source" 0755`, E: syscall.EROFS},
		{C: `close 4`},

		// error /rofs is a read-only filesystem, create a mimic
		{C: `lstat "/rofs" <ptr>`, R: syscall.Stat_t{Mode: 0755}},
		{C: `readdir "/rofs"`, R: []os.FileInfo(nil)},
		{C: `lstat "/tmp/.snap/rofs"`, E: syscall.ENOENT},
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "tmp" 0755`},
		{C: `openat 3 "tmp" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `fchown 4 0 0`},
		{C: `mkdirat 4 ".snap" 0755`},
		{C: `openat 4 ".snap" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 5},
		{C: `fchown 5 0 0`},
		{C: `close 4`},
		{C: `close 3`},
		{C: `mkdirat 5 "rofs" 0755`},
		{C: `openat 5 "rofs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `fchown 3 0 0`},
		{C: `close 3`},
		{C: `close 5`},
		{C: `lstat "/rofs"`, R: testutil.FileInfoDir},
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "rofs" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 4},
		{C: `fstat 4 <ptr>`, R: syscall.Stat_t{}},
		{C: `close 3`},
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "tmp" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 5},
		{C: `openat 5 ".snap" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 6},
		{C: `openat 6 "rofs" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 7},
		{C: `fstat 7 <ptr>`, R: syscall.Stat_t{}},
		{C: `close 6`},
		{C: `close 5`},
		{C: `close 3`},
		{C: `mount "/proc/self/fd/4" "/proc/self/fd/7" "" MS_BIND|MS_REC ""`},
		{C: `close 7`},
		{C: `close 4`},
		{C: `lstat "/rofs"`, R: testutil.FileInfoDir},
		{C: `mount "tmpfs" "/rofs" "tmpfs" 0 "mode=0755,uid=0,gid=0"`},
		{C: `unmount "/tmp/.snap/rofs" UMOUNT_NOFOLLOW|MNT_DETACH`},

		// /rofs/source was missing (we checked earlier), create it
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "rofs" 0755`, E: syscall.EEXIST},
		{C: `openat 3 "rofs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `close 3`},
		{C: `mkdirat 4 "source" 0755`},
		{C: `openat 4 "source" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `fchown 3 0 0`},
		{C: `close 3`},
		{C: `close 4`},

		// bind mount /rofs/source -> /target
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "rofs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 4},
		{C: `openat 4 "source" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 5},
		{C: `fstat 5 <ptr>`, R: syscall.Stat_t{}},
		{C: `close 4`},
		{C: `close 3`},
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "target" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 4},
		{C: `fstat 4 <ptr>`, R: syscall.Stat_t{}},
		{C: `close 3`},
		{C: `mount "/proc/self/fd/5" "/proc/self/fd/4" "" MS_BIND ""`},
		{C: `close 4`},
		{C: `close 5`},
	})
}

// Change.Perform wants to bind mount a directory but there's a symlink in mount point.
func (s *changeSuite) TestPerformDirectoryBindMountWithSymlinkInMountPoint(c *C) {
	s.sys.InsertOsLstatResult(`lstat "/target"`, testutil.FileInfoSymlink)
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "/source", Dir: "/target", Options: []string{"bind"}}}
	synth, err := chg.Perform(s.sec)
	c.Assert(err, ErrorMatches, `cannot use "/target" as mount point: not a directory`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/target"`, R: testutil.FileInfoSymlink},
	})
}

// Change.Perform wants to bind mount a directory but there's a file in mount mount.
func (s *changeSuite) TestPerformDirectoryBindMountWithFileInMountPoint(c *C) {
	s.sys.InsertOsLstatResult(`lstat "/target"`, testutil.FileInfoFile)
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "/source", Dir: "/target", Options: []string{"bind"}}}
	synth, err := chg.Perform(s.sec)
	c.Assert(err, ErrorMatches, `cannot use "/target" as mount point: not a directory`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/target"`, R: testutil.FileInfoFile},
	})
}

// Change.Perform wants to bind mount a directory but there's a symlink in source.
func (s *changeSuite) TestPerformDirectoryBindMountWithSymlinkInMountSource(c *C) {
	s.sys.InsertOsLstatResult(`lstat "/target"`, testutil.FileInfoDir)
	s.sys.InsertOsLstatResult(`lstat "/source"`, testutil.FileInfoSymlink)
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "/source", Dir: "/target", Options: []string{"bind"}}}
	synth, err := chg.Perform(s.sec)
	c.Assert(err, ErrorMatches, `cannot use "/source" as bind-mount source: not a directory`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/target"`, R: testutil.FileInfoDir},
		{C: `lstat "/source"`, R: testutil.FileInfoSymlink},
	})
}

// Change.Perform wants to bind mount a directory but there's a file in source.
func (s *changeSuite) TestPerformDirectoryBindMountWithFileInMountSource(c *C) {
	s.sys.InsertOsLstatResult(`lstat "/target"`, testutil.FileInfoDir)
	s.sys.InsertOsLstatResult(`lstat "/source"`, testutil.FileInfoFile)
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "/source", Dir: "/target", Options: []string{"bind"}}}
	synth, err := chg.Perform(s.sec)
	c.Assert(err, ErrorMatches, `cannot use "/source" as bind-mount source: not a directory`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/target"`, R: testutil.FileInfoDir},
		{C: `lstat "/source"`, R: testutil.FileInfoFile},
	})
}

// Change.Perform wants to unmount a directory bind mount.
func (s *changeSuite) TestPerformDirectoryBindUnmount(c *C) {
	chg := &update.Change{Action: update.Unmount, Entry: osutil.MountEntry{Name: "/source", Dir: "/target", Options: []string{"bind"}}}
	synth, err := chg.Perform(s.sec)
	c.Assert(err, IsNil)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `unmount "/target" UMOUNT_NOFOLLOW`},
	})
	c.Assert(synth, HasLen, 0)
}

// Change.Perform wants to unmount a directory bind mount but it fails.
func (s *changeSuite) TestPerformDirectoryBindUnmountError(c *C) {
	s.sys.InsertFault(`unmount "/target" UMOUNT_NOFOLLOW`, errTesting)
	chg := &update.Change{Action: update.Unmount, Entry: osutil.MountEntry{Name: "/source", Dir: "/target", Options: []string{"bind"}}}
	synth, err := chg.Perform(s.sec)
	c.Assert(err, Equals, errTesting)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `unmount "/target" UMOUNT_NOFOLLOW`, E: errTesting},
	})
	c.Assert(synth, HasLen, 0)
}

// #########################################
// Topic: bind-mounting and unmounting files
// #########################################

// Change.Perform wants to bind mount a file but the target cannot be stat'ed.
func (s *changeSuite) TestPerformFileBindMountTargetLstatError(c *C) {
	s.sys.InsertFault(`lstat "/target"`, errTesting)
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "/source", Dir: "/target", Options: []string{"bind", "x-snapd.kind=file"}}}
	synth, err := chg.Perform(s.sec)
	c.Assert(err, ErrorMatches, `cannot inspect "/target": testing`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/target"`, E: errTesting},
	})
}

// Change.Perform wants to bind mount a file but the source cannot be stat'ed.
func (s *changeSuite) TestPerformFileBindMountSourceLstatError(c *C) {
	s.sys.InsertOsLstatResult(`lstat "/target"`, testutil.FileInfoFile)
	s.sys.InsertFault(`lstat "/source"`, errTesting)
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "/source", Dir: "/target", Options: []string{"bind", "x-snapd.kind=file"}}}
	synth, err := chg.Perform(s.sec)
	c.Assert(err, ErrorMatches, `cannot inspect "/source": testing`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/target"`, R: testutil.FileInfoFile},
		{C: `lstat "/source"`, E: errTesting},
	})
}

// Change.Perform wants to bind mount a file.
func (s *changeSuite) TestPerformFileBindMount(c *C) {
	s.sys.InsertOsLstatResult(`lstat "/source"`, testutil.FileInfoFile)
	s.sys.InsertOsLstatResult(`lstat "/target"`, testutil.FileInfoFile)
	s.sys.InsertFstatResult(`fstat 4 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFstatResult(`fstat 5 <ptr>`, syscall.Stat_t{})
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "/source", Dir: "/target", Options: []string{"bind", "x-snapd.kind=file"}}}
	synth, err := chg.Perform(s.sec)
	c.Assert(err, IsNil)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/target"`, R: testutil.FileInfoFile},
		{C: `lstat "/source"`, R: testutil.FileInfoFile},
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "source" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 4},
		{C: `fstat 4 <ptr>`, R: syscall.Stat_t{}},
		{C: `close 3`},
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "target" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 5},
		{C: `fstat 5 <ptr>`, R: syscall.Stat_t{}},
		{C: `close 3`},
		{C: `mount "/proc/self/fd/4" "/proc/self/fd/5" "" MS_BIND ""`},
		{C: `close 5`},
		{C: `close 4`},
	})
}

// Change.Perform wants to bind mount a file but it fails.
func (s *changeSuite) TestPerformFileBindMountWithError(c *C) {
	s.sys.InsertOsLstatResult(`lstat "/target"`, testutil.FileInfoFile)
	s.sys.InsertOsLstatResult(`lstat "/source"`, testutil.FileInfoFile)
	s.sys.InsertFstatResult(`fstat 4 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFstatResult(`fstat 5 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFault(`mount "/proc/self/fd/4" "/proc/self/fd/5" "" MS_BIND ""`, errTesting)
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "/source", Dir: "/target", Options: []string{"bind", "x-snapd.kind=file"}}}
	synth, err := chg.Perform(s.sec)
	c.Assert(err, Equals, errTesting)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/target"`, R: testutil.FileInfoFile},
		{C: `lstat "/source"`, R: testutil.FileInfoFile},
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "source" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 4},
		{C: `fstat 4 <ptr>`, R: syscall.Stat_t{}},
		{C: `close 3`},
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "target" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 5},
		{C: `fstat 5 <ptr>`, R: syscall.Stat_t{}},
		{C: `close 3`},
		{C: `mount "/proc/self/fd/4" "/proc/self/fd/5" "" MS_BIND ""`, E: errTesting},
		{C: `close 5`},
		{C: `close 4`},
	})
}

// Change.Perform wants to bind mount a file but the mount point isn't there.
func (s *changeSuite) TestPerformFileBindMountWithoutMountPoint(c *C) {
	s.sys.InsertOsLstatResult(`lstat "/source"`, testutil.FileInfoFile)
	s.sys.InsertFault(`lstat "/target"`, syscall.ENOENT)
	s.sys.InsertFstatResult(`fstat 4 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFstatResult(`fstat 5 <ptr>`, syscall.Stat_t{})
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "/source", Dir: "/target", Options: []string{"bind", "x-snapd.kind=file"}}}
	synth, err := chg.Perform(s.sec)
	c.Assert(err, IsNil)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/target"`, E: syscall.ENOENT},
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `openat 3 "target" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`, R: 4},
		{C: `fchown 4 0 0`},
		{C: `close 4`},
		{C: `close 3`},
		{C: `lstat "/source"`, R: testutil.FileInfoFile},
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "source" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 4},
		{C: `fstat 4 <ptr>`, R: syscall.Stat_t{}},
		{C: `close 3`},
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "target" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 5},
		{C: `fstat 5 <ptr>`, R: syscall.Stat_t{}},
		{C: `close 3`},
		{C: `mount "/proc/self/fd/4" "/proc/self/fd/5" "" MS_BIND ""`},
		{C: `close 5`},
		{C: `close 4`},
	})
}

// Change.Perform wants to create a directory bind mount but the mount point isn't there and cannot be created.
func (s *changeSuite) TestPerformFileBindMountWithoutMountPointWithErrors(c *C) {
	s.sys.InsertFault(`lstat "/target"`, syscall.ENOENT)
	s.sys.InsertFault(`openat 3 "target" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`, errTesting)
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "/source", Dir: "/target", Options: []string{"bind", "x-snapd.kind=file"}}}
	synth, err := chg.Perform(s.sec)
	c.Assert(err, ErrorMatches, `cannot open file "/target": testing`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/target"`, E: syscall.ENOENT},
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `openat 3 "target" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`, E: errTesting},
		{C: `close 3`},
	})
}

// Change.Perform wants to bind mount a file but the mount source isn't there.
func (s *changeSuite) TestPerformFileBindMountWithoutMountSource(c *C) {
	s.sys.InsertFault(`lstat "/source"`, syscall.ENOENT)
	s.sys.InsertOsLstatResult(`lstat "/target"`, testutil.FileInfoFile)
	s.sys.InsertFstatResult(`fstat 4 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFstatResult(`fstat 5 <ptr>`, syscall.Stat_t{})
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "/source", Dir: "/target", Options: []string{"bind", "x-snapd.kind=file"}}}
	synth, err := chg.Perform(s.sec)
	c.Assert(err, IsNil)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/target"`, R: testutil.FileInfoFile},
		{C: `lstat "/source"`, E: syscall.ENOENT},
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `openat 3 "source" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`, R: 4},
		{C: `fchown 4 0 0`},
		{C: `close 4`},
		{C: `close 3`},
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "source" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 4},
		{C: `fstat 4 <ptr>`, R: syscall.Stat_t{}},
		{C: `close 3`},
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "target" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 5},
		{C: `fstat 5 <ptr>`, R: syscall.Stat_t{}},
		{C: `close 3`},
		{C: `mount "/proc/self/fd/4" "/proc/self/fd/5" "" MS_BIND ""`},
		{C: `close 5`},
		{C: `close 4`},
	})
}

// Change.Perform wants to create a file bind mount but the mount source isn't there and cannot be created.
func (s *changeSuite) TestPerformFileBindMountWithoutMountSourceWithErrors(c *C) {
	s.sys.InsertFault(`lstat "/source"`, syscall.ENOENT)
	s.sys.InsertFault(`openat 3 "source" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`, errTesting)
	s.sys.InsertOsLstatResult(`lstat "/target"`, testutil.FileInfoFile)
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "/source", Dir: "/target", Options: []string{"bind", "x-snapd.kind=file"}}}
	synth, err := chg.Perform(s.sec)
	c.Assert(err, ErrorMatches, `cannot open file "/source": testing`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/target"`, R: testutil.FileInfoFile},
		{C: `lstat "/source"`, E: syscall.ENOENT},
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `openat 3 "source" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`, E: errTesting},
		{C: `close 3`},
	})
}

// Change.Perform wants to bind mount a file but the mount point isn't there and the parent is read-only.
func (s *changeSuite) TestPerformFileBindMountWithoutMountPointAndReadOnlyBase(c *C) {
	s.sys.InsertFault(`lstat "/rofs/target"`, syscall.ENOENT)
	s.sys.InsertFault(`mkdirat 3 "rofs" 0755`, syscall.EEXIST)
	s.sys.InsertFault(`openat 4 "target" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`, syscall.EROFS, nil) // works on 2nd try
	s.sys.InsertSysLstatResult(`lstat "/rofs" <ptr>`, syscall.Stat_t{Uid: 0, Gid: 0, Mode: 0755})
	s.sys.InsertReadDirResult(`readdir "/rofs"`, nil) // pretend /rofs is empty.
	s.sys.InsertFault(`lstat "/tmp/.snap/rofs"`, syscall.ENOENT)
	s.sys.InsertOsLstatResult(`lstat "/rofs"`, testutil.FileInfoDir)
	s.sys.InsertOsLstatResult(`lstat "/source"`, testutil.FileInfoFile)
	s.sys.InsertFstatResult(`fstat 4 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFstatResult(`fstat 6 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFstatResult(`fstat 7 <ptr>`, syscall.Stat_t{})

	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "/source", Dir: "/rofs/target", Options: []string{"bind", "x-snapd.kind=file"}}}
	synth, err := chg.Perform(s.sec)
	c.Assert(err, IsNil)
	c.Assert(synth, DeepEquals, []*update.Change{
		{Action: update.Mount, Entry: osutil.MountEntry{
			Name: "tmpfs", Dir: "/rofs", Type: "tmpfs",
			Options: []string{"x-snapd.synthetic", "x-snapd.needed-by=/rofs/target", "mode=0755", "uid=0", "gid=0"}},
		},
	})
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		// sniff mount target
		{C: `lstat "/rofs/target"`, E: syscall.ENOENT},

		// /rofs/target is missing, create it
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "rofs" 0755`, E: syscall.EEXIST},
		{C: `openat 3 "rofs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `close 3`},
		{C: `openat 4 "target" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`, E: syscall.EROFS},
		{C: `close 4`},

		// error, read only filesystem, create a mimic
		{C: `lstat "/rofs" <ptr>`, R: syscall.Stat_t{Mode: 0755}},
		{C: `readdir "/rofs"`, R: []os.FileInfo(nil)},
		{C: `lstat "/tmp/.snap/rofs"`, E: syscall.ENOENT},
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "tmp" 0755`},
		{C: `openat 3 "tmp" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `fchown 4 0 0`},
		{C: `mkdirat 4 ".snap" 0755`},
		{C: `openat 4 ".snap" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 5},
		{C: `fchown 5 0 0`},
		{C: `close 4`},
		{C: `close 3`},
		{C: `mkdirat 5 "rofs" 0755`},
		{C: `openat 5 "rofs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `fchown 3 0 0`},
		{C: `close 3`},
		{C: `close 5`},
		{C: `lstat "/rofs"`, R: testutil.FileInfoDir},

		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "rofs" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 4},
		{C: `fstat 4 <ptr>`, R: syscall.Stat_t{}},
		{C: `close 3`},
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "tmp" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 5},
		{C: `openat 5 ".snap" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 6},
		{C: `openat 6 "rofs" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 7},
		{C: `fstat 7 <ptr>`, R: syscall.Stat_t{}},
		{C: `close 6`},
		{C: `close 5`},
		{C: `close 3`},
		{C: `mount "/proc/self/fd/4" "/proc/self/fd/7" "" MS_BIND|MS_REC ""`},
		{C: `close 7`},
		{C: `close 4`},

		{C: `lstat "/rofs"`, R: testutil.FileInfoDir},
		{C: `mount "tmpfs" "/rofs" "tmpfs" 0 "mode=0755,uid=0,gid=0"`},
		{C: `unmount "/tmp/.snap/rofs" UMOUNT_NOFOLLOW|MNT_DETACH`},

		// mimic ready, re-try initial mkdir
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "rofs" 0755`, E: syscall.EEXIST},
		{C: `openat 3 "rofs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `close 3`},
		{C: `openat 4 "target" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`, R: 3},
		{C: `fchown 3 0 0`},
		{C: `close 3`},
		{C: `close 4`},

		// sniff mount source
		{C: `lstat "/source"`, R: testutil.FileInfoFile},

		// mount the filesystem
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "source" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 4},
		{C: `fstat 4 <ptr>`, R: syscall.Stat_t{}},
		{C: `close 3`},
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "rofs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 5},
		{C: `openat 5 "target" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 6},
		{C: `fstat 6 <ptr>`, R: syscall.Stat_t{}},
		{C: `close 5`},
		{C: `close 3`},
		{C: `mount "/proc/self/fd/4" "/proc/self/fd/6" "" MS_BIND ""`},
		{C: `close 6`},
		{C: `close 4`},
	})
}

// Change.Perform wants to bind mount a file but there's a symlink in mount point.
func (s *changeSuite) TestPerformFileBindMountWithSymlinkInMountPoint(c *C) {
	s.sys.InsertOsLstatResult(`lstat "/target"`, testutil.FileInfoSymlink)
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "/source", Dir: "/target", Options: []string{"bind", "x-snapd.kind=file"}}}
	synth, err := chg.Perform(s.sec)
	c.Assert(err, ErrorMatches, `cannot use "/target" as mount point: not a regular file`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/target"`, R: testutil.FileInfoSymlink},
	})
}

// Change.Perform wants to bind mount a file but there's a directory in mount point.
func (s *changeSuite) TestPerformBindMountFileWithDirectoryInMountPoint(c *C) {
	s.sys.InsertOsLstatResult(`lstat "/target"`, testutil.FileInfoDir)
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "/source", Dir: "/target", Options: []string{"bind", "x-snapd.kind=file"}}}
	synth, err := chg.Perform(s.sec)
	c.Assert(err, ErrorMatches, `cannot use "/target" as mount point: not a regular file`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/target"`, R: testutil.FileInfoDir},
	})
}

// Change.Perform wants to bind mount a file but there's a symlink in source.
func (s *changeSuite) TestPerformFileBindMountWithSymlinkInMountSource(c *C) {
	s.sys.InsertOsLstatResult(`lstat "/target"`, testutil.FileInfoFile)
	s.sys.InsertOsLstatResult(`lstat "/source"`, testutil.FileInfoSymlink)
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "/source", Dir: "/target", Options: []string{"bind", "x-snapd.kind=file"}}}
	synth, err := chg.Perform(s.sec)
	c.Assert(err, ErrorMatches, `cannot use "/source" as bind-mount source: not a regular file`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/target"`, R: testutil.FileInfoFile},
		{C: `lstat "/source"`, R: testutil.FileInfoSymlink},
	})
}

// Change.Perform wants to bind mount a file but there's a directory in source.
func (s *changeSuite) TestPerformFileBindMountWithDirectoryInMountSource(c *C) {
	s.sys.InsertOsLstatResult(`lstat "/target"`, testutil.FileInfoFile)
	s.sys.InsertOsLstatResult(`lstat "/source"`, testutil.FileInfoDir)
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "/source", Dir: "/target", Options: []string{"bind", "x-snapd.kind=file"}}}
	synth, err := chg.Perform(s.sec)
	c.Assert(err, ErrorMatches, `cannot use "/source" as bind-mount source: not a regular file`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/target"`, R: testutil.FileInfoFile},
		{C: `lstat "/source"`, R: testutil.FileInfoDir},
	})
}

// Change.Perform wants to unmount a file bind mount.
func (s *changeSuite) TestPerformFileBindUnmount(c *C) {
	chg := &update.Change{Action: update.Unmount, Entry: osutil.MountEntry{Name: "/source", Dir: "/target", Options: []string{"bind", "x-snapd.kind=file"}}}
	synth, err := chg.Perform(s.sec)
	c.Assert(err, IsNil)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `unmount "/target" UMOUNT_NOFOLLOW`},
	})
	c.Assert(synth, HasLen, 0)
}

// Change.Perform wants to unmount a file bind mount but it fails.
func (s *changeSuite) TestPerformFileBindUnmountError(c *C) {
	s.sys.InsertFault(`unmount "/target" UMOUNT_NOFOLLOW`, errTesting)
	chg := &update.Change{Action: update.Unmount, Entry: osutil.MountEntry{Name: "/source", Dir: "/target", Options: []string{"bind", "x-snapd.kind=file"}}}
	synth, err := chg.Perform(s.sec)
	c.Assert(err, Equals, errTesting)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `unmount "/target" UMOUNT_NOFOLLOW`, E: errTesting},
	})
	c.Assert(synth, HasLen, 0)
}

// #############################################################
// Topic: handling mounts with the x-snapd.ignore-missing option
// #############################################################

func (s *changeSuite) TestPerformMountWithIgnoredMissingMountSource(c *C) {
	s.sys.InsertFault(`lstat "/source"`, syscall.ENOENT)
	s.sys.InsertOsLstatResult(`lstat "/target"`, testutil.FileInfoDir)
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "/source", Dir: "/target", Options: []string{"bind", "x-snapd.ignore-missing"}}}
	synth, err := chg.Perform(s.sec)
	c.Assert(err, Equals, update.ErrIgnoredMissingMount)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/target"`, R: testutil.FileInfoDir},
		{C: `lstat "/source"`, E: syscall.ENOENT},
	})
}

func (s *changeSuite) TestPerformMountWithIgnoredMissingMountPoint(c *C) {
	s.sys.InsertFault(`lstat "/target"`, syscall.ENOENT)
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "/source", Dir: "/target", Options: []string{"bind", "x-snapd.ignore-missing"}}}
	synth, err := chg.Perform(s.sec)
	c.Assert(err, Equals, update.ErrIgnoredMissingMount)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/target"`, E: syscall.ENOENT},
	})
}

// ########################
// Topic: creating symlinks
// ########################

// Change.Perform wants to create a symlink but name cannot be stat'ed.
func (s *changeSuite) TestPerformCreateSymlinkNameLstatError(c *C) {
	s.sys.InsertFault(`lstat "/name"`, errTesting)
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "unused", Dir: "/name", Options: []string{"x-snapd.kind=symlink", "x-snapd.symlink=/oldname"}}}
	synth, err := chg.Perform(s.sec)
	c.Assert(err, ErrorMatches, `cannot inspect "/name": testing`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/name"`, E: errTesting},
	})
}

// Change.Perform wants to create a symlink.
func (s *changeSuite) TestPerformCreateSymlink(c *C) {
	s.sys.InsertFault(`lstat "/name"`, syscall.ENOENT)
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "unused", Dir: "/name", Options: []string{"x-snapd.kind=symlink", "x-snapd.symlink=/oldname"}}}
	synth, err := chg.Perform(s.sec)
	c.Assert(err, IsNil)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/name"`, E: syscall.ENOENT},
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `symlinkat "/oldname" 3 "name"`},
		{C: `close 3`},
	})
}

// Change.Perform wants to create a symlink but it fails.
func (s *changeSuite) TestPerformCreateSymlinkWithError(c *C) {
	s.sys.InsertFault(`lstat "/name"`, syscall.ENOENT)
	s.sys.InsertFault(`symlinkat "/oldname" 3 "name"`, errTesting)
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "unused", Dir: "/name", Options: []string{"x-snapd.kind=symlink", "x-snapd.symlink=/oldname"}}}
	synth, err := chg.Perform(s.sec)
	c.Assert(err, ErrorMatches, `cannot create symlink "/name": testing`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/name"`, E: syscall.ENOENT},
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `symlinkat "/oldname" 3 "name"`, E: errTesting},
		{C: `close 3`},
	})
}

// Change.Perform wants to create a symlink but the target is empty.
func (s *changeSuite) TestPerformCreateSymlinkWithNoTargetError(c *C) {
	s.sys.InsertFault(`lstat "/name"`, syscall.ENOENT)
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "unused", Dir: "/name", Options: []string{"x-snapd.kind=symlink", "x-snapd.symlink="}}}
	synth, err := chg.Perform(s.sec)
	c.Assert(err, ErrorMatches, `cannot create symlink with empty target: "/name"`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/name"`, E: syscall.ENOENT},
	})
}

// Change.Perform wants to create a symlink but the base directory isn't there.
func (s *changeSuite) TestPerformCreateSymlinkWithoutBaseDir(c *C) {
	s.sys.InsertFault(`lstat "/base/name"`, syscall.ENOENT)
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "unused", Dir: "/base/name", Options: []string{"x-snapd.kind=symlink", "x-snapd.symlink=/oldname"}}}
	synth, err := chg.Perform(s.sec)
	c.Assert(err, IsNil)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/base/name"`, E: syscall.ENOENT},
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "base" 0755`},
		{C: `openat 3 "base" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `fchown 4 0 0`},
		{C: `close 3`},
		{C: `symlinkat "/oldname" 4 "name"`},
		{C: `close 4`},
	})
}

// Change.Perform wants to create a symlink but the base directory isn't there and cannot be created.
func (s *changeSuite) TestPerformCreateSymlinkWithoutBaseDirWithErrors(c *C) {
	s.sys.InsertFault(`lstat "/base/name"`, syscall.ENOENT)
	s.sys.InsertFault(`mkdirat 3 "base" 0755`, errTesting)
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "unused", Dir: "/base/name", Options: []string{"x-snapd.kind=symlink", "x-snapd.symlink=/oldname"}}}
	synth, err := chg.Perform(s.sec)
	c.Assert(err, ErrorMatches, `cannot create directory "/base": testing`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/base/name"`, E: syscall.ENOENT},
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "base" 0755`, E: errTesting},
		{C: `close 3`},
	})
}

// Change.Perform wants to create a symlink but the base directory isn't there and the parent is read-only.
func (s *changeSuite) TestPerformCreateSymlinkWithoutBaseDirAndReadOnlyBase(c *C) {
	s.sys.InsertFault(`lstat "/rofs/name"`, syscall.ENOENT)
	s.sys.InsertFault(`mkdirat 3 "rofs" 0755`, syscall.EEXIST)
	s.sys.InsertFault(`symlinkat "/oldname" 4 "name"`, syscall.EROFS, nil) // works on 2nd try
	s.sys.InsertSysLstatResult(`lstat "/rofs" <ptr>`, syscall.Stat_t{Uid: 0, Gid: 0, Mode: 0755})
	s.sys.InsertReadDirResult(`readdir "/rofs"`, nil) // pretend /rofs is empty.
	s.sys.InsertFault(`lstat "/tmp/.snap/rofs"`, syscall.ENOENT)
	s.sys.InsertOsLstatResult(`lstat "/rofs"`, testutil.FileInfoDir)
	s.sys.InsertFstatResult(`fstat 4 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFstatResult(`fstat 7 <ptr>`, syscall.Stat_t{})

	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "unused", Dir: "/rofs/name", Options: []string{"x-snapd.kind=symlink", "x-snapd.symlink=/oldname"}}}
	synth, err := chg.Perform(s.sec)
	c.Assert(err, IsNil)
	c.Assert(synth, DeepEquals, []*update.Change{
		{Action: update.Mount, Entry: osutil.MountEntry{
			Name: "tmpfs", Dir: "/rofs", Type: "tmpfs",
			Options: []string{"x-snapd.synthetic", "x-snapd.needed-by=/rofs/name", "mode=0755", "uid=0", "gid=0"}},
		},
	})
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		// sniff symlink name
		{C: `lstat "/rofs/name"`, E: syscall.ENOENT},

		// create base name (/rofs)
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "rofs" 0755`, E: syscall.EEXIST},
		{C: `openat 3 "rofs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `close 3`},
		// create symlink
		{C: `symlinkat "/oldname" 4 "name"`, E: syscall.EROFS},
		{C: `close 4`},

		// error, read only filesystem, create a mimic
		{C: `lstat "/rofs" <ptr>`, R: syscall.Stat_t{Mode: 0755}},
		{C: `readdir "/rofs"`, R: []os.FileInfo(nil)},
		{C: `lstat "/tmp/.snap/rofs"`, E: syscall.ENOENT},
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "tmp" 0755`},
		{C: `openat 3 "tmp" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `fchown 4 0 0`},
		{C: `mkdirat 4 ".snap" 0755`},
		{C: `openat 4 ".snap" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 5},
		{C: `fchown 5 0 0`},
		{C: `close 4`},
		{C: `close 3`},
		{C: `mkdirat 5 "rofs" 0755`},
		{C: `openat 5 "rofs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `fchown 3 0 0`},
		{C: `close 3`},
		{C: `close 5`},
		{C: `lstat "/rofs"`, R: testutil.FileInfoDir},

		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "rofs" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 4},
		{C: `fstat 4 <ptr>`, R: syscall.Stat_t{}},
		{C: `close 3`},
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "tmp" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 5},
		{C: `openat 5 ".snap" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 6},
		{C: `openat 6 "rofs" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 7},
		{C: `fstat 7 <ptr>`, R: syscall.Stat_t{}},
		{C: `close 6`},
		{C: `close 5`},
		{C: `close 3`},
		{C: `mount "/proc/self/fd/4" "/proc/self/fd/7" "" MS_BIND|MS_REC ""`},
		{C: `close 7`},
		{C: `close 4`},

		{C: `lstat "/rofs"`, R: testutil.FileInfoDir},
		{C: `mount "tmpfs" "/rofs" "tmpfs" 0 "mode=0755,uid=0,gid=0"`},
		{C: `unmount "/tmp/.snap/rofs" UMOUNT_NOFOLLOW|MNT_DETACH`},

		// mimic ready, re-try initial base mkdir
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "rofs" 0755`, E: syscall.EEXIST},
		{C: `openat 3 "rofs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `close 3`},
		// create symlink
		{C: `symlinkat "/oldname" 4 "name"`},
		{C: `close 4`},
	})
}

// Change.Perform wants to create a symlink but there's a file in the way.
func (s *changeSuite) TestPerformCreateSymlinkWithFileInTheWay(c *C) {
	s.sys.InsertOsLstatResult(`lstat "/name"`, testutil.FileInfoFile)
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "unused", Dir: "/name", Options: []string{"x-snapd.kind=symlink", "x-snapd.symlink=/oldname"}}}
	synth, err := chg.Perform(s.sec)
	c.Assert(err, ErrorMatches, `cannot create symlink in "/name": existing file in the way`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/name"`, R: testutil.FileInfoFile},
	})
}

// Change.Perform wants to create a symlink but a correct symlink is already present.
func (s *changeSuite) TestPerformCreateSymlinkWithGoodSymlinkPresent(c *C) {
	s.sys.InsertOsLstatResult(`lstat "/name"`, testutil.FileInfoSymlink)
	s.sys.InsertFault(`symlinkat "/oldname" 3 "name"`, syscall.EEXIST)
	s.sys.InsertFstatResult(`fstat 4 <ptr>`, syscall.Stat_t{Mode: syscall.S_IFLNK})
	s.sys.InsertReadlinkatResult(`readlinkat 4 "" <ptr>`, "/oldname")
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "unused", Dir: "/name", Options: []string{"x-snapd.kind=symlink", "x-snapd.symlink=/oldname"}}}
	synth, err := chg.Perform(s.sec)
	c.Assert(err, IsNil)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/name"`, R: testutil.FileInfoSymlink},
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `symlinkat "/oldname" 3 "name"`, E: syscall.EEXIST},
		{C: `openat 3 "name" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 4},
		{C: `fstat 4 <ptr>`, R: syscall.Stat_t{Mode: syscall.S_IFLNK}},
		{C: `readlinkat 4 "" <ptr>`, R: "/oldname"},
		{C: `close 4`},
		{C: `close 3`},
	})
}

// Change.Perform wants to create a symlink but a incorrect symlink is already present.
func (s *changeSuite) TestPerformCreateSymlinkWithBadSymlinkPresent(c *C) {
	s.sys.InsertOsLstatResult(`lstat "/name"`, testutil.FileInfoSymlink)
	s.sys.InsertFault(`symlinkat "/oldname" 3 "name"`, syscall.EEXIST)
	s.sys.InsertFstatResult(`fstat 4 <ptr>`, syscall.Stat_t{Mode: syscall.S_IFLNK})
	s.sys.InsertReadlinkatResult(`readlinkat 4 "" <ptr>`, "/evil")
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "unused", Dir: "/name", Options: []string{"x-snapd.kind=symlink", "x-snapd.symlink=/oldname"}}}
	synth, err := chg.Perform(s.sec)
	c.Assert(err, ErrorMatches, `cannot create symbolic link "/name": existing symbolic link in the way`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/name"`, R: testutil.FileInfoSymlink},
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `symlinkat "/oldname" 3 "name"`, E: syscall.EEXIST},
		{C: `openat 3 "name" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 4},
		{C: `fstat 4 <ptr>`, R: syscall.Stat_t{Mode: syscall.S_IFLNK}},
		{C: `readlinkat 4 "" <ptr>`, R: "/evil"},
		{C: `close 4`},
		{C: `close 3`},
	})
}

func (s *changeSuite) TestPerformRemoveSymlink(c *C) {
	chg := &update.Change{Action: update.Unmount, Entry: osutil.MountEntry{Name: "unused", Dir: "/name", Options: []string{"x-snapd.kind=symlink", "x-snapd.symlink=/oldname"}}}
	synth, err := chg.Perform(s.sec)
	c.Assert(err, IsNil)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `remove "/name"`},
	})
}

// ###########
// Topic: misc
// ###########

// Change.Perform handles unknown actions.
func (s *changeSuite) TestPerformUnknownAction(c *C) {
	chg := &update.Change{Action: update.Action(42)}
	synth, err := chg.Perform(s.sec)
	c.Assert(err, ErrorMatches, `cannot process mount change: unknown action: .*`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), HasLen, 0)
}

// Change.Perform wants to keep a mount entry unchanged.
func (s *changeSuite) TestPerformKeep(c *C) {
	chg := &update.Change{Action: update.Keep}
	synth, err := chg.Perform(s.sec)
	c.Assert(err, IsNil)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), HasLen, 0)
}
