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

	. "gopkg.in/check.v1"

	update "github.com/snapcore/snapd/cmd/snap-update-ns"
	"github.com/snapcore/snapd/interfaces/mount"
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
		Entry:  mount.Entry{Dir: "/a/b", Name: "/dev/sda1"},
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
	current := &mount.Profile{Entries: []mount.Entry{{Dir: "/common/stuff"}}}
	desired := &mount.Profile{Entries: []mount.Entry{{Dir: "/common/stuff"}}}
	changes := update.NeededChanges(current, desired)
	c.Assert(changes, DeepEquals, []*update.Change{
		{Entry: mount.Entry{Dir: "/common/stuff"}, Action: update.Keep},
	})
}

// When the content interface is connected we should mount the new entry.
func (s *changeSuite) TestNeededChangesTrivialMount(c *C) {
	current := &mount.Profile{}
	desired := &mount.Profile{Entries: []mount.Entry{{Dir: "/common/stuff"}}}
	changes := update.NeededChanges(current, desired)
	c.Assert(changes, DeepEquals, []*update.Change{
		{Entry: desired.Entries[0], Action: update.Mount},
	})
}

// When the content interface is disconnected we should unmount the mounted entry.
func (s *changeSuite) TestNeededChangesTrivialUnmount(c *C) {
	current := &mount.Profile{Entries: []mount.Entry{{Dir: "/common/stuff"}}}
	desired := &mount.Profile{}
	changes := update.NeededChanges(current, desired)
	c.Assert(changes, DeepEquals, []*update.Change{
		{Entry: current.Entries[0], Action: update.Unmount},
	})
}

// When umounting we unmount children before parents.
func (s *changeSuite) TestNeededChangesUnmountOrder(c *C) {
	current := &mount.Profile{Entries: []mount.Entry{
		{Dir: "/common/stuff/extra"},
		{Dir: "/common/stuff"},
	}}
	desired := &mount.Profile{}
	changes := update.NeededChanges(current, desired)
	c.Assert(changes, DeepEquals, []*update.Change{
		{Entry: mount.Entry{Dir: "/common/stuff/extra"}, Action: update.Unmount},
		{Entry: mount.Entry{Dir: "/common/stuff"}, Action: update.Unmount},
	})
}

// When mounting we mount the parents before the children.
func (s *changeSuite) TestNeededChangesMountOrder(c *C) {
	current := &mount.Profile{}
	desired := &mount.Profile{Entries: []mount.Entry{
		{Dir: "/common/stuff/extra"},
		{Dir: "/common/stuff"},
	}}
	changes := update.NeededChanges(current, desired)
	c.Assert(changes, DeepEquals, []*update.Change{
		{Entry: mount.Entry{Dir: "/common/stuff"}, Action: update.Mount},
		{Entry: mount.Entry{Dir: "/common/stuff/extra"}, Action: update.Mount},
	})
}

// When parent changes we don't reuse its children
func (s *changeSuite) TestNeededChangesChangedParentSameChild(c *C) {
	current := &mount.Profile{Entries: []mount.Entry{
		{Dir: "/common/stuff", Name: "/dev/sda1"},
		{Dir: "/common/stuff/extra"},
		{Dir: "/common/unrelated"},
	}}
	desired := &mount.Profile{Entries: []mount.Entry{
		{Dir: "/common/stuff", Name: "/dev/sda2"},
		{Dir: "/common/stuff/extra"},
		{Dir: "/common/unrelated"},
	}}
	changes := update.NeededChanges(current, desired)
	c.Assert(changes, DeepEquals, []*update.Change{
		{Entry: mount.Entry{Dir: "/common/unrelated"}, Action: update.Keep},
		{Entry: mount.Entry{Dir: "/common/stuff/extra"}, Action: update.Unmount},
		{Entry: mount.Entry{Dir: "/common/stuff", Name: "/dev/sda1"}, Action: update.Unmount},
		{Entry: mount.Entry{Dir: "/common/stuff", Name: "/dev/sda2"}, Action: update.Mount},
		{Entry: mount.Entry{Dir: "/common/stuff/extra"}, Action: update.Mount},
	})
}

