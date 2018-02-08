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
	"syscall"

	. "gopkg.in/check.v1"

	update "github.com/snapcore/snapd/cmd/snap-update-ns"
	"github.com/snapcore/snapd/interfaces/mount"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/testutil"
)

type changeSuite struct {
	testutil.BaseTest
	sys *update.SyscallRecorder
}

var (
	errTesting = errors.New("testing")
)

var _ = Suite(&changeSuite{})

func (s *changeSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	// Mock and record system interactions.
	s.sys = &update.SyscallRecorder{}
	s.BaseTest.AddCleanup(update.MockSystemCalls(s.sys))
}

func (s *changeSuite) TestFakeFileInfo(c *C) {
	c.Assert(update.FileInfoDir.IsDir(), Equals, true)
	c.Assert(update.FileInfoFile.IsDir(), Equals, false)
	c.Assert(update.FileInfoSymlink.IsDir(), Equals, false)
}

func (s *changeSuite) TestString(c *C) {
	change := update.Change{
		Entry:  osutil.Entry{Dir: "/a/b", Name: "/dev/sda1"},
		Action: update.Mount,
	}
	c.Assert(change.String(), Equals, "mount (/dev/sda1 /a/b none defaults 0 0)")
}

// When there are no profiles we don't do anything.
func (s *changeSuite) TestNeededChangesNoProfiles(c *C) {
	current := &mount.Profile{}
	desired := &mount.Profile{}
	changes := update.NeededChanges(current, desired)
	c.Assert(changes, IsNil)
}

// When the profiles are the same we don't do anything.
func (s *changeSuite) TestNeededChangesNoChange(c *C) {
	current := &mount.Profile{Entries: []osutil.Entry{{Dir: "/common/stuff"}}}
	desired := &mount.Profile{Entries: []osutil.Entry{{Dir: "/common/stuff"}}}
	changes := update.NeededChanges(current, desired)
	c.Assert(changes, DeepEquals, []*update.Change{
		{Entry: osutil.Entry{Dir: "/common/stuff"}, Action: update.Keep},
	})
}

// When the content interface is connected we should mount the new entry.
func (s *changeSuite) TestNeededChangesTrivialMount(c *C) {
	current := &mount.Profile{}
	desired := &mount.Profile{Entries: []osutil.Entry{{Dir: "/common/stuff"}}}
	changes := update.NeededChanges(current, desired)
	c.Assert(changes, DeepEquals, []*update.Change{
		{Entry: desired.Entries[0], Action: update.Mount},
	})
}

// When the content interface is disconnected we should unmount the mounted entry.
func (s *changeSuite) TestNeededChangesTrivialUnmount(c *C) {
	current := &mount.Profile{Entries: []osutil.Entry{{Dir: "/common/stuff"}}}
	desired := &mount.Profile{}
	changes := update.NeededChanges(current, desired)
	c.Assert(changes, DeepEquals, []*update.Change{
		{Entry: current.Entries[0], Action: update.Unmount},
	})
}

// When umounting we unmount children before parents.
func (s *changeSuite) TestNeededChangesUnmountOrder(c *C) {
	current := &mount.Profile{Entries: []osutil.Entry{
		{Dir: "/common/stuff/extra"},
		{Dir: "/common/stuff"},
	}}
	desired := &mount.Profile{}
	changes := update.NeededChanges(current, desired)
	c.Assert(changes, DeepEquals, []*update.Change{
		{Entry: osutil.Entry{Dir: "/common/stuff/extra"}, Action: update.Unmount},
		{Entry: osutil.Entry{Dir: "/common/stuff"}, Action: update.Unmount},
	})
}

// When mounting we mount the parents before the children.
func (s *changeSuite) TestNeededChangesMountOrder(c *C) {
	current := &mount.Profile{}
	desired := &mount.Profile{Entries: []osutil.Entry{
		{Dir: "/common/stuff/extra"},
		{Dir: "/common/stuff"},
	}}
	changes := update.NeededChanges(current, desired)
	c.Assert(changes, DeepEquals, []*update.Change{
		{Entry: osutil.Entry{Dir: "/common/stuff"}, Action: update.Mount},
		{Entry: osutil.Entry{Dir: "/common/stuff/extra"}, Action: update.Mount},
	})
}

// When parent changes we don't reuse its children
func (s *changeSuite) TestNeededChangesChangedParentSameChild(c *C) {
	current := &mount.Profile{Entries: []osutil.Entry{
		{Dir: "/common/stuff", Name: "/dev/sda1"},
		{Dir: "/common/stuff/extra"},
		{Dir: "/common/unrelated"},
	}}
	desired := &mount.Profile{Entries: []osutil.Entry{
		{Dir: "/common/stuff", Name: "/dev/sda2"},
		{Dir: "/common/stuff/extra"},
		{Dir: "/common/unrelated"},
	}}
	changes := update.NeededChanges(current, desired)
	c.Assert(changes, DeepEquals, []*update.Change{
		{Entry: osutil.Entry{Dir: "/common/unrelated"}, Action: update.Keep},
		{Entry: osutil.Entry{Dir: "/common/stuff/extra"}, Action: update.Unmount},
		{Entry: osutil.Entry{Dir: "/common/stuff", Name: "/dev/sda1"}, Action: update.Unmount},
		{Entry: osutil.Entry{Dir: "/common/stuff", Name: "/dev/sda2"}, Action: update.Mount},
		{Entry: osutil.Entry{Dir: "/common/stuff/extra"}, Action: update.Mount},
	})
}

// When child changes we don't touch the unchanged parent
func (s *changeSuite) TestNeededChangesSameParentChangedChild(c *C) {
	current := &mount.Profile{Entries: []osutil.Entry{
		{Dir: "/common/stuff"},
		{Dir: "/common/stuff/extra", Name: "/dev/sda1"},
		{Dir: "/common/unrelated"},
	}}
	desired := &mount.Profile{Entries: []osutil.Entry{
		{Dir: "/common/stuff"},
		{Dir: "/common/stuff/extra", Name: "/dev/sda2"},
		{Dir: "/common/unrelated"},
	}}
	changes := update.NeededChanges(current, desired)
	c.Assert(changes, DeepEquals, []*update.Change{
		{Entry: osutil.Entry{Dir: "/common/unrelated"}, Action: update.Keep},
		{Entry: osutil.Entry{Dir: "/common/stuff/extra", Name: "/dev/sda1"}, Action: update.Unmount},
		{Entry: osutil.Entry{Dir: "/common/stuff"}, Action: update.Keep},
		{Entry: osutil.Entry{Dir: "/common/stuff/extra", Name: "/dev/sda2"}, Action: update.Mount},
	})
}