// When child changes we don't touch the unchanged parent
func (s *changeSuite) TestNeededChangesSameParentChangedChild(c *C) {
	current := &mount.Profile{Entries: []mount.Entry{
		{Dir: "/common/stuff"},
		{Dir: "/common/stuff/extra", Name: "/dev/sda1"},
		{Dir: "/common/unrelated"},
	}}
	desired := &mount.Profile{Entries: []mount.Entry{
		{Dir: "/common/stuff"},
		{Dir: "/common/stuff/extra", Name: "/dev/sda2"},
		{Dir: "/common/unrelated"},
	}}
	changes := update.NeededChanges(current, desired)
	c.Assert(changes, DeepEquals, []*update.Change{
		{Entry: mount.Entry{Dir: "/common/unrelated"}, Action: update.Keep},
		{Entry: mount.Entry{Dir: "/common/stuff/extra", Name: "/dev/sda1"}, Action: update.Unmount},
		{Entry: mount.Entry{Dir: "/common/stuff"}, Action: update.Keep},
		{Entry: mount.Entry{Dir: "/common/stuff/extra", Name: "/dev/sda2"}, Action: update.Mount},
	})
}

// Unused bind mount farms are unmounted.
func (s *changeSuite) TestNeededChangesTmpfsBindMountFarmUnused(c *C) {
	current := &mount.Profile{Entries: []mount.Entry{{
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
		{Entry: mount.Entry{
			Name:    "/var/lib/snapd/hostfs/snap/name/42/subdir/existing",
			Dir:     "/snap/name/42/subdir/existing",
			Options: []string{"bind", "ro", "x-snapd.needed-by=/snap/name/42/subdir", "x-snapd.synthetic"},
		}, Action: update.Unmount},
		{Entry: mount.Entry{
			Name:    "/snap/other/123/libs",
			Dir:     "/snap/name/42/subdir/created",
			Options: []string{"bind", "ro"},
		}, Action: update.Unmount},
		{Entry: mount.Entry{
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
	current := &mount.Profile{Entries: []mount.Entry{{
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

	desired := &mount.Profile{Entries: []mount.Entry{{
		// This is the only entry that we explicitly want but in order to
		// support it we need to keep the remaining implicit entries.
		Name:    "/snap/other/123/libs",
		Dir:     "/snap/name/42/subdir/created",
		Options: []string{"bind", "ro"},
	}}}

	changes := update.NeededChanges(current, desired)

	c.Assert(changes, DeepEquals, []*update.Change{
		{Entry: mount.Entry{
			Name:    "/var/lib/snapd/hostfs/snap/name/42/subdir/existing",
			Dir:     "/snap/name/42/subdir/existing",
			Options: []string{"bind", "ro", "x-snapd.needed-by=/snap/name/42/subdir/created", "x-snapd.synthetic"},
		}, Action: update.Keep},
		{Entry: mount.Entry{
			Name:    "/snap/other/123/libs",
			Dir:     "/snap/name/42/subdir/created",
			Options: []string{"bind", "ro"},
		}, Action: update.Keep},
		{Entry: mount.Entry{
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
	current := &mount.Profile{Entries: []mount.Entry{
		{Dir: "/a/b", Name: "/dev/sda1"},
		{Dir: "/a/b-1"},
		{Dir: "/a/b-1/3"},
		{Dir: "/a/b/c"},
	}}
	desired := &mount.Profile{Entries: []mount.Entry{
		{Dir: "/a/b", Name: "/dev/sda2"},
		{Dir: "/a/b-1"},
		{Dir: "/a/b/c"},
	}}
	changes := update.NeededChanges(current, desired)
	c.Assert(changes, DeepEquals, []*update.Change{
		{Entry: mount.Entry{Dir: "/a/b/c"}, Action: update.Unmount},
		{Entry: mount.Entry{Dir: "/a/b", Name: "/dev/sda1"}, Action: update.Unmount},
		{Entry: mount.Entry{Dir: "/a/b-1/3"}, Action: update.Unmount},
		{Entry: mount.Entry{Dir: "/a/b-1"}, Action: update.Keep},

		{Entry: mount.Entry{Dir: "/a/b", Name: "/dev/sda2"}, Action: update.Mount},
		{Entry: mount.Entry{Dir: "/a/b/c"}, Action: update.Mount},
	})
}

// Change.Perform calls the mount system call.
func (s *changeSuite) TestPerformMount(c *C) {
	s.sys.InsertLstatResult(`lstat "/target"`, update.FileInfoDir)
	chg := &update.Change{Action: update.Mount, Entry: mount.Entry{Name: "/source", Dir: "/target", Type: "type"}}
	synth, err := chg.Perform()
	c.Assert(err, IsNil)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`lstat "/target"`,
		`mount "/source" "/target" "type" 0 ""`,
	})
}

// Change.Perform calls the mount system call (for directory bind mounts).
func (s *changeSuite) TestPerformBindMountDirectory(c *C) {
	s.sys.InsertLstatResult(`lstat "/source"`, update.FileInfoDir)
	s.sys.InsertLstatResult(`lstat "/target"`, update.FileInfoDir)
	chg := &update.Change{Action: update.Mount, Entry: mount.Entry{Name: "/source", Dir: "/target", Type: "type", Options: []string{"bind"}}}
	synth, err := chg.Perform()
	c.Check(err, IsNil)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`lstat "/target"`,
		`lstat "/source"`,
		`mount "/source" "/target" "type" MS_BIND ""`,
	})
}

// Change.Perform calls the mount system call (for file bind mounts).
func (s *changeSuite) TestPerformBindMountFile(c *C) {
	s.sys.InsertLstatResult(`lstat "/source"`, update.FileInfoFile)
	s.sys.InsertLstatResult(`lstat "/target"`, update.FileInfoFile)
	chg := &update.Change{Action: update.Mount, Entry: mount.Entry{Name: "/source", Dir: "/target", Type: "type", Options: []string{"bind", "x-snapd.kind=file"}}}
	synth, err := chg.Perform()
	c.Check(err, IsNil)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`lstat "/target"`,
		`lstat "/source"`,
		`mount "/source" "/target" "type" MS_BIND ""`,
	})
}

// Change.Perform creates the missing mount target.
func (s *changeSuite) TestPerformMountAutomaticMkdirTarget(c *C) {
	s.sys.InsertFault(`lstat "/target"`, os.ErrNotExist)
	chg := &update.Change{Action: update.Mount, Entry: mount.Entry{Name: "/source", Dir: "/target", Type: "type"}}
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
		`mount "/source" "/target" "type" 0 ""`,
	})
}

// Change.Perform creates the missing bind-mount source.
func (s *changeSuite) TestPerformMountAutomaticMkdirSource(c *C) {
	s.sys.InsertLstatResult(`lstat "/target"`, update.FileInfoDir)
	s.sys.InsertFault(`lstat "/source"`, os.ErrNotExist)
	chg := &update.Change{Action: update.Mount, Entry: mount.Entry{Name: "/source", Dir: "/target", Type: "type", Options: []string{"bind"}}}
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
		`mount "/source" "/target" "type" MS_BIND ""`,
	})
}

// Change.Perform rejects mount target if it is a symlink.
func (s *changeSuite) TestPerformMountRejectsTargetSymlink(c *C) {
	s.sys.InsertLstatResult(`lstat "/target"`, update.FileInfoSymlink)
	chg := &update.Change{Action: update.Mount, Entry: mount.Entry{Name: "/source", Dir: "/target", Type: "type"}}
	synth, err := chg.Perform()
	c.Assert(err, ErrorMatches, `cannot use "/target" as mount destination, not a directory`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`lstat "/target"`,
	})
}

// Change.Perform rejects bind-mount target if it is a symlink.
func (s *changeSuite) TestPerformBindMountRejectsTargetSymlink(c *C) {
	s.sys.InsertLstatResult(`lstat "/target"`, update.FileInfoSymlink)
	chg := &update.Change{Action: update.Mount, Entry: mount.Entry{Name: "/source", Dir: "/target", Type: "type", Options: []string{"bind"}}}
	synth, err := chg.Perform()
	c.Assert(err, ErrorMatches, `cannot use "/target" as mount destination, not a directory`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`lstat "/target"`,
	})
}