// Unused bind mount farms are unmounted.
func (s *changeSuite) TestNeededChangesTmpfsBindMountFarmUnused(c *C) {
	current := &mount.Profile{Entries: []osutil.Entry{{
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

	desired := &mount.Profile{}

	changes := update.NeededChanges(current, desired)

	c.Assert(changes, DeepEquals, []*update.Change{
		{Entry: osutil.Entry{
			Name:    "/var/lib/snapd/hostfs/snap/name/42/subdir/existing",
			Dir:     "/snap/name/42/subdir/existing",
			Options: []string{"bind", "ro", "x-snapd.needed-by=/snap/name/42/subdir", "x-snapd.synthetic"},
		}, Action: update.Unmount},
		{Entry: osutil.Entry{
			Name:    "/snap/other/123/libs",
			Dir:     "/snap/name/42/subdir/created",
			Options: []string{"bind", "ro"},
		}, Action: update.Unmount},
		{Entry: osutil.Entry{
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
	current := &mount.Profile{Entries: []osutil.Entry{{
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

	desired := &mount.Profile{Entries: []osutil.Entry{{
		// This is the only entry that we explicitly want but in order to
		// support it we need to keep the remaining implicit entries.
		Name:    "/snap/other/123/libs",
		Dir:     "/snap/name/42/subdir/created",
		Options: []string{"bind", "ro"},
	}}}

	changes := update.NeededChanges(current, desired)

	c.Assert(changes, DeepEquals, []*update.Change{
		{Entry: osutil.Entry{
			Name:    "/var/lib/snapd/hostfs/snap/name/42/subdir/existing",
			Dir:     "/snap/name/42/subdir/existing",
			Options: []string{"bind", "ro", "x-snapd.needed-by=/snap/name/42/subdir/created", "x-snapd.synthetic"},
		}, Action: update.Keep},
		{Entry: osutil.Entry{
			Name:    "/snap/other/123/libs",
			Dir:     "/snap/name/42/subdir/created",
			Options: []string{"bind", "ro"},
		}, Action: update.Keep},
		{Entry: osutil.Entry{
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
	current := &mount.Profile{Entries: []osutil.Entry{
		{Dir: "/a/b", Name: "/dev/sda1"},
		{Dir: "/a/b-1"},
		{Dir: "/a/b-1/3"},
		{Dir: "/a/b/c"},
	}}
	desired := &mount.Profile{Entries: []osutil.Entry{
		{Dir: "/a/b", Name: "/dev/sda2"},
		{Dir: "/a/b-1"},
		{Dir: "/a/b/c"},
	}}
	changes := update.NeededChanges(current, desired)
	c.Assert(changes, DeepEquals, []*update.Change{
		{Entry: osutil.Entry{Dir: "/a/b/c"}, Action: update.Unmount},
		{Entry: osutil.Entry{Dir: "/a/b", Name: "/dev/sda1"}, Action: update.Unmount},
		{Entry: osutil.Entry{Dir: "/a/b-1/3"}, Action: update.Unmount},
		{Entry: osutil.Entry{Dir: "/a/b-1"}, Action: update.Keep},

		{Entry: osutil.Entry{Dir: "/a/b", Name: "/dev/sda2"}, Action: update.Mount},
		{Entry: osutil.Entry{Dir: "/a/b/c"}, Action: update.Mount},
	})
}

// ########################################
// Topic: mounting & unmounting filesystems
// ########################################

// Change.Perform returns errors from os.Lstat (apart from ErrNotExist)
func (s *changeSuite) TestPerformFilesystemMountLstatError(c *C) {
	s.sys.InsertFault(`lstat "/target"`, errTesting)
	chg := &update.Change{Action: update.Mount, Entry: osutil.Entry{Name: "device", Dir: "/target", Type: "type"}}
	synth, err := chg.Perform()
	c.Assert(err, ErrorMatches, `cannot inspect "/target": testing`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.Calls(), DeepEquals, []string{`lstat "/target"`})
}

// Change.Perform wants to mount a filesystem.
func (s *changeSuite) TestPerformFilesystemMount(c *C) {
	s.sys.InsertLstatResult(`lstat "/target"`, update.FileInfoDir)
	chg := &update.Change{Action: update.Mount, Entry: osutil.Entry{Name: "device", Dir: "/target", Type: "type"}}
	synth, err := chg.Perform()
	c.Assert(err, IsNil)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`lstat "/target"`,
		`mount "device" "/target" "type" 0 ""`,
	})
}

// Change.Perform wants to mount a filesystem but it fails.
func (s *changeSuite) TestPerformFilesystemMountWithError(c *C) {
	s.sys.InsertLstatResult(`lstat "/target"`, update.FileInfoDir)
	s.sys.InsertFault(`mount "device" "/target" "type" 0 ""`, errTesting)
	chg := &update.Change{Action: update.Mount, Entry: osutil.Entry{Name: "device", Dir: "/target", Type: "type"}}
	synth, err := chg.Perform()
	c.Assert(err, Equals, errTesting)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`lstat "/target"`,
		`mount "device" "/target" "type" 0 ""`,
	})
}

// Change.Perform wants to mount a filesystem but the mount point isn't there.
func (s *changeSuite) TestPerformFilesystemMountWithoutMountPoint(c *C) {
	s.sys.InsertFault(`lstat "/target"`, syscall.ENOENT)
	chg := &update.Change{Action: update.Mount, Entry: osutil.Entry{Name: "device", Dir: "/target", Type: "type"}}
	synth, err := chg.Perform()
	c.Assert(err, IsNil)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`lstat "/target"`,
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`mkdirat 3 "target" 0755`,
		`openat 3 "target" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`fchown 4 0 0`,
		`close 4`,
		`close 3`,
		`mount "device" "/target" "type" 0 ""`,
	})
}

// Change.Perform wants to create a filesystem but the mount point isn't there and cannot be created.
func (s *changeSuite) TestPerformFilesystemMountWithoutMountPointWithErrors(c *C) {
	s.sys.InsertFault(`lstat "/target"`, syscall.ENOENT)
	s.sys.InsertFault(`mkdirat 3 "target" 0755`, errTesting)
	chg := &update.Change{Action: update.Mount, Entry: osutil.Entry{Name: "device", Dir: "/target", Type: "type"}}
	synth, err := chg.Perform()
	c.Assert(err, ErrorMatches, `cannot create path "/target": cannot mkdir path segment "target": testing`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`lstat "/target"`,
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`mkdirat 3 "target" 0755`,
		`close 3`,
	})
}

// Change.Perform wants to mount a filesystem but the mount point isn't there and the parent is read-only.
func (s *changeSuite) TestPerformFilesystemMountWithoutMountPointAndReadOnlyBase(c *C) {
	s.sys.InsertFault(`lstat "/rofs/target"`, syscall.ENOENT)
	s.sys.InsertFault(`mkdirat 3 "rofs" 0755`, syscall.EEXIST)
	s.sys.InsertFault(`openat 3 "target" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, syscall.ENOENT, nil) // works on 2nd try
	s.sys.InsertFault(`mkdirat 4 "target" 0755`, syscall.EROFS, nil)                               // works on 2nd try
	s.sys.InsertReadDirResult(`readdir "/rofs"`, nil)                                              // pretend /rofs is empty.
	s.sys.InsertFault(`lstat "/tmp/.snap/rofs"`, syscall.ENOENT)
	s.sys.InsertLstatResult(`lstat "/rofs"`, update.FileInfoDir)

	chg := &update.Change{Action: update.Mount, Entry: osutil.Entry{Name: "device", Dir: "/rofs/target", Type: "type"}}
	synth, err := chg.Perform()
	c.Assert(err, IsNil)
	c.Assert(synth, DeepEquals, []*update.Change{
		{Action: update.Mount, Entry: osutil.Entry{Name: "tmpfs", Dir: "/rofs", Type: "tmpfs"}},
	})
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		// sniff mount target
		`lstat "/rofs/target"`,

		// /rofs/target is missing, create it
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`mkdirat 3 "rofs" 0755`,
		`openat 3 "rofs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`close 3`,
		`mkdirat 4 "target" 0755`,
		`close 4`,

		// error, read only filesystem, create a mimic
		`readdir "/rofs"`,
		`lstat "/tmp/.snap/rofs"`,
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`mkdirat 3 "tmp" 0755`,
		`openat 3 "tmp" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`fchown 4 0 0`,
		`mkdirat 4 ".snap" 0755`,
		`openat 4 ".snap" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`fchown 5 0 0`,
		`close 4`,
		`close 3`,
		`mkdirat 5 "rofs" 0755`,
		`openat 5 "rofs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`fchown 3 0 0`,
		`close 3`,
		`close 5`,
		`lstat "/rofs"`,
		`mount "/rofs" "/tmp/.snap/rofs" "" MS_BIND ""`,
		`lstat "/rofs"`,
		`mount "tmpfs" "/rofs" "tmpfs" 0 ""`,
		`unmount "/tmp/.snap/rofs" UMOUNT_NOFOLLOW`,

		// mimic ready, re-try initial mkdir
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`mkdirat 3 "rofs" 0755`,
		`openat 3 "rofs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`close 3`,
		`mkdirat 4 "target" 0755`,
		`openat 4 "target" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`fchown 3 0 0`,
		`close 3`,
		`close 4`,

		// mount the filesystem
		`mount "device" "/rofs/target" "type" 0 ""`,
	})
}

// Change.Perform wants to mount a filesystem but the mount point isn't there and the parent is read-only and mimic fails during planning.
func (s *changeSuite) TestPerformFilesystemMountWithoutMountPointAndReadOnlyBaseErrorWhilePlanning(c *C) {
	s.sys.InsertFault(`lstat "/rofs/target"`, syscall.ENOENT)
	s.sys.InsertFault(`mkdirat 3 "rofs" 0755`, syscall.EEXIST)
	s.sys.InsertFault(`openat 3 "target" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, syscall.ENOENT)
	s.sys.InsertFault(`mkdirat 4 "target" 0755`, syscall.EROFS)
	s.sys.InsertFault(`lstat "/tmp/.snap/rofs"`, syscall.ENOENT)
	s.sys.InsertLstatResult(`lstat "/rofs"`, update.FileInfoDir)
	s.sys.InsertReadDirResult(`readdir "/rofs"`, nil)
	s.sys.InsertFault(`readdir "/rofs"`, errTesting) // make the writable mimic fail

	chg := &update.Change{Action: update.Mount, Entry: osutil.Entry{Name: "device", Dir: "/rofs/target", Type: "type"}}
	synth, err := chg.Perform()
	c.Assert(err, ErrorMatches, `cannot create writable mimic over "/rofs": testing`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		// sniff mount target
		`lstat "/rofs/target"`,

		// /rofs/target is missing, create it
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`mkdirat 3 "rofs" 0755`,
		`openat 3 "rofs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`close 3`,
		`mkdirat 4 "target" 0755`,
		`close 4`,

		// error, read only filesystem, create a mimic
		`readdir "/rofs"`,
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
	s.sys.InsertLstatResult(`lstat "/rofs"`, update.FileInfoDir)
	s.sys.InsertReadDirResult(`readdir "/rofs"`, nil)
	s.sys.InsertFault(`mkdirat 4 ".snap" 0755`, errTesting) // make the writable mimic fail

	chg := &update.Change{Action: update.Mount, Entry: osutil.Entry{Name: "device", Dir: "/rofs/target", Type: "type"}}
	synth, err := chg.Perform()
	c.Assert(err, ErrorMatches, `cannot create writable mimic over "/rofs": cannot create path "/tmp/.snap/rofs": cannot mkdir path segment ".snap": testing`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		// sniff mount target
		`lstat "/rofs/target"`,

		// /rofs/target is missing, create it
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`mkdirat 3 "rofs" 0755`,
		`openat 3 "rofs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`close 3`,
		`mkdirat 4 "target" 0755`,
		`close 4`,

		// error, read only filesystem, create a mimic
		`readdir "/rofs"`,
		`lstat "/tmp/.snap/rofs"`,
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`mkdirat 3 "tmp" 0755`,
		`openat 3 "tmp" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`fchown 4 0 0`,
		`mkdirat 4 ".snap" 0755`,
		`close 4`,
		`close 3`,
		// cannot create mimic, that's it
	})
}

// Change.Perform wants to mount a filesystem but there's a symlink in mount point.
func (s *changeSuite) TestPerformFilesystemMountWithSymlinkInMountPoint(c *C) {
	s.sys.InsertLstatResult(`lstat "/target"`, update.FileInfoSymlink)
	chg := &update.Change{Action: update.Mount, Entry: osutil.Entry{Name: "device", Dir: "/target", Type: "type"}}
	synth, err := chg.Perform()
	c.Assert(err, ErrorMatches, `cannot use "/target" as mount point: not a directory`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`lstat "/target"`,
	})
}

// Change.Perform wants to mount a filesystem but there's a file in mount point.
func (s *changeSuite) TestPerformFilesystemMountWithFileInMountPoint(c *C) {
	s.sys.InsertLstatResult(`lstat "/target"`, update.FileInfoFile)
	chg := &update.Change{Action: update.Mount, Entry: osutil.Entry{Name: "device", Dir: "/target", Type: "type"}}
	synth, err := chg.Perform()
	c.Assert(err, ErrorMatches, `cannot use "/target" as mount point: not a directory`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`lstat "/target"`,
	})
}

// Change.Perform wants to unmount a filesystem.
func (s *changeSuite) TestPerformFilesystemUnmount(c *C) {
	chg := &update.Change{Action: update.Unmount, Entry: osutil.Entry{Name: "device", Dir: "/target", Type: "type"}}
	synth, err := chg.Perform()
	c.Assert(err, IsNil)
	c.Assert(s.sys.Calls(), DeepEquals, []string{`unmount "/target" UMOUNT_NOFOLLOW`})
	c.Assert(synth, HasLen, 0)
}

// Change.Perform wants to unmount a filesystem but it fails.
func (s *changeSuite) TestPerformFilesystemUnmountError(c *C) {
	s.sys.InsertFault(`unmount "/target" UMOUNT_NOFOLLOW`, errTesting)
	chg := &update.Change{Action: update.Unmount, Entry: osutil.Entry{Name: "device", Dir: "/target", Type: "type"}}
	synth, err := chg.Perform()
	c.Assert(err, Equals, errTesting)
	c.Assert(s.sys.Calls(), DeepEquals, []string{`unmount "/target" UMOUNT_NOFOLLOW`})
	c.Assert(synth, HasLen, 0)
}

// Change.Perform passes non-flag options to the kernel.
func (s *changeSuite) TestPerformFilesystemMountWithOptions(c *C) {
	s.sys.InsertLstatResult(`lstat "/target"`, update.FileInfoDir)
	chg := &update.Change{Action: update.Mount, Entry: osutil.Entry{Name: "device", Dir: "/target", Type: "type", Options: []string{"ro", "funky"}}}
	synth, err := chg.Perform()
	c.Assert(err, IsNil)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`lstat "/target"`,
		`mount "device" "/target" "type" MS_RDONLY "funky"`,
	})
}

// Change.Perform doesn't pass snapd-specific options to the kernel.
func (s *changeSuite) TestPerformFilesystemMountWithSnapdSpecificOptions(c *C) {
	s.sys.InsertLstatResult(`lstat "/target"`, update.FileInfoDir)
	chg := &update.Change{Action: update.Mount, Entry: osutil.Entry{Name: "device", Dir: "/target", Type: "type", Options: []string{"ro", "x-snapd.funky"}}}
	synth, err := chg.Perform()
	c.Assert(err, IsNil)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`lstat "/target"`,
		`mount "device" "/target" "type" MS_RDONLY ""`,
	})
}

// ###############################################
// Topic: bind-mounting and unmounting directories
// ###############################################

// Change.Perform wants to bind mount a directory but the target cannot be stat'ed.
func (s *changeSuite) TestPerformDirectoryBindMountTargetLstatError(c *C) {
	s.sys.InsertFault(`lstat "/target"`, errTesting)
	chg := &update.Change{Action: update.Mount, Entry: osutil.Entry{Name: "/source", Dir: "/target", Options: []string{"bind"}}}
	synth, err := chg.Perform()
	c.Assert(err, ErrorMatches, `cannot inspect "/target": testing`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.Calls(), DeepEquals, []string{`lstat "/target"`})
}

// Change.Perform wants to bind mount a directory but the source cannot be stat'ed.
func (s *changeSuite) TestPerformDirectoryBindMountSourceLstatError(c *C) {
	s.sys.InsertLstatResult(`lstat "/target"`, update.FileInfoDir)
	s.sys.InsertFault(`lstat "/source"`, errTesting)
	chg := &update.Change{Action: update.Mount, Entry: osutil.Entry{Name: "/source", Dir: "/target", Options: []string{"bind"}}}
	synth, err := chg.Perform()
	c.Assert(err, ErrorMatches, `cannot inspect "/source": testing`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`lstat "/target"`,
		`lstat "/source"`,
	})
}

// Change.Perform wants to bind mount a directory.
func (s *changeSuite) TestPerformDirectoryBindMount(c *C) {
	s.sys.InsertLstatResult(`lstat "/source"`, update.FileInfoDir)
	s.sys.InsertLstatResult(`lstat "/target"`, update.FileInfoDir)
	chg := &update.Change{Action: update.Mount, Entry: osutil.Entry{Name: "/source", Dir: "/target", Options: []string{"bind"}}}
	synth, err := chg.Perform()
	c.Assert(err, IsNil)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`lstat "/target"`,
		`lstat "/source"`,
		`mount "/source" "/target" "" MS_BIND ""`,
	})
}

// Change.Perform wants to bind mount a directory but it fails.
func (s *changeSuite) TestPerformDirectoryBindMountWithError(c *C) {
	s.sys.InsertLstatResult(`lstat "/target"`, update.FileInfoDir)
	s.sys.InsertLstatResult(`lstat "/source"`, update.FileInfoDir)
	s.sys.InsertFault(`mount "/source" "/target" "" MS_BIND ""`, errTesting)
	chg := &update.Change{Action: update.Mount, Entry: osutil.Entry{Name: "/source", Dir: "/target", Options: []string{"bind"}}}
	synth, err := chg.Perform()
	c.Assert(err, Equals, errTesting)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`lstat "/target"`,
		`lstat "/source"`,
		`mount "/source" "/target" "" MS_BIND ""`,
	})
}