// Change.Perform rejects bind-mount source if it is a symlink.
func (s *changeSuite) TestPerformBindMountRejectsSourceSymlink(c *C) {
	s.sys.InsertLstatResult(`lstat "/target"`, update.FileInfoDir)
	s.sys.InsertLstatResult(`lstat "/source"`, update.FileInfoSymlink)
	chg := &update.Change{Action: update.Mount, Entry: mount.Entry{Name: "/source", Dir: "/target", Type: "type", Options: []string{"bind"}}}
	synth, err := chg.Perform()
	c.Assert(err, ErrorMatches, `cannot use "/source" as bind-mount source, not a directory`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`lstat "/target"`,
		`lstat "/source"`,
	})
}

// Change.Perform returns errors from os.Lstat (apart from ErrNotExist)
func (s *changeSuite) TestPerformMountLstatError(c *C) {
	s.sys.InsertFault(`lstat "/target"`, errTesting)
	chg := &update.Change{Action: update.Mount, Entry: mount.Entry{Name: "/source", Dir: "/target", Type: "type"}}
	synth, err := chg.Perform()
	c.Assert(err, ErrorMatches, `cannot inspect "/target": testing`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.Calls(), DeepEquals, []string{`lstat "/target"`})
}

// Change.Perform returns errors from os.MkdirAll
func (s *changeSuite) TestPerformMountMkdirAllError(c *C) {
	s.sys.InsertFault(`lstat "/target"`, os.ErrNotExist)
	s.sys.InsertFault(`mkdirat 3 "target" 0755`, errTesting)
	chg := &update.Change{Action: update.Mount, Entry: mount.Entry{Name: "/source", Dir: "/target", Type: "type"}}
	synth, err := chg.Perform()
	c.Assert(err, ErrorMatches, `cannot create file/directory "/target": cannot mkdir path segment "target": testing`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`lstat "/target"`,
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`mkdirat 3 "target" 0755`,
		`close 3`,
	})
}