// Change.Perform wants to bind mount a directory but the mount point isn't there.
func (s *changeSuite) TestPerformDirectoryBindMountWithoutMountPoint(c *C) {
	s.sys.InsertLstatResult(`lstat "/source"`, update.FileInfoDir)
	s.sys.InsertFault(`lstat "/target"`, syscall.ENOENT)
	chg := &update.Change{Action: update.Mount, Entry: osutil.Entry{Name: "/source", Dir: "/target", Options: []string{"bind"}}}
	synth, err := chg.Perform()
	c.Assert(err, IsNil)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`lstat "/target"`,
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`mkdirat 3 "target" 0755`,
		`openat 3 "target" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`fchown 4 0 0`,
		`close 4`,
		`close 3`,
		`lstat "/source"`,
		`mount "/source" "/target" "" MS_BIND ""`,
	})
}

// Change.Perform wants to bind mount a directory but the mount source isn't there.
func (s *changeSuite) TestPerformDirectoryBindMountWithoutMountSource(c *C) {
	s.sys.InsertFault(`lstat "/source"`, syscall.ENOENT)
	s.sys.InsertLstatResult(`lstat "/target"`, update.FileInfoDir)
	chg := &update.Change{Action: update.Mount, Entry: osutil.Entry{Name: "/source", Dir: "/target", Options: []string{"bind"}}}
	synth, err := chg.Perform()
	c.Assert(err, IsNil)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`lstat "/target"`,
		`lstat "/source"`,
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`mkdirat 3 "source" 0755`,
		`openat 3 "source" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`fchown 4 0 0`,
		`close 4`,
		`close 3`,
		`mount "/source" "/target" "" MS_BIND ""`,
	})
}

// Change.Perform wants to create a directory bind mount but the mount point isn't there and cannot be created.
func (s *changeSuite) TestPerformDirectoryBindMountWithoutMountPointWithErrors(c *C) {
	s.sys.InsertLstatResult(`lstat "/source"`, update.FileInfoDir)
	s.sys.InsertFault(`lstat "/target"`, syscall.ENOENT)
	s.sys.InsertFault(`mkdirat 3 "target" 0755`, errTesting)
	chg := &update.Change{Action: update.Mount, Entry: osutil.Entry{Name: "/source", Dir: "/target", Options: []string{"bind"}}}
	synth, err := chg.Perform()
	c.Assert(err, ErrorMatches, `cannot create path "/target": cannot mkdir path segment "target": testing`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`lstat "/target"`,
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`mkdirat 3 "target" 0755`,
		`close 3`,
	})
}

// Change.Perform wants to create a directory bind mount but the mount source isn't there and cannot be created.
func (s *changeSuite) TestPerformDirectoryBindMountWithoutMountSourceWithErrors(c *C) {
	s.sys.InsertFault(`lstat "/source"`, syscall.ENOENT)
	s.sys.InsertFault(`mkdirat 3 "source" 0755`, errTesting)
	s.sys.InsertLstatResult(`lstat "/target"`, update.FileInfoDir)
	chg := &update.Change{Action: update.Mount, Entry: osutil.Entry{Name: "/source", Dir: "/target", Options: []string{"bind"}}}
	synth, err := chg.Perform()
	c.Assert(err, ErrorMatches, `cannot create path "/source": cannot mkdir path segment "source": testing`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`lstat "/target"`,
		`lstat "/source"`,
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`mkdirat 3 "source" 0755`,
		`close 3`,
	})
}

// Change.Perform wants to bind mount a directory but the mount point isn't there and the parent is read-only.
func (s *changeSuite) TestPerformDirectoryBindMountWithoutMountPointAndReadOnlyBase(c *C) {
	s.sys.InsertFault(`lstat "/rofs/target"`, syscall.ENOENT)
	s.sys.InsertFault(`mkdirat 3 "rofs" 0755`, syscall.EEXIST)
	s.sys.InsertFault(`openat 3 "target" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, syscall.ENOENT, nil) // works on 2nd try
	s.sys.InsertFault(`mkdirat 4 "target" 0755`, syscall.EROFS, nil)                               // works on 2nd try
	s.sys.InsertReadDirResult(`readdir "/rofs"`, nil)                                              // pretend /rofs is empty.
	s.sys.InsertFault(`lstat "/tmp/.snap/rofs"`, syscall.ENOENT)
	s.sys.InsertLstatResult(`lstat "/rofs"`, update.FileInfoDir)
	s.sys.InsertLstatResult(`lstat "/source"`, update.FileInfoDir)

	chg := &update.Change{Action: update.Mount, Entry: osutil.Entry{Name: "/source", Dir: "/rofs/target", Options: []string{"bind"}}}
	synth, err := chg.Perform()
	c.Assert(err, IsNil)
	c.Assert(synth, DeepEquals, []*update.Change{
		{Action: update.Mount, Entry: osutil.Entry{Name: "tmpfs", Dir: "/rofs", Type: "tmpfs"}},
	})
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		// sniff mount target
		`lstat "/rofs/target"`,

		// /rofs/target is missing, create it
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`mkdirat 3 "rofs" 0755`,
		`openat 3 "rofs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`close 3`,
		`mkdirat 4 "target" 0755`,
		`close 4`,

		// error, read only filesystem, create a mimic
		`readdir "/rofs"`,
		`lstat "/tmp/.snap/rofs"`,
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`mkdirat 3 "tmp" 0755`,
		`openat 3 "tmp" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`fchown 4 0 0`,
		`mkdirat 4 ".snap" 0755`,
		`openat 4 ".snap" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`fchown 5 0 0`,
		`close 4`,
		`close 3`,
		`mkdirat 5 "rofs" 0755`,
		`openat 5 "rofs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`fchown 3 0 0`,
		`close 3`,
		`close 5`,
		`lstat "/rofs"`,
		`mount "/rofs" "/tmp/.snap/rofs" "" MS_BIND ""`,
		`lstat "/rofs"`,
		`mount "tmpfs" "/rofs" "tmpfs" 0 ""`,
		`unmount "/tmp/.snap/rofs" UMOUNT_NOFOLLOW`,

		// mimic ready, re-try initial mkdir
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`mkdirat 3 "rofs" 0755`,
		`openat 3 "rofs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`close 3`,
		`mkdirat 4 "target" 0755`,
		`openat 4 "target" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`fchown 3 0 0`,
		`close 3`,
		`close 4`,

		// sniff mount source
		`lstat "/source"`,

		// mount the filesystem
		`mount "/source" "/rofs/target" "" MS_BIND ""`,
	})
}

// Change.Perform wants to bind mount a directory but there's a symlink in mount point.
func (s *changeSuite) TestPerformDirectoryBindMountWithSymlinkInMountPoint(c *C) {
	s.sys.InsertLstatResult(`lstat "/target"`, update.FileInfoSymlink)
	chg := &update.Change{Action: update.Mount, Entry: osutil.Entry{Name: "/source", Dir: "/target", Options: []string{"bind"}}}
	synth, err := chg.Perform()
	c.Assert(err, ErrorMatches, `cannot use "/target" as mount point: not a directory`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`lstat "/target"`,
	})
}

// Change.Perform wants to bind mount a directory but there's a file in mount mount.
func (s *changeSuite) TestPerformDirectoryBindMountWithFileInMountPoint(c *C) {
	s.sys.InsertLstatResult(`lstat "/target"`, update.FileInfoFile)
	chg := &update.Change{Action: update.Mount, Entry: osutil.Entry{Name: "/source", Dir: "/target", Options: []string{"bind"}}}
	synth, err := chg.Perform()
	c.Assert(err, ErrorMatches, `cannot use "/target" as mount point: not a directory`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`lstat "/target"`,
	})
}

// Change.Perform wants to bind mount a directory but there's a symlink in source.
func (s *changeSuite) TestPerformDirectoryBindMountWithSymlinkInMountSource(c *C) {
	s.sys.InsertLstatResult(`lstat "/target"`, update.FileInfoDir)
	s.sys.InsertLstatResult(`lstat "/source"`, update.FileInfoSymlink)
	chg := &update.Change{Action: update.Mount, Entry: osutil.Entry{Name: "/source", Dir: "/target", Options: []string{"bind"}}}
	synth, err := chg.Perform()
	c.Assert(err, ErrorMatches, `cannot use "/source" as bind-mount source: not a directory`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`lstat "/target"`,
		`lstat "/source"`,
	})
}

// Change.Perform wants to bind mount a directory but there's a file in source.
func (s *changeSuite) TestPerformDirectoryBindMountWithFileInMountSource(c *C) {
	s.sys.InsertLstatResult(`lstat "/target"`, update.FileInfoDir)
	s.sys.InsertLstatResult(`lstat "/source"`, update.FileInfoFile)
	chg := &update.Change{Action: update.Mount, Entry: osutil.Entry{Name: "/source", Dir: "/target", Options: []string{"bind"}}}
	synth, err := chg.Perform()
	c.Assert(err, ErrorMatches, `cannot use "/source" as bind-mount source: not a directory`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`lstat "/target"`,
		`lstat "/source"`,
	})
}

// Change.Perform wants to unmount a directory bind mount.
func (s *changeSuite) TestPerformDirectoryBindUnmount(c *C) {
	chg := &update.Change{Action: update.Unmount, Entry: osutil.Entry{Name: "/source", Dir: "/target", Options: []string{"bind"}}}
	synth, err := chg.Perform()
	c.Assert(err, IsNil)
	c.Assert(s.sys.Calls(), DeepEquals, []string{`unmount "/target" UMOUNT_NOFOLLOW`})
	c.Assert(synth, HasLen, 0)
}

// Change.Perform wants to unmount a directory bind mount but it fails.
func (s *changeSuite) TestPerformDirectoryBindUnmountError(c *C) {
	s.sys.InsertFault(`unmount "/target" UMOUNT_NOFOLLOW`, errTesting)
	chg := &update.Change{Action: update.Unmount, Entry: osutil.Entry{Name: "/source", Dir: "/target", Options: []string{"bind"}}}
	synth, err := chg.Perform()
	c.Assert(err, Equals, errTesting)
	c.Assert(s.sys.Calls(), DeepEquals, []string{`unmount "/target" UMOUNT_NOFOLLOW`})
	c.Assert(synth, HasLen, 0)
}

// #########################################
// Topic: bind-mounting and unmounting files
// #########################################

// Change.Perform wants to bind mount a file but the target cannot be stat'ed.
func (s *changeSuite) TestPerformFileBindMountTargetLstatError(c *C) {
	s.sys.InsertFault(`lstat "/target"`, errTesting)
	chg := &update.Change{Action: update.Mount, Entry: osutil.Entry{Name: "/source", Dir: "/target", Options: []string{"bind", "x-snapd.kind=file"}}}
	synth, err := chg.Perform()
	c.Assert(err, ErrorMatches, `cannot inspect "/target": testing`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.Calls(), DeepEquals, []string{`lstat "/target"`})
}

// Change.Perform wants to bind mount a file but the source cannot be stat'ed.
func (s *changeSuite) TestPerformFileBindMountSourceLstatError(c *C) {
	s.sys.InsertLstatResult(`lstat "/target"`, update.FileInfoFile)
	s.sys.InsertFault(`lstat "/source"`, errTesting)
	chg := &update.Change{Action: update.Mount, Entry: osutil.Entry{Name: "/source", Dir: "/target", Options: []string{"bind", "x-snapd.kind=file"}}}
	synth, err := chg.Perform()
	c.Assert(err, ErrorMatches, `cannot inspect "/source": testing`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`lstat "/target"`,
		`lstat "/source"`,
	})
}

// Change.Perform wants to bind mount a file.
func (s *changeSuite) TestPerformFileBindMount(c *C) {
	s.sys.InsertLstatResult(`lstat "/source"`, update.FileInfoFile)
	s.sys.InsertLstatResult(`lstat "/target"`, update.FileInfoFile)
	chg := &update.Change{Action: update.Mount, Entry: osutil.Entry{Name: "/source", Dir: "/target", Options: []string{"bind", "x-snapd.kind=file"}}}
	synth, err := chg.Perform()
	c.Assert(err, IsNil)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`lstat "/target"`,
		`lstat "/source"`,
		`mount "/source" "/target" "" MS_BIND ""`,
	})
}

// Change.Perform wants to bind mount a file but it fails.
func (s *changeSuite) TestPerformFileBindMountWithError(c *C) {
	s.sys.InsertLstatResult(`lstat "/target"`, update.FileInfoFile)
	s.sys.InsertLstatResult(`lstat "/source"`, update.FileInfoFile)
	s.sys.InsertFault(`mount "/source" "/target" "" MS_BIND ""`, errTesting)
	chg := &update.Change{Action: update.Mount, Entry: osutil.Entry{Name: "/source", Dir: "/target", Options: []string{"bind", "x-snapd.kind=file"}}}
	synth, err := chg.Perform()
	c.Assert(err, Equals, errTesting)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`lstat "/target"`,
		`lstat "/source"`,
		`mount "/source" "/target" "" MS_BIND ""`,
	})
}

// Change.Perform wants to bind mount a file but the mount point isn't there.
func (s *changeSuite) TestPerformFileBindMountWithoutMountPoint(c *C) {
	s.sys.InsertLstatResult(`lstat "/source"`, update.FileInfoFile)
	s.sys.InsertFault(`lstat "/target"`, syscall.ENOENT)
	chg := &update.Change{Action: update.Mount, Entry: osutil.Entry{Name: "/source", Dir: "/target", Options: []string{"bind", "x-snapd.kind=file"}}}
	synth, err := chg.Perform()
	c.Assert(err, IsNil)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`lstat "/target"`,
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`openat 3 "target" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`,
		`fchown 4 0 0`,
		`close 4`,
		`close 3`,
		`lstat "/source"`,
		`mount "/source" "/target" "" MS_BIND ""`,
	})
}

// Change.Perform wants to create a directory bind mount but the mount point isn't there and cannot be created.
func (s *changeSuite) TestPerformFileBindMountWithoutMountPointWithErrors(c *C) {
	s.sys.InsertLstatResult(`lstat "/source"`, update.FileInfoFile)
	s.sys.InsertFault(`lstat "/target"`, syscall.ENOENT)
	s.sys.InsertFault(`openat 3 "target" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`, errTesting)
	chg := &update.Change{Action: update.Mount, Entry: osutil.Entry{Name: "/source", Dir: "/target", Options: []string{"bind", "x-snapd.kind=file"}}}
	synth, err := chg.Perform()
	c.Assert(err, ErrorMatches, `cannot create path "/target": cannot open file "target": testing`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`lstat "/target"`,
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`openat 3 "target" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`,
		`close 3`,
	})
}

// Change.Perform wants to bind mount a file but the mount source isn't there.
func (s *changeSuite) TestPerformFileBindMountWithoutMountSource(c *C) {
	s.sys.InsertFault(`lstat "/source"`, syscall.ENOENT)
	s.sys.InsertLstatResult(`lstat "/target"`, update.FileInfoFile)
	chg := &update.Change{Action: update.Mount, Entry: osutil.Entry{Name: "/source", Dir: "/target", Options: []string{"bind", "x-snapd.kind=file"}}}
	synth, err := chg.Perform()
	c.Assert(err, IsNil)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`lstat "/target"`,
		`lstat "/source"`,
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`openat 3 "source" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`,
		`fchown 4 0 0`,
		`close 4`,
		`close 3`,
		`mount "/source" "/target" "" MS_BIND ""`,
	})
}

// Change.Perform wants to create a file bind mount but the mount source isn't there and cannot be created.
func (s *changeSuite) TestPerformFileBindMountWithoutMountSourceWithErrors(c *C) {
	s.sys.InsertFault(`lstat "/source"`, syscall.ENOENT)
	s.sys.InsertFault(`openat 3 "source" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`, errTesting)
	s.sys.InsertLstatResult(`lstat "/target"`, update.FileInfoFile)
	chg := &update.Change{Action: update.Mount, Entry: osutil.Entry{Name: "/source", Dir: "/target", Options: []string{"bind", "x-snapd.kind=file"}}}
	synth, err := chg.Perform()
	c.Assert(err, ErrorMatches, `cannot create path "/source": cannot open file "source": testing`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`lstat "/target"`,
		`lstat "/source"`,
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`openat 3 "source" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`,
		`close 3`,
	})
}

// Change.Perform wants to bind mount a file but the mount point isn't there and the parent is read-only.
func (s *changeSuite) TestPerformFileBindMountWithoutMountPointAndReadOnlyBase(c *C) {
	s.sys.InsertFault(`lstat "/rofs/target"`, syscall.ENOENT)
	s.sys.InsertFault(`mkdirat 3 "rofs" 0755`, syscall.EEXIST)
	s.sys.InsertFault(`openat 4 "target" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`, syscall.EROFS, nil) // works on 2nd try
	s.sys.InsertReadDirResult(`readdir "/rofs"`, nil)                                                   // pretend /rofs is empty.
	s.sys.InsertFault(`lstat "/tmp/.snap/rofs"`, syscall.ENOENT)
	s.sys.InsertLstatResult(`lstat "/rofs"`, update.FileInfoDir)
	s.sys.InsertLstatResult(`lstat "/source"`, update.FileInfoFile)

	chg := &update.Change{Action: update.Mount, Entry: osutil.Entry{Name: "/source", Dir: "/rofs/target", Options: []string{"bind", "x-snapd.kind=file"}}}
	synth, err := chg.Perform()
	c.Assert(err, IsNil)
	c.Assert(synth, DeepEquals, []*update.Change{
		{Action: update.Mount, Entry: osutil.Entry{Name: "tmpfs", Dir: "/rofs", Type: "tmpfs"}},
	})
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		// sniff mount target
		`lstat "/rofs/target"`,

		// /rofs/target is missing, create it
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`mkdirat 3 "rofs" 0755`,
		`openat 3 "rofs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`close 3`,
		`openat 4 "target" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`,
		`close 4`,

		// error, read only filesystem, create a mimic
		`readdir "/rofs"`,
		`lstat "/tmp/.snap/rofs"`,
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`mkdirat 3 "tmp" 0755`,
		`openat 3 "tmp" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`fchown 4 0 0`,
		`mkdirat 4 ".snap" 0755`,
		`openat 4 ".snap" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`fchown 5 0 0`,
		`close 4`,
		`close 3`,
		`mkdirat 5 "rofs" 0755`,
		`openat 5 "rofs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`fchown 3 0 0`,
		`close 3`,
		`close 5`,
		`lstat "/rofs"`,
		`mount "/rofs" "/tmp/.snap/rofs" "" MS_BIND ""`,
		`lstat "/rofs"`,
		`mount "tmpfs" "/rofs" "tmpfs" 0 ""`,
		`unmount "/tmp/.snap/rofs" UMOUNT_NOFOLLOW`,

		// mimic ready, re-try initial mkdir
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`mkdirat 3 "rofs" 0755`,
		`openat 3 "rofs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`close 3`,
		`openat 4 "target" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`,
		`fchown 3 0 0`,
		`close 3`,
		`close 4`,

		// sniff mount source
		`lstat "/source"`,

		// mount the filesystem
		`mount "/source" "/rofs/target" "" MS_BIND ""`,
	})
}