// Change.Perform returns errors from mount system call
func (s *changeSuite) TestPerformMountError(c *C) {
	s.sys.InsertLstatResult(`lstat "/target"`, update.FileInfoDir)
	s.sys.InsertFault(`mount "/source" "/target" "type" 0 ""`, errTesting)
	chg := &update.Change{Action: update.Mount, Entry: mount.Entry{Name: "/source", Dir: "/target", Type: "type"}}
	synth, err := chg.Perform()
	c.Assert(err, Equals, errTesting)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`lstat "/target"`,
		`mount "/source" "/target" "type" 0 ""`,
	})
}

// Change.Perform passes unrecognized options to mount.
func (s *changeSuite) TestPerformMountOptions(c *C) {
	s.sys.InsertLstatResult(`lstat "/target"`, update.FileInfoDir)
	chg := &update.Change{Action: update.Mount, Entry: mount.Entry{Name: "/source", Dir: "/target", Type: "type", Options: []string{"funky"}}}
	synth, err := chg.Perform()
	c.Assert(err, IsNil)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`lstat "/target"`,
		`mount "/source" "/target" "type" 0 "funky"`,
	})
}

// Change.Perform calls the unmount system call.
func (s *changeSuite) TestPerformUnmount(c *C) {
	chg := &update.Change{Action: update.Unmount, Entry: mount.Entry{Name: "/source", Dir: "/target", Type: "type"}}
	synth, err := chg.Perform()
	c.Assert(err, IsNil)
	c.Assert(s.sys.Calls(), DeepEquals, []string{`unmount "/target" UMOUNT_NOFOLLOW`})
	c.Assert(synth, HasLen, 0)
}

// Change.Perform returns errors from unmount system call
func (s *changeSuite) TestPerformUnountError(c *C) {
	s.sys.InsertFault(`unmount "target" UMOUNT_NOFOLLOW`, errTesting)
	chg := &update.Change{Action: update.Unmount, Entry: mount.Entry{Name: "source", Dir: "target", Type: "type"}}
	synth, err := chg.Perform()
	c.Assert(err, Equals, errTesting)
	c.Assert(s.sys.Calls(), DeepEquals, []string{`unmount "target" UMOUNT_NOFOLLOW`})
	c.Assert(synth, HasLen, 0)
}

// Change.Perform handles unknown actions.
func (s *changeSuite) TestPerformUnknownAction(c *C) {
	chg := &update.Change{Action: update.Action(42)}
	synth, err := chg.Perform()
	c.Assert(err, ErrorMatches, `cannot process mount change, unknown action: .*`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.Calls(), HasLen, 0)
}

func (s *changeSuite) TestPerformSymlinkMount(c *C) {
	s.sys.InsertFault(`lstat "/name"`, os.ErrNotExist)
	chg := &update.Change{Action: update.Mount, Entry: mount.Entry{
		Name: "unused", Dir: "/name",
		Options: []string{"x-snapd.kind=symlink", "x-snapd.symlink=/target"}}}
	synth, err := chg.Perform()
	c.Assert(err, IsNil)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		// check what may be at /name
		`lstat "/name"`,
		// create base directory
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`close 3`,
		// create the symlink
		`symlink "/name" -> "/target"`,
	})
}

func (s *changeSuite) TestPerformSymlinkUnmount(c *C) {
	chg := &update.Change{Action: update.Unmount, Entry: mount.Entry{
		Name: "unused", Dir: "/name",
		Options: []string{"x-snapd.kind=symlink", "x-snapd.symlink=/target"}}}
	synth, err := chg.Perform()
	c.Assert(err, IsNil)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.Calls(), DeepEquals, []string{`remove "/name"`})
}