// Change.Perform wants to bind mount a file but there's a symlink in mount point.
func (s *changeSuite) TestPerformFileBindMountWithSymlinkInMountPoint(c *C) {
	s.sys.InsertLstatResult(`lstat "/target"`, update.FileInfoSymlink)
	s.sys.InsertLstatResult(`lstat "/source"`, update.FileInfoFile)
	chg := &update.Change{Action: update.Mount, Entry: osutil.Entry{Name: "/source", Dir: "/target", Options: []string{"bind", "x-snapd.kind=file"}}}
	synth, err := chg.Perform()
	c.Assert(err, ErrorMatches, `cannot use "/target" as mount point: not a regular file`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`lstat "/target"`,
	})
}

// Change.Perform wants to bind mount a file but there's a directory in mount point.
func (s *changeSuite) TestPerformBindMountFileWithDirectoryInMountPoint(c *C) {
	s.sys.InsertLstatResult(`lstat "/target"`, update.FileInfoDir)
	chg := &update.Change{Action: update.Mount, Entry: osutil.Entry{Name: "/source", Dir: "/target", Options: []string{"bind", "x-snapd.kind=file"}}}
	synth, err := chg.Perform()
	c.Assert(err, ErrorMatches, `cannot use "/target" as mount point: not a regular file`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`lstat "/target"`,
	})
}

// Change.Perform wants to bind mount a file but there's a symlink in source.
func (s *changeSuite) TestPerformFileBindMountWithSymlinkInMountSource(c *C) {
	s.sys.InsertLstatResult(`lstat "/target"`, update.FileInfoFile)
	s.sys.InsertLstatResult(`lstat "/source"`, update.FileInfoSymlink)
	chg := &update.Change{Action: update.Mount, Entry: osutil.Entry{Name: "/source", Dir: "/target", Options: []string{"bind", "x-snapd.kind=file"}}}
	synth, err := chg.Perform()
	c.Assert(err, ErrorMatches, `cannot use "/source" as bind-mount source: not a regular file`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`lstat "/target"`,
		`lstat "/source"`,
	})
}

// Change.Perform wants to bind mount a file but there's a directory in source.
func (s *changeSuite) TestPerformFileBindMountWithDirectoryInMountSource(c *C) {
	s.sys.InsertLstatResult(`lstat "/target"`, update.FileInfoFile)
	s.sys.InsertLstatResult(`lstat "/source"`, update.FileInfoDir)
	chg := &update.Change{Action: update.Mount, Entry: osutil.Entry{Name: "/source", Dir: "/target", Options: []string{"bind", "x-snapd.kind=file"}}}
	synth, err := chg.Perform()
	c.Assert(err, ErrorMatches, `cannot use "/source" as bind-mount source: not a regular file`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`lstat "/target"`,
		`lstat "/source"`,
	})
}

// Change.Perform wants to unmount a file bind mount.
func (s *changeSuite) TestPerformFileBindUnmount(c *C) {
	chg := &update.Change{Action: update.Unmount, Entry: osutil.Entry{Name: "/source", Dir: "/target", Options: []string{"bind", "x-snapd.kind=file"}}}
	synth, err := chg.Perform()
	c.Assert(err, IsNil)
	c.Assert(s.sys.Calls(), DeepEquals, []string{`unmount "/target" UMOUNT_NOFOLLOW`})
	c.Assert(synth, HasLen, 0)
}

// Change.Perform wants to unmount a file bind mount but it fails.
func (s *changeSuite) TestPerformFileBindUnmountError(c *C) {
	s.sys.InsertFault(`unmount "/target" UMOUNT_NOFOLLOW`, errTesting)
	chg := &update.Change{Action: update.Unmount, Entry: osutil.Entry{Name: "/source", Dir: "/target", Options: []string{"bind", "x-snapd.kind=file"}}}
	synth, err := chg.Perform()
	c.Assert(err, Equals, errTesting)
	c.Assert(s.sys.Calls(), DeepEquals, []string{`unmount "/target" UMOUNT_NOFOLLOW`})
	c.Assert(synth, HasLen, 0)
}

// ########################
// Topic: creating symlinks
// ########################

// Change.Perform wants to create a symlink but name cannot be stat'ed.
func (s *changeSuite) TestPerformCreateSymlinkNameLstatError(c *C) {
	s.sys.InsertFault(`lstat "/name"`, errTesting)
	chg := &update.Change{Action: update.Mount, Entry: osutil.Entry{Name: "unused", Dir: "/name", Options: []string{"x-snapd.kind=symlink", "x-snapd.symlink=/oldname"}}}
	synth, err := chg.Perform()
	c.Assert(err, ErrorMatches, `cannot inspect "/name": testing`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.Calls(), DeepEquals, []string{`lstat "/name"`})
}

// Change.Perform wants to create a symlink.
func (s *changeSuite) TestPerformCreateSymlink(c *C) {
	s.sys.InsertFault(`lstat "/name"`, syscall.ENOENT)
	chg := &update.Change{Action: update.Mount, Entry: osutil.Entry{Name: "unused", Dir: "/name", Options: []string{"x-snapd.kind=symlink", "x-snapd.symlink=/oldname"}}}
	synth, err := chg.Perform()
	c.Assert(err, IsNil)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`lstat "/name"`,
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`close 3`,
		`symlink "/name" -> "/oldname"`,
	})
}

// Change.Perform wants to create a symlink but it fails.
func (s *changeSuite) TestPerformCreateSymlinkWithError(c *C) {
	s.sys.InsertFault(`lstat "/name"`, syscall.ENOENT)
	s.sys.InsertFault(`symlink "/name" -> "/oldname"`, errTesting)
	chg := &update.Change{Action: update.Mount, Entry: osutil.Entry{Name: "unused", Dir: "/name", Options: []string{"x-snapd.kind=symlink", "x-snapd.symlink=/oldname"}}}
	synth, err := chg.Perform()
	c.Assert(err, ErrorMatches, `cannot create path "/name": testing`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`lstat "/name"`,
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`close 3`,
		`symlink "/name" -> "/oldname"`,
	})
}

// Change.Perform wants to create a symlink but the target is empty.
func (s *changeSuite) TestPerformCreateSymlinkWithNoTargetError(c *C) {
	s.sys.InsertFault(`lstat "/name"`, syscall.ENOENT)
	chg := &update.Change{Action: update.Mount, Entry: osutil.Entry{Name: "unused", Dir: "/name", Options: []string{"x-snapd.kind=symlink", "x-snapd.symlink="}}}
	synth, err := chg.Perform()
	c.Assert(err, ErrorMatches, `cannot create path "/name": cannot create symlink with empty target`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`lstat "/name"`,
	})
}

// Change.Perform wants to create a symlink but the base directory isn't there.
func (s *changeSuite) TestPerformCreateSymlinkWithoutBaseDir(c *C) {
	s.sys.InsertFault(`lstat "/base/name"`, syscall.ENOENT)
	chg := &update.Change{Action: update.Mount, Entry: osutil.Entry{Name: "unused", Dir: "/base/name", Options: []string{"x-snapd.kind=symlink", "x-snapd.symlink=/oldname"}}}
	synth, err := chg.Perform()
	c.Assert(err, IsNil)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`lstat "/base/name"`,
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`mkdirat 3 "base" 0755`,
		`openat 3 "base" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`fchown 4 0 0`,
		`close 4`,
		`close 3`,
		`symlink "/base/name" -> "/oldname"`,
	})
}

// Change.Perform wants to create a symlink but the base directory isn't there and cannot be created.
func (s *changeSuite) TestPerformCreateSymlinkWithoutBaseDirWithErrors(c *C) {
	s.sys.InsertFault(`lstat "/base/name"`, syscall.ENOENT)
	s.sys.InsertFault(`mkdirat 3 "base" 0755`, errTesting)
	chg := &update.Change{Action: update.Mount, Entry: osutil.Entry{Name: "unused", Dir: "/base/name", Options: []string{"x-snapd.kind=symlink", "x-snapd.symlink=/oldname"}}}
	synth, err := chg.Perform()
	c.Assert(err, ErrorMatches, `cannot create path "/base/name": cannot mkdir path segment "base": testing`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`lstat "/base/name"`,
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`mkdirat 3 "base" 0755`,
		`close 3`,
	})
}

// Change.Perform wants to create a symlink but the base directory isn't there and the parent is read-only.
func (s *changeSuite) TestPerformCreateSymlinkWithoutBaseDirAndReadOnlyBase(c *C) {
	s.sys.InsertFault(`lstat "/rofs/name"`, syscall.ENOENT)
	s.sys.InsertFault(`mkdirat 3 "rofs" 0755`, syscall.EEXIST)
	s.sys.InsertFault(`symlink "/rofs/name" -> "/oldname"`, syscall.EROFS, nil) // works on 2nd try
	s.sys.InsertReadDirResult(`readdir "/rofs"`, nil)                           // pretend /rofs is empty.
	s.sys.InsertFault(`lstat "/tmp/.snap/rofs"`, syscall.ENOENT)
	s.sys.InsertLstatResult(`lstat "/rofs"`, update.FileInfoDir)

	chg := &update.Change{Action: update.Mount, Entry: osutil.Entry{Name: "unused", Dir: "/rofs/name", Options: []string{"x-snapd.kind=symlink", "x-snapd.symlink=/oldname"}}}
	synth, err := chg.Perform()
	c.Assert(err, IsNil)
	c.Assert(synth, DeepEquals, []*update.Change{
		{Action: update.Mount, Entry: osutil.Entry{Name: "tmpfs", Dir: "/rofs", Type: "tmpfs"}},
	})
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		// sniff symlink name
		`lstat "/rofs/name"`,

		// create base name (/rofs)
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`mkdirat 3 "rofs" 0755`,
		`openat 3 "rofs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`close 4`,
		`close 3`,

		// create symlink
		`symlink "/rofs/name" -> "/oldname"`,

		// error, read only filesystem, create a mimic
		`readdir "/rofs"`,
		`lstat "/tmp/.snap/rofs"`,
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`mkdirat 3 "tmp" 0755`,
		`openat 3 "tmp" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`fchown 4 0 0`,
		`mkdirat 4 ".snap" 0755`,
		`openat 4 ".snap" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`fchown 5 0 0`,
		`close 4`,
		`close 3`,
		`mkdirat 5 "rofs" 0755`,
		`openat 5 "rofs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`fchown 3 0 0`,
		`close 3`,
		`close 5`,
		`lstat "/rofs"`,
		`mount "/rofs" "/tmp/.snap/rofs" "" MS_BIND ""`,
		`lstat "/rofs"`,
		`mount "tmpfs" "/rofs" "tmpfs" 0 ""`,
		`unmount "/tmp/.snap/rofs" UMOUNT_NOFOLLOW`,

		// mimic ready, re-try initial base mkdir
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`mkdirat 3 "rofs" 0755`,
		`openat 3 "rofs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`close 4`,
		`close 3`,

		// create symlink
		`symlink "/rofs/name" -> "/oldname"`,
	})
}

// Change.Perform wants to create a symlink but there's a file in the way.
func (s *changeSuite) TestPerformCreateSymlinkWithFileInTheWay(c *C) {
	s.sys.InsertLstatResult(`lstat "/name"`, update.FileInfoFile)
	chg := &update.Change{Action: update.Mount, Entry: osutil.Entry{Name: "unused", Dir: "/name", Options: []string{"x-snapd.kind=symlink", "x-snapd.symlink=/oldname"}}}
	synth, err := chg.Perform()
	c.Assert(err, ErrorMatches, `cannot create symlink in "/name": existing file in the way`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`lstat "/name"`,
	})
}

func (s *changeSuite) TestPerformRemoveSymlink(c *C) {
	chg := &update.Change{Action: update.Unmount, Entry: osutil.Entry{Name: "unused", Dir: "/name", Options: []string{"x-snapd.kind=symlink", "x-snapd.symlink=/oldname"}}}
	synth, err := chg.Perform()
	c.Assert(err, IsNil)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.Calls(), DeepEquals, []string{`remove "/name"`})
}

// ###########
// Topic: misc
// ###########

// Change.Perform handles unknown actions.
func (s *changeSuite) TestPerformUnknownAction(c *C) {
	chg := &update.Change{Action: update.Action(42)}
	synth, err := chg.Perform()
	c.Assert(err, ErrorMatches, `cannot process mount change: unknown action: .*`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.Calls(), HasLen, 0)
}

// Change.Perform wants to keep a mount entry unchanged.
func (s *changeSuite) TestPerformKeep(c *C) {
	chg := &update.Change{Action: update.Keep}
	synth, err := chg.Perform()
	c.Assert(err, IsNil)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.Calls(), HasLen, 0)
}
