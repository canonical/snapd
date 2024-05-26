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
	"io/fs"
	"path/filepath"
	"syscall"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	update "github.com/snapcore/snapd/cmd/snap-update-ns"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/sys"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/testutil"
)

type changeSuite struct {
	testutil.BaseTest
	sys *testutil.SyscallRecorder
	as  *update.Assumptions
}

var errTesting = errors.New("testing")

var _ = Suite(&changeSuite{})

func (s *changeSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	// This isolates us from host's experimental settings.
	dirs.SetRootDir(c.MkDir())
	// Mock and record system interactions.
	s.sys = &testutil.SyscallRecorder{}
	s.BaseTest.AddCleanup(update.MockSystemCalls(s.sys))
	s.as = &update.Assumptions{}
}

func (s *changeSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
	s.sys.CheckForStrayDescriptors(c)
	dirs.SetRootDir("")
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

// When the rootfs was setup by snap-confine, don't touch it
func (s *changeSuite) TestNeededChangesKeepRootfs(c *C) {
	current := &osutil.MountProfile{Entries: []osutil.MountEntry{
		{Dir: "/", Options: []string{"x-snapd.origin=rootfs"}},
	}}
	desired := &osutil.MountProfile{Entries: []osutil.MountEntry{{Dir: "/common/stuff"}}}
	changes := update.NeededChanges(current, desired)
	c.Assert(changes, DeepEquals, []*update.Change{
		{Entry: current.Entries[0], Action: update.Keep},
		{Entry: desired.Entries[0], Action: update.Mount},
	})
}

// When the rootfs was *not* setup by snap-confine, it's umounted
func (s *changeSuite) TestNeededChangesUmountRootfs(c *C) {
	current := &osutil.MountProfile{Entries: []osutil.MountEntry{
		// Like the test above, but without "x-snapd.origin=rootfs"
		{Dir: "/"},
	}}
	desired := &osutil.MountProfile{Entries: []osutil.MountEntry{{Dir: "/common/stuff"}}}
	changes := update.NeededChanges(current, desired)
	c.Assert(changes, DeepEquals, []*update.Change{
		{Entry: current.Entries[0], Action: update.Unmount},
		{Entry: desired.Entries[0], Action: update.Mount},
	})
}

// When umounting we unmount children before parents.
func (s *changeSuite) TestNeededChangesUnmountOrder(c *C) {
	current := &osutil.MountProfile{Entries: []osutil.MountEntry{
		{Dir: "/common/stuff"},
		{Dir: "/common/stuff/extra"},
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
	existingDirectories := []string{}

	restore := update.MockIsDirectory(func(path string) bool {
		return strutil.ListContains(existingDirectories, path)
	})
	defer restore()

	current := &osutil.MountProfile{}
	desired := &osutil.MountProfile{Entries: []osutil.MountEntry{
		{Dir: "/c/stuff/dir/symlink1"},
		{Dir: "/c/stuff/dir/file2", Options: []string{"x-snapd.kind=file"}},
		{Dir: "/c/stuff/dir"},
		{Dir: "/c/stuff"},
		{Dir: "/c/stuff/dir/file1", Options: []string{"x-snapd.kind=file"}},
	}}

	for _, testData := range []struct {
		existingDirs  []string
		expectedOrder []string
	}{
		{
			existingDirs:  []string{"/c"},
			expectedOrder: []string{"/c/stuff", "/c/stuff/dir", "/c/stuff/dir/file1", "/c/stuff/dir/file2", "/c/stuff/dir/symlink1"},
		},
		{
			existingDirs:  []string{"/c", "/c/stuff"},
			expectedOrder: []string{"/c/stuff", "/c/stuff/dir", "/c/stuff/dir/file1", "/c/stuff/dir/file2", "/c/stuff/dir/symlink1"},
		},
		{
			existingDirs:  []string{"/c", "/c/stuff", "/c/stuff/dir"},
			expectedOrder: []string{"/c/stuff", "/c/stuff/dir", "/c/stuff/dir/file1", "/c/stuff/dir/file2", "/c/stuff/dir/symlink1"},
		},
	} {
		existingDirectories = testData.existingDirs
		changes := update.NeededChanges(current, desired)

		// Check that every change is sane, and extract their path in order
		actualOrder := make([]string, 0, len(changes))
		for _, change := range changes {
			c.Check(change.Action, Equals, update.Mount)
			actualOrder = append(actualOrder, change.Entry.Dir)
		}

		c.Check(actualOrder, DeepEquals, testData.expectedOrder,
			Commentf("Existing dirs: %q", existingDirectories))
	}
}

func (s *changeSuite) TestNeededChangesMountFromReal(c *C) {
	existingDirectories := []string{}

	restore := update.MockIsDirectory(func(path string) bool {
		return strutil.ListContains(existingDirectories, path)
	})
	defer restore()

	current := &osutil.MountProfile{}
	desired := &osutil.MountProfile{Entries: []osutil.MountEntry{
		{Dir: "/snap/test-snapd-layout/x1/fooo-top", Options: []string{"x-snapd.origin=layout"}},
		{Dir: "/snap/test-snapd-layout/x1/fooo/deeper", Options: []string{"x-snapd.origin=layout"}},
		{Dir: "/usr/lib/x86_64-linux-gnu/wpe-webkit-1.0", Options: []string{"x-snapd.origin=layout"}},
		{Dir: "/usr/libexec/wpe-webkit-1.0", Options: []string{"x-snapd.origin=layout"}},
		{Dir: "/var/fooo-top", Options: []string{"x-snapd.origin=layout"}},
		{Dir: "/var/fooo/deeper", Options: []string{"x-snapd.origin=layout"}},
	}}

	for _, testData := range []struct {
		existingDirs  []string
		expectedOrder []string
	}{
		{
			existingDirs: []string{"/snap/test-snapd-layout/x1", "/usr", "/usr/lib/x86_64-linux-gnu", "/var"},
			expectedOrder: []string{
				"/snap/test-snapd-layout/x1/fooo-top", "/snap/test-snapd-layout/x1/fooo/deeper",
				// triggers a mimic on /usr
				"/usr/libexec/wpe-webkit-1.0",
				"/usr/lib/x86_64-linux-gnu/wpe-webkit-1.0",
				"/var/fooo-top", "/var/fooo/deeper",
			},
		},
		{
			existingDirs: []string{"/snap/test-snapd-layout/x1", "/usr", "/usr/lib/x86_64-linux-gnu", "/usr/libexec", "/var"},
			expectedOrder: []string{
				// parents for all dirs exists, so entries are
				// ordered lexicographically
				"/snap/test-snapd-layout/x1/fooo-top", "/snap/test-snapd-layout/x1/fooo/deeper",
				"/usr/lib/x86_64-linux-gnu/wpe-webkit-1.0", "/usr/libexec/wpe-webkit-1.0",
				"/var/fooo-top", "/var/fooo/deeper",
			},
		},
	} {
		existingDirectories = testData.existingDirs
		changes := update.NeededChanges(current, desired)

		// Check that every change is sane, and extract their path in order
		actualOrder := make([]string, 0, len(changes))
		for _, change := range changes {
			c.Check(change.Action, Equals, update.Mount)
			actualOrder = append(actualOrder, change.Entry.Dir)
		}

		c.Check(actualOrder, DeepEquals, testData.expectedOrder,
			Commentf("Existing dirs: %q", existingDirectories))
	}
}

func (s *changeSuite) TestNeededChangesKind(c *C) {
	current := &osutil.MountProfile{}
	desired := &osutil.MountProfile{Entries: []osutil.MountEntry{
		{Dir: "/common/file", Options: []string{"x-snapd.kind=file"}},
		{Dir: "/common/symlink", Options: []string{"x-snapd.kind=symlink"}},
	}}
	changes := update.NeededChanges(current, desired)
	c.Assert(changes, DeepEquals, []*update.Change{
		{Entry: osutil.MountEntry{Dir: "/common/file", Options: []string{"x-snapd.kind=file"}}, Action: update.Mount},
		{Entry: osutil.MountEntry{Dir: "/common/symlink", Options: []string{"x-snapd.kind=symlink"}}, Action: update.Mount},
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
			Name:    "/snap/other/123/libs",
			Dir:     "/snap/name/42/subdir/created",
			Options: []string{"bind", "ro", "x-snapd.detach"},
		}, Action: update.Unmount},
		{Entry: osutil.MountEntry{
			Name:    "/var/lib/snapd/hostfs/snap/name/42/subdir/existing",
			Dir:     "/snap/name/42/subdir/existing",
			Options: []string{"bind", "ro", "x-snapd.needed-by=/snap/name/42/subdir", "x-snapd.synthetic", "x-snapd.detach"},
		}, Action: update.Unmount},
		{Entry: osutil.MountEntry{
			Name:    "tmpfs",
			Dir:     "/snap/name/42/subdir",
			Type:    "tmpfs",
			Options: []string{"x-snapd.needed-by=/snap/name/42/subdir", "x-snapd.synthetic", "x-snapd.detach"},
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
			Name:    "/snap/other/123/libs",
			Dir:     "/snap/name/42/subdir/created",
			Options: []string{"bind", "ro"},
		}, Action: update.Keep},
		{Entry: osutil.MountEntry{
			Name:    "/var/lib/snapd/hostfs/snap/name/42/subdir/existing",
			Dir:     "/snap/name/42/subdir/existing",
			Options: []string{"bind", "ro", "x-snapd.needed-by=/snap/name/42/subdir/created", "x-snapd.synthetic"},
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
func (s *changeSuite) TestNeededChangesSmartEntryComparisonOld(c *C) {
	current := &osutil.MountProfile{Entries: []osutil.MountEntry{
		{Dir: "/a/b", Name: "/dev/sda1"},
		{Dir: "/a/b/c"},
		{Dir: "/a/b-1"},
		{Dir: "/a/b-1/3"},
	}}
	desired := &osutil.MountProfile{Entries: []osutil.MountEntry{
		{Dir: "/a/b", Name: "/dev/sda2"},
		{Dir: "/a/b-1"},
		{Dir: "/a/b/c"},
	}}
	changes := update.NeededChanges(current, desired)
	for _, chg := range changes {
		c.Logf("- %+v", chg)
	}
	c.Assert(changes, DeepEquals, []*update.Change{
		{Entry: osutil.MountEntry{Dir: "/a/b-1/3"}, Action: update.Unmount},
		{Entry: osutil.MountEntry{Dir: "/a/b-1"}, Action: update.Keep},
		{Entry: osutil.MountEntry{Dir: "/a/b/c"}, Action: update.Unmount},
		{Entry: osutil.MountEntry{Dir: "/a/b", Name: "/dev/sda1"}, Action: update.Unmount},

		{Entry: osutil.MountEntry{Dir: "/a/b", Name: "/dev/sda2"}, Action: update.Mount},
		{Entry: osutil.MountEntry{Dir: "/a/b/c"}, Action: update.Mount},
	})
}

// Parallel instance changes are executed first
func (s *changeSuite) TestNeededChangesParallelInstancesManyComeFirst(c *C) {
	desired := &osutil.MountProfile{Entries: []osutil.MountEntry{
		{Dir: "/common/stuff", Name: "/dev/sda1"},
		{Dir: "/common/stuff/extra"},
		{Dir: "/common/unrelated"},
		{Dir: "/foo/bar", Name: "/foo/bar_bar", Options: []string{osutil.XSnapdOriginOvername()}},
		{Dir: "/snap/foo", Name: "/snap/foo_bar", Options: []string{osutil.XSnapdOriginOvername()}},
	}}
	changes := update.NeededChanges(&osutil.MountProfile{}, desired)
	c.Assert(changes, DeepEquals, []*update.Change{
		{Entry: osutil.MountEntry{Dir: "/foo/bar", Name: "/foo/bar_bar", Options: []string{osutil.XSnapdOriginOvername()}}, Action: update.Mount},
		{Entry: osutil.MountEntry{Dir: "/snap/foo", Name: "/snap/foo_bar", Options: []string{osutil.XSnapdOriginOvername()}}, Action: update.Mount},
		{Entry: osutil.MountEntry{Dir: "/common/stuff", Name: "/dev/sda1"}, Action: update.Mount},
		{Entry: osutil.MountEntry{Dir: "/common/stuff/extra"}, Action: update.Mount},
		{Entry: osutil.MountEntry{Dir: "/common/unrelated"}, Action: update.Mount},
	})
}

// Parallel instance changes are kept if already present
func (s *changeSuite) TestNeededChangesParallelInstancesKeep(c *C) {
	desired := &osutil.MountProfile{Entries: []osutil.MountEntry{
		{Dir: "/common/stuff", Name: "/dev/sda1"},
		{Dir: "/common/unrelated"},
		{Dir: "/foo/bar", Name: "/foo/bar_bar", Options: []string{osutil.XSnapdOriginOvername()}},
		{Dir: "/snap/foo", Name: "/snap/foo_bar", Options: []string{osutil.XSnapdOriginOvername()}},
	}}
	current := &osutil.MountProfile{Entries: []osutil.MountEntry{
		{Dir: "/foo/bar", Name: "/foo/bar_bar", Options: []string{osutil.XSnapdOriginOvername()}},
		{Dir: "/snap/foo", Name: "/snap/foo_bar", Options: []string{osutil.XSnapdOriginOvername()}},
	}}
	changes := update.NeededChanges(current, desired)
	c.Assert(changes, DeepEquals, []*update.Change{
		{Entry: osutil.MountEntry{Dir: "/snap/foo", Name: "/snap/foo_bar", Options: []string{osutil.XSnapdOriginOvername()}}, Action: update.Keep},
		{Entry: osutil.MountEntry{Dir: "/foo/bar", Name: "/foo/bar_bar", Options: []string{osutil.XSnapdOriginOvername()}}, Action: update.Keep},
		{Entry: osutil.MountEntry{Dir: "/common/stuff", Name: "/dev/sda1"}, Action: update.Mount},
		{Entry: osutil.MountEntry{Dir: "/common/unrelated"}, Action: update.Mount},
	})
}

// Parallel instance with mounts inside
func (s *changeSuite) TestNeededChangesParallelInstancesInsideMount(c *C) {
	desired := &osutil.MountProfile{Entries: []osutil.MountEntry{
		{Dir: "/foo/bar/baz"},
		{Dir: "/foo/bar", Name: "/foo/bar_bar", Options: []string{osutil.XSnapdOriginOvername()}},
		{Dir: "/snap/foo", Name: "/snap/foo_bar", Options: []string{osutil.XSnapdOriginOvername()}},
	}}
	current := &osutil.MountProfile{Entries: []osutil.MountEntry{
		{Dir: "/foo/bar", Name: "/foo/bar_bar", Options: []string{osutil.XSnapdOriginOvername()}},
		{Dir: "/foo/bar/zed"},
		{Dir: "/snap/foo", Name: "/snap/foo_bar", Options: []string{osutil.XSnapdOriginOvername()}},
	}}
	changes := update.NeededChanges(current, desired)
	c.Assert(changes, DeepEquals, []*update.Change{
		{Entry: osutil.MountEntry{Dir: "/snap/foo", Name: "/snap/foo_bar", Options: []string{osutil.XSnapdOriginOvername()}}, Action: update.Keep},
		{Entry: osutil.MountEntry{Dir: "/foo/bar/zed"}, Action: update.Unmount},
		{Entry: osutil.MountEntry{Dir: "/foo/bar", Name: "/foo/bar_bar", Options: []string{osutil.XSnapdOriginOvername()}}, Action: update.Keep},
		{Entry: osutil.MountEntry{Dir: "/foo/bar/baz"}, Action: update.Mount},
	})
}

func (s *changeSuite) TestNeededChangesRepeatedDir(c *C) {
	desired := &osutil.MountProfile{Entries: []osutil.MountEntry{
		{Name: "tmpfs", Dir: "/foo/mytmp", Type: "tmpfs", Options: []string{osutil.XSnapdOriginLayout()}},
	}}
	current := &osutil.MountProfile{Entries: []osutil.MountEntry{
		{
			Name: "/foo/bar", Dir: "/foo/bar", Type: "none",
			Options: []string{osutil.XSnapdSynthetic(), osutil.XSnapdNeededBy("/foo/mytmp")},
		},
		{Name: "tmpfs", Dir: "/foo/mytmp", Type: "tmpfs", Options: []string{osutil.XSnapdOriginLayout()}},
		{
			Name: "tmpfs", Dir: "/foo/bar", Type: "tmpfs",
			Options: []string{osutil.XSnapdSynthetic(), osutil.XSnapdNeededBy("/foo/bar/two")},
		},
	}}
	changes := update.NeededChanges(current, desired)

	// Make sure that we unmount the one that is needed by an entry
	// not desired anymore (even though it is in the same mountpoint
	// as the one needed by /foo/mytmp).
	c.Assert(changes, DeepEquals, []*update.Change{
		{Entry: osutil.MountEntry{
			Name: "tmpfs", Dir: "/foo/bar", Type: "tmpfs",
			Options: []string{osutil.XSnapdSynthetic(), osutil.XSnapdNeededBy("/foo/bar/two"), osutil.XSnapdDetach()},
		}, Action: update.Unmount},
		{Entry: osutil.MountEntry{
			Name: "tmpfs", Dir: "/foo/mytmp", Type: "tmpfs",
			Options: []string{osutil.XSnapdOriginLayout()},
		}, Action: update.Keep},
		{Entry: osutil.MountEntry{
			Name: "/foo/bar", Dir: "/foo/bar", Type: "none",
			Options: []string{osutil.XSnapdSynthetic(), osutil.XSnapdNeededBy("/foo/mytmp")},
		}, Action: update.Keep},
	})
}

func (s *changeSuite) TestRuntimeUsingSymlinks(c *C) {
	dirs.SetRootDir(c.MkDir())
	defer func() {
		dirs.SetRootDir("")
	}()

	optDir := filepath.Join(dirs.GlobalRootDir, "/opt")
	optFooRuntimeDir := filepath.Join(dirs.GlobalRootDir, "/opt/foo-runtime")
	snapAppX1FooRuntimeDir := filepath.Join(dirs.GlobalRootDir, "/snap/app/x1/foo-runtime")
	snapAppX2FooRuntimeDir := filepath.Join(dirs.GlobalRootDir, "/snap/app/x2/foo-runtime")
	snapFooRuntimeX1OptFooRuntime := filepath.Join(dirs.GlobalRootDir, "/snap/foo-runtime/x1/opt/foo-runtime")

	// We start with a runtime shared from one snap to another and then exposed
	// to /opt with a symbolic link. This is the initial state of the
	// application in version v1.
	initial := &osutil.MountProfile{}
	desiredV1 := &osutil.MountProfile{Entries: []osutil.MountEntry{
		{Name: "none", Dir: optFooRuntimeDir, Type: "none", Options: []string{"x-snapd.kind=symlink", "x-snapd.symlink=" + snapAppX1FooRuntimeDir, "x-snapd.origin=layout"}},
		{Name: snapFooRuntimeX1OptFooRuntime, Dir: snapAppX1FooRuntimeDir, Type: "none", Options: []string{"bind", "ro"}},
	}}
	// The changes we compute are trivial, simply perform each operation in order.
	changes := update.NeededChanges(initial, desiredV1)
	c.Assert(changes, DeepEquals, []*update.Change{
		{Entry: osutil.MountEntry{Name: snapFooRuntimeX1OptFooRuntime, Dir: snapAppX1FooRuntimeDir, Type: "none", Options: []string{"bind", "ro"}}, Action: update.Mount},
		{Entry: osutil.MountEntry{Name: "none", Dir: optFooRuntimeDir, Type: "none", Options: []string{"x-snapd.kind=symlink", "x-snapd.symlink=" + snapAppX1FooRuntimeDir, "x-snapd.origin=layout"}}, Action: update.Mount},
	})
	// After performing both changes we have a new synthesized entry. We get an
	// extra writable mimic over /opt so that we can add our symlink. The
	// content sharing into $SNAP is applied as expected since the snap ships
	// the required mount point.
	currentV1 := &osutil.MountProfile{Entries: []osutil.MountEntry{
		{Name: snapFooRuntimeX1OptFooRuntime, Dir: snapAppX1FooRuntimeDir, Type: "none", Options: []string{"bind", "ro"}},
		{Name: "none", Dir: optFooRuntimeDir, Type: "none", Options: []string{"x-snapd.kind=symlink", "x-snapd.symlink=" + snapAppX1FooRuntimeDir, "x-snapd.origin=layout"}},
		{Name: "tmpfs", Dir: optDir, Type: "tmpfs", Options: []string{"x-snapd.synthetic", "x-snapd.needed-by=" + optFooRuntimeDir, "mode=0755", "uid=0", "gid=0"}},
	}}
	// We now proceed to replace app v1 with v2 which uses a bind mount instead
	// of a symlink. First, let's start with the updated desired profile:
	desiredV2 := &osutil.MountProfile{Entries: []osutil.MountEntry{
		{Name: snapAppX2FooRuntimeDir, Dir: optFooRuntimeDir, Type: "none", Options: []string{"rbind", "rw", "x-snapd.origin=layout"}},
		{Name: snapFooRuntimeX1OptFooRuntime, Dir: snapAppX2FooRuntimeDir, Type: "none", Options: []string{"bind", "ro"}},
	}}

	// Let's see what the update algorithm thinks.
	changes = update.NeededChanges(currentV1, desiredV2)
	c.Assert(changes, DeepEquals, []*update.Change{
		// We are keeping /opt, but re-creating /opt from scratch.
		{Entry: osutil.MountEntry{Name: "tmpfs", Dir: optDir, Type: "tmpfs", Options: []string{"x-snapd.synthetic", "x-snapd.needed-by=" + optFooRuntimeDir, "mode=0755", "uid=0", "gid=0"}}, Action: update.Keep},
		{Entry: osutil.MountEntry{Name: "none", Dir: optFooRuntimeDir, Type: "none", Options: []string{"x-snapd.kind=symlink", "x-snapd.symlink=" + snapAppX1FooRuntimeDir, "x-snapd.origin=layout"}}, Action: update.Unmount},
		// We are dropping the content interface bind mount because app changed revision
		{Entry: osutil.MountEntry{Name: snapFooRuntimeX1OptFooRuntime, Dir: snapAppX1FooRuntimeDir, Type: "none", Options: []string{"bind", "ro", "x-snapd.detach"}}, Action: update.Unmount},
		// We also adding the updated path of the content interface (for revision x2)
		{Entry: osutil.MountEntry{Name: snapFooRuntimeX1OptFooRuntime, Dir: snapAppX2FooRuntimeDir, Type: "none", Options: []string{"bind", "ro"}}, Action: update.Mount},
		// We are adding a new bind mount for /opt/foo-runtime
		{Entry: osutil.MountEntry{Name: snapAppX2FooRuntimeDir, Dir: optFooRuntimeDir, Type: "none", Options: []string{"rbind", "rw", "x-snapd.origin=layout"}}, Action: update.Mount},
	})

	// After performing all those changes this is the profile we observe.
	currentV2 := &osutil.MountProfile{Entries: []osutil.MountEntry{
		{Name: "tmpfs", Dir: optDir, Type: "tmpfs", Options: []string{"x-snapd.synthetic", "x-snapd.needed-by=" + optFooRuntimeDir, "mode=0755", "uid=0", "gid=0", "x-snapd.detach"}},
		{Name: snapAppX2FooRuntimeDir, Dir: optFooRuntimeDir, Type: "none", Options: []string{"rbind", "rw", "x-snapd.origin=layout"}},
		{Name: snapFooRuntimeX1OptFooRuntime, Dir: snapAppX2FooRuntimeDir, Type: "none", Options: []string{"bind", "ro"}},
	}}

	// So far so good. To trigger the issue we now revert or refresh to v1
	// again. Let's see what happens here. The desired profiles are already
	// known so let's see what the algorithm thinks now.
	changes = update.NeededChanges(currentV2, desiredV1)
	c.Assert(changes, DeepEquals, []*update.Change{
		// We are dropping the content interface bind mount because app changed revision
		{Entry: osutil.MountEntry{Name: snapFooRuntimeX1OptFooRuntime, Dir: snapAppX2FooRuntimeDir, Type: "none", Options: []string{"bind", "ro", "x-snapd.detach"}}, Action: update.Unmount},
		// We are also dropping the bind mount from /opt/runtime since we want a symlink instead
		{Entry: osutil.MountEntry{Name: snapAppX2FooRuntimeDir, Dir: optFooRuntimeDir, Type: "none", Options: []string{"rbind", "rw", "x-snapd.origin=layout", "x-snapd.detach"}}, Action: update.Unmount},
		// Keep the tmpfs on /opt
		{Entry: osutil.MountEntry{Name: "tmpfs", Dir: optDir, Type: "tmpfs", Options: []string{"x-snapd.synthetic", "x-snapd.needed-by=" + optFooRuntimeDir, "mode=0755", "uid=0", "gid=0", "x-snapd.detach"}}, Action: update.Keep},
		// We are bind mounting the runtime from another snap into $SNAP/foo-runtime
		{Entry: osutil.MountEntry{Name: snapFooRuntimeX1OptFooRuntime, Dir: snapAppX1FooRuntimeDir, Type: "none", Options: []string{"bind", "ro"}}, Action: update.Mount},
		// We are providing a symlink /opt/foo-runtime -> to $SNAP/foo-runtime.
		{Entry: osutil.MountEntry{Name: "none", Dir: optFooRuntimeDir, Type: "none", Options: []string{"x-snapd.kind=symlink", "x-snapd.symlink=" + snapAppX1FooRuntimeDir, "x-snapd.origin=layout"}}, Action: update.Mount},
	})

	// The problem is that the tmpfs contains leftovers from the things we
	// created and those prevent the execution of this mount profile.
}

// ########################################
// Topic: mounting & unmounting filesystems
// ########################################

// Change.Perform returns errors from os.Lstat (apart from ErrNotExist)
func (s *changeSuite) TestPerformFilesystemMountLstatError(c *C) {
	s.sys.InsertFault(`lstat "/target"`, errTesting)
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "device", Dir: "/target", Type: "type"}}
	synth := mylog.Check2(chg.Perform(s.as))
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
	synth := mylog.Check2(chg.Perform(s.as))

	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/target"`, R: testutil.FileInfoDir},
		{C: `mount "device" "/target" "type" 0 ""`},
	})
}

// Change.Perform wants to mount a filesystem with sharing changes.
func (s *changeSuite) TestPerformFilesystemMountAndShareChanges(c *C) {
	s.sys.InsertOsLstatResult(`lstat "/target"`, testutil.FileInfoDir)
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "device", Dir: "/target", Type: "type", Options: []string{"shared"}}}
	synth := mylog.Check2(chg.Perform(s.as))

	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/target"`, R: testutil.FileInfoDir},
		{C: `mount "device" "/target" "type" 0 ""`},
		{C: `mount "none" "/target" "" MS_SHARED ""`},
	})
}

// Change.Perform wants to mount a filesystem but it fails.
func (s *changeSuite) TestPerformFilesystemMountWithError(c *C) {
	s.sys.InsertOsLstatResult(`lstat "/target"`, testutil.FileInfoDir)
	s.sys.InsertFault(`mount "device" "/target" "type" 0 ""`, errTesting)
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "device", Dir: "/target", Type: "type"}}
	synth := mylog.Check2(chg.Perform(s.as))
	c.Assert(err, Equals, errTesting)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/target"`, R: testutil.FileInfoDir},
		{C: `mount "device" "/target" "type" 0 ""`, E: errTesting},
	})
}

// Change.Perform wants to mount a filesystem with sharing changes but mounting fails.
func (s *changeSuite) TestPerformFilesystemMountAndShareWithError(c *C) {
	s.sys.InsertOsLstatResult(`lstat "/target"`, testutil.FileInfoDir)
	s.sys.InsertFault(`mount "device" "/target" "type" 0 ""`, errTesting)
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "device", Dir: "/target", Type: "type", Options: []string{"shared"}}}
	synth := mylog.Check2(chg.Perform(s.as))
	c.Assert(err, Equals, errTesting)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/target"`, R: testutil.FileInfoDir},
		{C: `mount "device" "/target" "type" 0 ""`, E: errTesting},
	})
}

// Change.Perform wants to mount a filesystem but the mount point isn't there.
func (s *changeSuite) TestPerformFilesystemMountWithoutMountPoint(c *C) {
	defer s.as.MockUnrestrictedPaths("/")() // Treat test path as unrestricted.
	s.sys.InsertFault(`lstat "/target"`, syscall.ENOENT)
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "device", Dir: "/target", Type: "type"}}
	synth := mylog.Check2(chg.Perform(s.as))

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
	defer s.as.MockUnrestrictedPaths("/")() // Treat test path as unrestricted.
	s.sys.InsertFault(`lstat "/target"`, syscall.ENOENT)
	s.sys.InsertFault(`mkdirat 3 "target" 0755`, errTesting)
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "device", Dir: "/target", Type: "type"}}
	synth := mylog.Check2(chg.Perform(s.as))
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
	defer s.as.MockUnrestrictedPaths("/")() // Treat test path as unrestricted.
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
	s.sys.InsertFstatResult(`fstat 6 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFstatfsResult(`fstatfs 6 <ptr>`, syscall.Statfs_t{})

	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "device", Dir: "/rofs/target", Type: "type"}}
	synth := mylog.Check2(chg.Perform(s.as))

	c.Assert(synth, DeepEquals, []*update.Change{
		{
			Action: update.Mount, Entry: osutil.MountEntry{
				Name: "tmpfs", Dir: "/rofs", Type: "tmpfs",
				Options: []string{"x-snapd.synthetic", "x-snapd.needed-by=/rofs/target", "mode=0755", "uid=0", "gid=0"},
			},
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
		{C: `readdir "/rofs"`, R: []fs.DirEntry(nil)},
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
		{C: `mount "none" "/tmp/.snap/rofs" "" MS_REC|MS_PRIVATE ""`},
		{C: `unmount "/tmp/.snap/rofs" UMOUNT_NOFOLLOW|MNT_DETACH`},

		// Perform clean up after the unmount operation.
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "tmp" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 4},
		{C: `openat 4 ".snap" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 5},
		{C: `openat 5 "rofs" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 6},
		{C: `fstat 6 <ptr>`, R: syscall.Stat_t{}},
		{C: `close 5`},
		{C: `close 4`},
		{C: `close 3`},
		{C: `fstatfs 6 <ptr>`, R: syscall.Statfs_t{}},
		{C: `remove "/tmp/.snap/rofs"`},
		{C: `close 6`},

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
	defer s.as.MockUnrestrictedPaths("/")() // Treat test path as unrestricted.
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
	synth := mylog.Check2(chg.Perform(s.as))
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
	defer s.as.MockUnrestrictedPaths("/")() // Treat test path as unrestricted.
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
	synth := mylog.Check2(chg.Perform(s.as))
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
		{C: `readdir "/rofs"`, R: []fs.DirEntry(nil)},
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
	synth := mylog.Check2(chg.Perform(s.as))
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
	synth := mylog.Check2(chg.Perform(s.as))
	c.Assert(err, ErrorMatches, `cannot use "/target" as mount point: not a directory`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/target"`, R: testutil.FileInfoFile},
	})
}

// Change.Perform wants to unmount a filesystem.
func (s *changeSuite) TestPerformFilesystemUnmount(c *C) {
	s.sys.InsertFstatResult(`fstat 4 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFstatfsResult(`fstatfs 4 <ptr>`, syscall.Statfs_t{})
	chg := &update.Change{Action: update.Unmount, Entry: osutil.MountEntry{Name: "device", Dir: "/target", Type: "type"}}
	synth := mylog.Check2(chg.Perform(s.as))

	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `unmount "/target" UMOUNT_NOFOLLOW`},
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "target" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 4},
		{C: `fstat 4 <ptr>`, R: syscall.Stat_t{}},
		{C: `close 3`},
		{C: `fstatfs 4 <ptr>`, R: syscall.Statfs_t{}},
		{C: `remove "/target"`},
		{C: `close 4`},
	})
	c.Assert(synth, HasLen, 0)
}

// Change.Perform wants to detach a bind mount.
func (s *changeSuite) TestPerformFilesystemDetch(c *C) {
	s.sys.InsertFstatResult(`fstat 4 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFstatfsResult(`fstatfs 4 <ptr>`, syscall.Statfs_t{})
	chg := &update.Change{Action: update.Unmount, Entry: osutil.MountEntry{Name: "/something", Dir: "/target", Options: []string{"x-snapd.detach"}}}
	synth := mylog.Check2(chg.Perform(s.as))

	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `mount "none" "/target" "" MS_REC|MS_PRIVATE ""`},
		{C: `unmount "/target" UMOUNT_NOFOLLOW|MNT_DETACH`},

		// Perform clean up after the unmount operation.
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "target" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 4},
		{C: `fstat 4 <ptr>`, R: syscall.Stat_t{}},
		{C: `close 3`},
		{C: `fstatfs 4 <ptr>`, R: syscall.Statfs_t{}},
		{C: `remove "/target"`},
		{C: `close 4`},
	})
	c.Assert(synth, HasLen, 0)
}

// Change.Perform wants to unmount a filesystem but it fails.
func (s *changeSuite) TestPerformFilesystemUnmountError(c *C) {
	s.sys.InsertFault(`unmount "/target" UMOUNT_NOFOLLOW`, errTesting)
	chg := &update.Change{Action: update.Unmount, Entry: osutil.MountEntry{Name: "device", Dir: "/target", Type: "type"}}
	synth := mylog.Check2(chg.Perform(s.as))
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
	synth := mylog.Check2(chg.Perform(s.as))

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
	synth := mylog.Check2(chg.Perform(s.as))

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
	synth := mylog.Check2(chg.Perform(s.as))
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
	synth := mylog.Check2(chg.Perform(s.as))
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
	synth := mylog.Check2(chg.Perform(s.as))

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

// Change.Perform wants to bind mount a directory with sharing changes.
func (s *changeSuite) TestPerformRecursiveDirectorySharedBindMount(c *C) {
	s.sys.InsertOsLstatResult(`lstat "/source"`, testutil.FileInfoDir)
	s.sys.InsertOsLstatResult(`lstat "/target"`, testutil.FileInfoDir)
	s.sys.InsertFstatResult(`fstat 4 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFstatResult(`fstat 5 <ptr>`, syscall.Stat_t{})
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "/source", Dir: "/target", Options: []string{"rshared", "rbind"}}}
	synth := mylog.Check2(chg.Perform(s.as))

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
		{C: `mount "/proc/self/fd/4" "/proc/self/fd/5" "" MS_BIND|MS_REC ""`},
		{C: `close 5`},
		{C: `close 4`},
		{C: `mount "none" "/target" "" MS_REC|MS_SHARED ""`},
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
	synth := mylog.Check2(chg.Perform(s.as))
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
	defer s.as.MockUnrestrictedPaths("/")() // Treat test path as unrestricted.
	s.sys.InsertOsLstatResult(`lstat "/source"`, testutil.FileInfoDir)
	s.sys.InsertFault(`lstat "/target"`, syscall.ENOENT)
	s.sys.InsertFstatResult(`fstat 4 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFstatResult(`fstat 5 <ptr>`, syscall.Stat_t{})
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "/source", Dir: "/target", Options: []string{"bind"}}}
	synth := mylog.Check2(chg.Perform(s.as))

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
	defer s.as.MockUnrestrictedPaths("/")() // Treat test path as unrestricted.
	s.sys.InsertFault(`lstat "/source"`, syscall.ENOENT)
	s.sys.InsertOsLstatResult(`lstat "/target"`, testutil.FileInfoDir)
	s.sys.InsertFstatResult(`fstat 4 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFstatResult(`fstat 5 <ptr>`, syscall.Stat_t{})
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "/source", Dir: "/target", Options: []string{"bind"}}}
	synth := mylog.Check2(chg.Perform(s.as))

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
	defer s.as.MockUnrestrictedPaths("/")() // Treat test path as unrestricted.
	s.sys.InsertFault(`lstat "/target"`, syscall.ENOENT)
	s.sys.InsertFault(`mkdirat 3 "target" 0755`, errTesting)
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "/source", Dir: "/target", Options: []string{"bind"}}}
	synth := mylog.Check2(chg.Perform(s.as))
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
	defer s.as.MockUnrestrictedPaths("/")() // Treat test path as unrestricted.
	s.sys.InsertFault(`lstat "/source"`, syscall.ENOENT)
	s.sys.InsertFault(`mkdirat 3 "source" 0755`, errTesting)
	s.sys.InsertOsLstatResult(`lstat "/target"`, testutil.FileInfoDir)
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "/source", Dir: "/target", Options: []string{"bind"}}}
	synth := mylog.Check2(chg.Perform(s.as))
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
	defer s.as.MockUnrestrictedPaths("/")() // Treat test path as unrestricted.
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
	s.sys.InsertFstatResult(`fstat 6 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFstatfsResult(`fstatfs 6 <ptr>`, syscall.Statfs_t{})

	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "/source", Dir: "/rofs/target", Options: []string{"bind"}}}
	synth := mylog.Check2(chg.Perform(s.as))

	c.Assert(synth, DeepEquals, []*update.Change{
		{
			Action: update.Mount, Entry: osutil.MountEntry{
				Name: "tmpfs", Dir: "/rofs", Type: "tmpfs",
				Options: []string{"x-snapd.synthetic", "x-snapd.needed-by=/rofs/target", "mode=0755", "uid=0", "gid=0"},
			},
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
		{C: `readdir "/rofs"`, R: []fs.DirEntry(nil)},
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
		{C: `mount "none" "/tmp/.snap/rofs" "" MS_REC|MS_PRIVATE ""`},
		{C: `unmount "/tmp/.snap/rofs" UMOUNT_NOFOLLOW|MNT_DETACH`},

		// Perform clean up after the unmount operation.
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "tmp" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 4},
		{C: `openat 4 ".snap" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 5},
		{C: `openat 5 "rofs" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 6},
		{C: `fstat 6 <ptr>`, R: syscall.Stat_t{}},
		{C: `close 5`},
		{C: `close 4`},
		{C: `close 3`},
		{C: `fstatfs 6 <ptr>`, R: syscall.Statfs_t{}},
		{C: `remove "/tmp/.snap/rofs"`},
		{C: `close 6`},

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
	defer s.as.MockUnrestrictedPaths("/")() // Treat test path as unrestricted.
	s.sys.InsertOsLstatResult(`lstat "/rofs"`, testutil.FileInfoDir)
	s.sys.InsertFault(`lstat "/rofs/source"`, syscall.ENOENT)
	s.sys.InsertOsLstatResult(`lstat "/target"`, testutil.FileInfoDir)
	s.sys.InsertFault(`mkdirat 3 "rofs" 0755`, syscall.EEXIST)
	s.sys.InsertFault(`mkdirat 4 "source" 0755`, syscall.EROFS)

	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "/rofs/source", Dir: "/target", Options: []string{"bind"}}}
	synth := mylog.Check2(chg.Perform(s.as))
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
	defer s.as.MockUnrestrictedPaths("/")() // Treat test path as unrestricted.
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
	s.sys.InsertFstatResult(`fstat 6 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFstatfsResult(`fstatfs 6 <ptr>`, syscall.Statfs_t{})

	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "/rofs/source", Dir: "/target", Options: []string{"bind", "x-snapd.origin=layout"}}}
	synth := mylog.Check2(chg.Perform(s.as))

	c.Check(synth, DeepEquals, []*update.Change{
		{
			Action: update.Mount, Entry: osutil.MountEntry{
				Name: "tmpfs", Dir: "/rofs", Type: "tmpfs",
				Options: []string{"x-snapd.synthetic", "x-snapd.needed-by=/target", "mode=0755", "uid=0", "gid=0"},
			},
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
		{C: `readdir "/rofs"`, R: []fs.DirEntry(nil)},
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
		{C: `mount "none" "/tmp/.snap/rofs" "" MS_REC|MS_PRIVATE ""`},
		{C: `unmount "/tmp/.snap/rofs" UMOUNT_NOFOLLOW|MNT_DETACH`},

		// Perform clean up after the unmount operation.
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "tmp" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 4},
		{C: `openat 4 ".snap" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 5},
		{C: `openat 5 "rofs" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 6},
		{C: `fstat 6 <ptr>`, R: syscall.Stat_t{}},
		{C: `close 5`},
		{C: `close 4`},
		{C: `close 3`},
		{C: `fstatfs 6 <ptr>`, R: syscall.Statfs_t{}},
		{C: `remove "/tmp/.snap/rofs"`},
		{C: `close 6`},

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
	synth := mylog.Check2(chg.Perform(s.as))
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
	synth := mylog.Check2(chg.Perform(s.as))
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
	synth := mylog.Check2(chg.Perform(s.as))
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
	synth := mylog.Check2(chg.Perform(s.as))
	c.Assert(err, ErrorMatches, `cannot use "/source" as bind-mount source: not a directory`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/target"`, R: testutil.FileInfoDir},
		{C: `lstat "/source"`, R: testutil.FileInfoFile},
	})
}

// Change.Perform wants to unmount a directory bind mount.
func (s *changeSuite) TestPerformDirectoryBindUnmount(c *C) {
	s.sys.InsertFstatResult(`fstat 4 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFstatfsResult(`fstatfs 4 <ptr>`, syscall.Statfs_t{})
	chg := &update.Change{Action: update.Unmount, Entry: osutil.MountEntry{Name: "/source", Dir: "/target", Options: []string{"bind"}}}
	synth := mylog.Check2(chg.Perform(s.as))

	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `unmount "/target" UMOUNT_NOFOLLOW`},

		// Perform clean up after the unmount operation.
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "target" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 4},
		{C: `fstat 4 <ptr>`, R: syscall.Stat_t{}},
		{C: `close 3`},
		{C: `fstatfs 4 <ptr>`, R: syscall.Statfs_t{}},
		{C: `remove "/target"`},
		{C: `close 4`},
	})
	c.Assert(synth, HasLen, 0)
}

// Change.Perform wants to unmount a directory bind mount but it fails.
func (s *changeSuite) TestPerformDirectoryBindUnmountError(c *C) {
	s.sys.InsertFault(`unmount "/target" UMOUNT_NOFOLLOW`, errTesting)
	chg := &update.Change{Action: update.Unmount, Entry: osutil.MountEntry{Name: "/source", Dir: "/target", Options: []string{"bind"}}}
	synth := mylog.Check2(chg.Perform(s.as))
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
	synth := mylog.Check2(chg.Perform(s.as))
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
	synth := mylog.Check2(chg.Perform(s.as))
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
	synth := mylog.Check2(chg.Perform(s.as))

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
	synth := mylog.Check2(chg.Perform(s.as))
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
	defer s.as.MockUnrestrictedPaths("/")() // Treat test path as unrestricted.
	s.sys.InsertOsLstatResult(`lstat "/source"`, testutil.FileInfoFile)
	s.sys.InsertFault(`lstat "/target"`, syscall.ENOENT)
	s.sys.InsertFstatResult(`fstat 4 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFstatResult(`fstat 5 <ptr>`, syscall.Stat_t{})
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "/source", Dir: "/target", Options: []string{"bind", "x-snapd.kind=file"}}}
	synth := mylog.Check2(chg.Perform(s.as))

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
	defer s.as.MockUnrestrictedPaths("/")() // Treat test path as unrestricted.
	s.sys.InsertFault(`lstat "/target"`, syscall.ENOENT)
	s.sys.InsertFault(`openat 3 "target" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`, errTesting)
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "/source", Dir: "/target", Options: []string{"bind", "x-snapd.kind=file"}}}
	synth := mylog.Check2(chg.Perform(s.as))
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
	defer s.as.MockUnrestrictedPaths("/")() // Treat test path as unrestricted.
	s.sys.InsertFault(`lstat "/source"`, syscall.ENOENT)
	s.sys.InsertOsLstatResult(`lstat "/target"`, testutil.FileInfoFile)
	s.sys.InsertFstatResult(`fstat 4 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFstatResult(`fstat 5 <ptr>`, syscall.Stat_t{})
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "/source", Dir: "/target", Options: []string{"bind", "x-snapd.kind=file"}}}
	synth := mylog.Check2(chg.Perform(s.as))

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
	defer s.as.MockUnrestrictedPaths("/")() // Treat test path as unrestricted.
	s.sys.InsertFault(`lstat "/source"`, syscall.ENOENT)
	s.sys.InsertFault(`openat 3 "source" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`, errTesting)
	s.sys.InsertOsLstatResult(`lstat "/target"`, testutil.FileInfoFile)
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "/source", Dir: "/target", Options: []string{"bind", "x-snapd.kind=file"}}}
	synth := mylog.Check2(chg.Perform(s.as))
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
	defer s.as.MockUnrestrictedPaths("/")() // Treat test path as unrestricted.
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
	s.sys.InsertFstatResult(`fstat 6 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFstatfsResult(`fstatfs 6 <ptr>`, syscall.Statfs_t{})

	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "/source", Dir: "/rofs/target", Options: []string{"bind", "x-snapd.kind=file"}}}
	synth := mylog.Check2(chg.Perform(s.as))

	c.Assert(synth, DeepEquals, []*update.Change{
		{
			Action: update.Mount, Entry: osutil.MountEntry{
				Name: "tmpfs", Dir: "/rofs", Type: "tmpfs",
				Options: []string{"x-snapd.synthetic", "x-snapd.needed-by=/rofs/target", "mode=0755", "uid=0", "gid=0"},
			},
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
		{C: `readdir "/rofs"`, R: []fs.DirEntry(nil)},
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
		{C: `mount "none" "/tmp/.snap/rofs" "" MS_REC|MS_PRIVATE ""`},
		{C: `unmount "/tmp/.snap/rofs" UMOUNT_NOFOLLOW|MNT_DETACH`},

		// Perform clean up after the unmount operation.
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "tmp" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 4},
		{C: `openat 4 ".snap" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 5},
		{C: `openat 5 "rofs" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 6},
		{C: `fstat 6 <ptr>`, R: syscall.Stat_t{}},
		{C: `close 5`},
		{C: `close 4`},
		{C: `close 3`},
		{C: `fstatfs 6 <ptr>`, R: syscall.Statfs_t{}},
		{C: `remove "/tmp/.snap/rofs"`},
		{C: `close 6`},

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
	synth := mylog.Check2(chg.Perform(s.as))
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
	synth := mylog.Check2(chg.Perform(s.as))
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
	synth := mylog.Check2(chg.Perform(s.as))
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
	synth := mylog.Check2(chg.Perform(s.as))
	c.Assert(err, ErrorMatches, `cannot use "/source" as bind-mount source: not a regular file`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/target"`, R: testutil.FileInfoFile},
		{C: `lstat "/source"`, R: testutil.FileInfoDir},
	})
}

// Change.Perform wants to unmount a file bind mount made on empty squashfs placeholder.
func (s *changeSuite) TestPerformFileBindUnmountOnSquashfs(c *C) {
	s.sys.InsertFstatfsResult(`fstatfs 4 <ptr>`, syscall.Statfs_t{Type: update.SquashfsMagic})
	s.sys.InsertFstatResult(`fstat 4 <ptr>`, syscall.Stat_t{Size: 0})
	chg := &update.Change{Action: update.Unmount, Entry: osutil.MountEntry{Name: "/source", Dir: "/target", Options: []string{"bind", "x-snapd.kind=file"}}}
	synth := mylog.Check2(chg.Perform(s.as))

	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `unmount "/target" UMOUNT_NOFOLLOW`},
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "target" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 4},
		{C: `fstat 4 <ptr>`, R: syscall.Stat_t{}},
		{C: `close 3`},
		{C: `fstatfs 4 <ptr>`, R: syscall.Statfs_t{Type: update.SquashfsMagic}},
		{C: `close 4`},
	})
	c.Assert(synth, HasLen, 0)
}

// Change.Perform wants to unmount a file bind mount made on non-empty ext4 placeholder.
func (s *changeSuite) TestPerformFileBindUnmountOnExt4NonEmpty(c *C) {
	s.sys.InsertFstatfsResult(`fstatfs 4 <ptr>`, syscall.Statfs_t{Type: update.Ext4Magic})
	s.sys.InsertFstatResult(`fstat 4 <ptr>`, syscall.Stat_t{Size: 1})
	chg := &update.Change{Action: update.Unmount, Entry: osutil.MountEntry{Name: "/source", Dir: "/target", Options: []string{"bind", "x-snapd.kind=file"}}}
	synth := mylog.Check2(chg.Perform(s.as))

	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `unmount "/target" UMOUNT_NOFOLLOW`},
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "target" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 4},
		{C: `fstat 4 <ptr>`, R: syscall.Stat_t{Size: 1}},
		{C: `close 3`},
		{C: `fstatfs 4 <ptr>`, R: syscall.Statfs_t{Type: update.Ext4Magic}},
		{C: `fstat 4 <ptr>`, R: syscall.Stat_t{Size: 1}},
		{C: `close 4`},
	})
	c.Assert(synth, HasLen, 0)
}

// Change.Perform wants to unmount a file bind mount made on empty tmpfs placeholder.
func (s *changeSuite) TestPerformFileBindUnmountOnTmpfsEmpty(c *C) {
	s.sys.InsertFstatfsResult(`fstatfs 4 <ptr>`, syscall.Statfs_t{Type: update.TmpfsMagic})
	s.sys.InsertFstatResult(`fstat 4 <ptr>`, syscall.Stat_t{Size: 0})
	chg := &update.Change{Action: update.Unmount, Entry: osutil.MountEntry{Name: "/source", Dir: "/target", Options: []string{"bind", "x-snapd.kind=file"}}}
	synth := mylog.Check2(chg.Perform(s.as))

	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `unmount "/target" UMOUNT_NOFOLLOW`},
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "target" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 4},
		{C: `fstat 4 <ptr>`, R: syscall.Stat_t{Size: 0}},
		{C: `close 3`},
		{C: `fstatfs 4 <ptr>`, R: syscall.Statfs_t{Type: update.TmpfsMagic}},
		{C: `fstat 4 <ptr>`, R: syscall.Stat_t{Size: 0}},
		{C: `remove "/target"`},
		{C: `close 4`},
	})
	c.Assert(synth, HasLen, 0)
}

// Change.Perform wants to unmount a file bind mount made on empty tmpfs placeholder but it is busy!.
func (s *changeSuite) TestPerformFileBindUnmountOnTmpfsEmptyButBusy(c *C) {
	restore := osutil.MockMountInfo("")
	defer restore()
	s.sys.InsertFstatfsResult(`fstatfs 4 <ptr>`, syscall.Statfs_t{Type: update.TmpfsMagic})
	s.sys.InsertFstatResult(`fstat 4 <ptr>`, syscall.Stat_t{Size: 0})
	s.sys.InsertFault(`remove "/target"`, syscall.EBUSY)
	chg := &update.Change{Action: update.Unmount, Entry: osutil.MountEntry{Name: "/source", Dir: "/target", Options: []string{"bind", "x-snapd.kind=file"}}}
	synth := mylog.Check2(chg.Perform(s.as))

	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `unmount "/target" UMOUNT_NOFOLLOW`},
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "target" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 4},
		{C: `fstat 4 <ptr>`, R: syscall.Stat_t{Size: 0}},
		{C: `close 3`},
		{C: `fstatfs 4 <ptr>`, R: syscall.Statfs_t{Type: update.TmpfsMagic}},
		{C: `fstat 4 <ptr>`, R: syscall.Stat_t{Size: 0}},
		{C: `remove "/target"`, E: syscall.EBUSY},
		{C: `close 4`},
	})
	c.Assert(synth, HasLen, 0)
}

// Change.Perform wants to unmount a file bind mount but it fails.
func (s *changeSuite) TestPerformFileBindUnmountError(c *C) {
	s.sys.InsertFault(`unmount "/target" UMOUNT_NOFOLLOW`, errTesting)
	chg := &update.Change{Action: update.Unmount, Entry: osutil.MountEntry{Name: "/source", Dir: "/target", Options: []string{"bind", "x-snapd.kind=file"}}}
	synth := mylog.Check2(chg.Perform(s.as))
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
	synth := mylog.Check2(chg.Perform(s.as))
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
	synth := mylog.Check2(chg.Perform(s.as))
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
	synth := mylog.Check2(chg.Perform(s.as))
	c.Assert(err, ErrorMatches, `cannot inspect "/name": testing`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/name"`, E: errTesting},
	})
}

// Change.Perform wants to create a symlink.
func (s *changeSuite) TestPerformCreateSymlink(c *C) {
	defer s.as.MockUnrestrictedPaths("/")() // Treat test path as unrestricted.
	s.sys.InsertFault(`lstat "/name"`, syscall.ENOENT)
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "unused", Dir: "/name", Options: []string{"x-snapd.kind=symlink", "x-snapd.symlink=/oldname"}}}
	synth := mylog.Check2(chg.Perform(s.as))

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
	defer s.as.MockUnrestrictedPaths("/")() // Treat test path as unrestricted.
	s.sys.InsertFault(`lstat "/name"`, syscall.ENOENT)
	s.sys.InsertFault(`symlinkat "/oldname" 3 "name"`, errTesting)
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "unused", Dir: "/name", Options: []string{"x-snapd.kind=symlink", "x-snapd.symlink=/oldname"}}}
	synth := mylog.Check2(chg.Perform(s.as))
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
	synth := mylog.Check2(chg.Perform(s.as))
	c.Assert(err, ErrorMatches, `cannot create symlink with empty target: "/name"`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/name"`, E: syscall.ENOENT},
	})
}

// Change.Perform wants to create a symlink but the base directory isn't there.
func (s *changeSuite) TestPerformCreateSymlinkWithoutBaseDir(c *C) {
	defer s.as.MockUnrestrictedPaths("/")() // Treat test path as unrestricted.
	s.sys.InsertFault(`lstat "/base/name"`, syscall.ENOENT)
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "unused", Dir: "/base/name", Options: []string{"x-snapd.kind=symlink", "x-snapd.symlink=/oldname"}}}
	synth := mylog.Check2(chg.Perform(s.as))

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
	defer s.as.MockUnrestrictedPaths("/")() // Treat test path as unrestricted.
	s.sys.InsertFault(`lstat "/base/name"`, syscall.ENOENT)
	s.sys.InsertFault(`mkdirat 3 "base" 0755`, errTesting)
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "unused", Dir: "/base/name", Options: []string{"x-snapd.kind=symlink", "x-snapd.symlink=/oldname"}}}
	synth := mylog.Check2(chg.Perform(s.as))
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
	defer s.as.MockUnrestrictedPaths("/")() // Treat test path as unrestricted.
	s.sys.InsertFault(`lstat "/rofs/name"`, syscall.ENOENT)
	s.sys.InsertFault(`mkdirat 3 "rofs" 0755`, syscall.EEXIST)
	s.sys.InsertFault(`symlinkat "/oldname" 4 "name"`, syscall.EROFS, nil) // works on 2nd try
	s.sys.InsertSysLstatResult(`lstat "/rofs" <ptr>`, syscall.Stat_t{Uid: 0, Gid: 0, Mode: 0755})
	s.sys.InsertReadDirResult(`readdir "/rofs"`, nil) // pretend /rofs is empty.
	s.sys.InsertFault(`lstat "/tmp/.snap/rofs"`, syscall.ENOENT)
	s.sys.InsertOsLstatResult(`lstat "/rofs"`, testutil.FileInfoDir)
	s.sys.InsertFstatResult(`fstat 4 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFstatResult(`fstat 7 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFstatResult(`fstat 6 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFstatfsResult(`fstatfs 6 <ptr>`, syscall.Statfs_t{})

	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "unused", Dir: "/rofs/name", Options: []string{"x-snapd.kind=symlink", "x-snapd.symlink=/oldname"}}}
	synth := mylog.Check2(chg.Perform(s.as))

	c.Assert(synth, DeepEquals, []*update.Change{
		{
			Action: update.Mount, Entry: osutil.MountEntry{
				Name: "tmpfs", Dir: "/rofs", Type: "tmpfs",
				Options: []string{"x-snapd.synthetic", "x-snapd.needed-by=/rofs/name", "mode=0755", "uid=0", "gid=0"},
			},
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
		{C: `readdir "/rofs"`, R: []fs.DirEntry(nil)},
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
		{C: `mount "none" "/tmp/.snap/rofs" "" MS_REC|MS_PRIVATE ""`},
		{C: `unmount "/tmp/.snap/rofs" UMOUNT_NOFOLLOW|MNT_DETACH`},

		// Perform clean up after the unmount operation.
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "tmp" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 4},
		{C: `openat 4 ".snap" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 5},
		{C: `openat 5 "rofs" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 6},
		{C: `fstat 6 <ptr>`, R: syscall.Stat_t{}},
		{C: `close 5`},
		{C: `close 4`},
		{C: `close 3`},
		{C: `fstatfs 6 <ptr>`, R: syscall.Statfs_t{}},
		{C: `remove "/tmp/.snap/rofs"`},
		{C: `close 6`},

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
	synth := mylog.Check2(chg.Perform(s.as))
	c.Assert(err, ErrorMatches, `cannot create symlink in "/name": existing file in the way`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `lstat "/name"`, R: testutil.FileInfoFile},
	})
}

// Change.Perform wants to create a symlink but a correct symlink is already present.
func (s *changeSuite) TestPerformCreateSymlinkWithGoodSymlinkPresent(c *C) {
	defer s.as.MockUnrestrictedPaths("/")() // Treat test path as unrestricted.
	s.sys.InsertOsLstatResult(`lstat "/name"`, testutil.FileInfoSymlink)
	s.sys.InsertFault(`symlinkat "/oldname" 3 "name"`, syscall.EEXIST)
	s.sys.InsertFstatResult(`fstat 4 <ptr>`, syscall.Stat_t{Mode: syscall.S_IFLNK})
	s.sys.InsertReadlinkatResult(`readlinkat 4 "" <ptr>`, "/oldname")
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "unused", Dir: "/name", Options: []string{"x-snapd.kind=symlink", "x-snapd.symlink=/oldname"}}}
	synth := mylog.Check2(chg.Perform(s.as))

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
	defer s.as.MockUnrestrictedPaths("/")() // Treat test path as unrestricted.
	s.sys.InsertOsLstatResult(`lstat "/name"`, testutil.FileInfoSymlink)
	s.sys.InsertFault(`symlinkat "/oldname" 3 "name"`, syscall.EEXIST)
	s.sys.InsertFstatResult(`fstat 4 <ptr>`, syscall.Stat_t{Mode: syscall.S_IFLNK})
	s.sys.InsertReadlinkatResult(`readlinkat 4 "" <ptr>`, "/evil")
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "unused", Dir: "/name", Options: []string{"x-snapd.kind=symlink", "x-snapd.symlink=/oldname"}}}
	synth := mylog.Check2(chg.Perform(s.as))
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
	synth := mylog.Check2(chg.Perform(s.as))

	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `remove "/name"`},
	})
}

// Change.Perform wants to create a symlink in /etc and the write is made private.
func (s *changeSuite) TestPerformCreateSymlinkWithAvoidedTrespassing(c *C) {
	defer s.as.MockUnrestrictedPaths("/tmp/")() // Allow writing to /tmp

	s.sys.InsertFault(`lstat "/etc/demo.conf"`, syscall.ENOENT)
	s.sys.InsertFstatfsResult(`fstatfs 3 <ptr>`, syscall.Statfs_t{Type: update.SquashfsMagic})
	s.sys.InsertFstatResult(`fstat 3 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFault(`mkdirat 3 "etc" 0755`, syscall.EEXIST)
	s.sys.InsertFstatfsResult(`fstatfs 4 <ptr>`,
		// On 1st call ext4, on 2nd call tmpfs
		syscall.Statfs_t{Type: update.Ext4Magic},
		syscall.Statfs_t{Type: update.TmpfsMagic})
	s.sys.InsertFstatResult(`fstat 4 <ptr>`, syscall.Stat_t{})
	s.sys.InsertSysLstatResult(`lstat "/etc" <ptr>`, syscall.Stat_t{Mode: 0755})
	otherConf := testutil.FakeDirEntry("other.conf", 0755)
	s.sys.InsertReadDirResult(`readdir "/etc"`, []fs.DirEntry{otherConf})
	s.sys.InsertFault(`lstat "/tmp/.snap/etc"`, syscall.ENOENT)
	s.sys.InsertFault(`lstat "/tmp/.snap/etc/other.conf"`, syscall.ENOENT)
	s.sys.InsertOsLstatResult(`lstat "/etc"`, testutil.FileInfoDir)
	otherConfInfo := mylog.Check2(otherConf.Info())

	s.sys.InsertOsLstatResult(`lstat "/etc/other.conf"`, otherConfInfo)
	s.sys.InsertFault(`mkdirat 3 "tmp" 0755`, syscall.EEXIST)
	s.sys.InsertFstatResult(`fstat 5 <ptr>`, syscall.Stat_t{Mode: syscall.S_IFREG})
	s.sys.InsertFstatResult(`fstat 4 <ptr>`, syscall.Stat_t{Mode: syscall.S_IFDIR})
	s.sys.InsertFstatResult(`fstat 7 <ptr>`, syscall.Stat_t{Mode: syscall.S_IFDIR})
	s.sys.InsertFstatResult(`fstat 6 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFstatfsResult(`fstatfs 6 <ptr>`, syscall.Statfs_t{})

	// This is the change we want to perform:
	// put a layout symlink at /etc/demo.conf -> /oldname
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "unused", Dir: "/etc/demo.conf", Options: []string{"x-snapd.kind=symlink", "x-snapd.symlink=/oldname"}}}
	synth := mylog.Check2(chg.Perform(s.as))
	c.Check(err, IsNil)
	c.Check(synth, HasLen, 2)
	// We have created some synthetic change (made /etc a new tmpfs and re-populate it)
	c.Assert(synth[0], DeepEquals, &update.Change{
		Entry:  osutil.MountEntry{Name: "tmpfs", Dir: "/etc", Type: "tmpfs", Options: []string{"x-snapd.synthetic", "x-snapd.needed-by=/etc/demo.conf", "mode=0755", "uid=0", "gid=0"}},
		Action: "mount",
	})
	c.Assert(synth[1], DeepEquals, &update.Change{
		Entry:  osutil.MountEntry{Name: "/etc/other.conf", Dir: "/etc/other.conf", Options: []string{"bind", "x-snapd.kind=file", "x-snapd.synthetic", "x-snapd.needed-by=/etc/demo.conf"}},
		Action: "mount",
	})

	// And this is exactly how we made that happen:
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		// Attempt to construct a symlink /etc/demo.conf -> /oldname.
		// This stops as soon as we notice that /etc is an ext4 filesystem.
		// To avoid writing to it directly we need a writable mimic.
		{C: `lstat "/etc/demo.conf"`, E: syscall.ENOENT},
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `fstatfs 3 <ptr>`, R: syscall.Statfs_t{Type: update.SquashfsMagic}},
		{C: `fstat 3 <ptr>`, R: syscall.Stat_t{}},
		{C: `mkdirat 3 "etc" 0755`, E: syscall.EEXIST},
		{C: `openat 3 "etc" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `close 3`},
		{C: `fstatfs 4 <ptr>`, R: syscall.Statfs_t{Type: update.Ext4Magic}},
		{C: `fstat 4 <ptr>`, R: syscall.Stat_t{Mode: 0x4000}},
		{C: `close 4`},

		// Create a writable mimic over /etc, scan the contents of /etc first.
		// For convenience we pretend that /etc is empty. The mimic
		// replicates /etc in /tmp/.snap/etc for subsequent re-construction.
		{C: `lstat "/etc" <ptr>`, R: syscall.Stat_t{Mode: 0755}},
		{C: `readdir "/etc"`, R: []fs.DirEntry{otherConf}},
		{C: `lstat "/tmp/.snap/etc"`, E: syscall.ENOENT},
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "tmp" 0755`, E: syscall.EEXIST},
		{C: `openat 3 "tmp" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `mkdirat 4 ".snap" 0755`},
		{C: `openat 4 ".snap" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 5},
		{C: `fchown 5 0 0`},
		{C: `close 4`},
		{C: `close 3`},
		{C: `mkdirat 5 "etc" 0755`},
		{C: `openat 5 "etc" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `fchown 3 0 0`},
		{C: `close 3`},
		{C: `close 5`},

		// Prepare a secure bind mount operation /etc -> /tmp/.snap/etc
		{C: `lstat "/etc"`, R: testutil.FileInfoDir},

		// Open an O_PATH descriptor to /etc. We need this as a source of a
		// secure bind mount operation. We also ensure that the descriptor
		// refers to a directory.
		// NOTE: we keep fd 4 open for subsequent use.
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "etc" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 4},
		{C: `fstat 4 <ptr>`, R: syscall.Stat_t{Mode: syscall.S_IFDIR}},
		{C: `close 3`},

		// Open an O_PATH descriptor to /tmp/.snap/etc. We need this as a
		// target of a secure bind mount operation. We also ensure that the
		// descriptor refers to a directory.
		// NOTE: we keep fd 7 open for subsequent use.
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "tmp" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 5},
		{C: `openat 5 ".snap" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 6},
		{C: `openat 6 "etc" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 7},
		{C: `fstat 7 <ptr>`, R: syscall.Stat_t{Mode: syscall.S_IFDIR}},
		{C: `close 6`},
		{C: `close 5`},
		{C: `close 3`},

		// Perform the secure bind mount operation /etc -> /tmp/.snap/etc
		// and release the two associated file descriptors.
		{C: `mount "/proc/self/fd/4" "/proc/self/fd/7" "" MS_BIND|MS_REC ""`},
		{C: `close 7`},
		{C: `close 4`},

		// Mount a tmpfs over /etc, re-constructing the original mode and
		// ownership. Bind mount each original file over and detach the copy
		// of /etc we had in /tmp/.snap/etc.

		{C: `lstat "/etc"`, R: testutil.FileInfoDir},
		{C: `mount "tmpfs" "/etc" "tmpfs" 0 "mode=0755,uid=0,gid=0"`},
		// Here we restore the contents of /etc: here it's just one file - other.conf
		{C: `lstat "/etc/other.conf"`, R: otherConfInfo},
		{C: `lstat "/tmp/.snap/etc/other.conf"`, E: syscall.ENOENT},

		// Create /tmp/.snap/etc/other.conf as an empty file.
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "tmp" 0755`, E: syscall.EEXIST},
		{C: `openat 3 "tmp" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `mkdirat 4 ".snap" 0755`},
		{C: `openat 4 ".snap" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 5},
		{C: `fchown 5 0 0`},
		{C: `mkdirat 5 "etc" 0755`},
		{C: `openat 5 "etc" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 6},
		{C: `fchown 6 0 0`},
		{C: `close 5`},
		{C: `close 4`},
		{C: `close 3`},
		// NOTE: This is without O_DIRECTORY and with O_CREAT|O_EXCL,
		// we are creating an empty file for the subsequent bind mount.
		{C: `openat 6 "other.conf" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`, R: 3},
		{C: `fchown 3 0 0`},
		{C: `close 3`},
		{C: `close 6`},

		// Open O_PATH to /tmp/.snap/etc/other.conf
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "tmp" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 4},
		{C: `openat 4 ".snap" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 5},
		{C: `openat 5 "etc" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 6},
		{C: `openat 6 "other.conf" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 7},
		{C: `fstat 7 <ptr>`, R: syscall.Stat_t{Mode: syscall.S_IFDIR}},
		{C: `close 6`},
		{C: `close 5`},
		{C: `close 4`},
		{C: `close 3`},

		// Open O_PATH to /etc/other.conf
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "etc" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 4},
		{C: `openat 4 "other.conf" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 5},
		{C: `fstat 5 <ptr>`, R: syscall.Stat_t{Mode: syscall.S_IFREG}},
		{C: `close 4`},
		{C: `close 3`},

		// Restore the /etc/other.conf file with a secure bind mount.
		{C: `mount "/proc/self/fd/7" "/proc/self/fd/5" "" MS_BIND ""`},
		{C: `close 5`},
		{C: `close 7`},

		// We're done restoring now.
		{C: `mount "none" "/tmp/.snap/etc" "" MS_REC|MS_PRIVATE ""`},
		{C: `unmount "/tmp/.snap/etc" UMOUNT_NOFOLLOW|MNT_DETACH`},

		// Perform clean up after the unmount operation.
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "tmp" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 4},
		{C: `openat 4 ".snap" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 5},
		{C: `openat 5 "etc" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 6},
		{C: `fstat 6 <ptr>`, R: syscall.Stat_t{}},
		{C: `close 5`},
		{C: `close 4`},
		{C: `close 3`},
		{C: `fstatfs 6 <ptr>`, R: syscall.Statfs_t{}},
		{C: `remove "/tmp/.snap/etc"`},
		{C: `close 6`},

		// The mimic is now complete and subsequent writes to /etc are private
		// to the mount namespace of the process.

		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `fstatfs 3 <ptr>`, R: syscall.Statfs_t{Type: update.SquashfsMagic}},
		{C: `fstat 3 <ptr>`, R: syscall.Stat_t{}},
		{C: `mkdirat 3 "etc" 0755`, E: syscall.EEXIST},
		{C: `openat 3 "etc" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `close 3`},
		{C: `fstatfs 4 <ptr>`, R: syscall.Statfs_t{Type: update.TmpfsMagic}},
		{C: `fstat 4 <ptr>`, R: syscall.Stat_t{Mode: 0x4000}},
		{C: `symlinkat "/oldname" 4 "demo.conf"`},
		{C: `close 4`},
	})
}

// Change.Perform wants to remove a directory which is a bind mount of ext4 from onto squashfs.
func (s *changeSuite) TestPerformRmdirOnExt4OnSquashfs(c *C) {
	defer s.as.MockUnrestrictedPaths("/tmp/")() // Allow writing to /tmp

	s.sys.InsertFstatResult(`fstat 4 <ptr>`, syscall.Stat_t{})
	// Pretend that /root is an ext4 bind mount from somewhere.
	s.sys.InsertFstatfsResult(`fstatfs 4 <ptr>`, syscall.Statfs_t{Type: update.Ext4Magic})
	// Pretend that removing /root returns EROFS (it really can!).
	s.sys.InsertFault(`remove "/root"`, syscall.EROFS)

	// This is the change we want to perform:
	// - unmount a layout from /root
	chg := &update.Change{Action: update.Unmount, Entry: osutil.MountEntry{Name: "unused", Dir: "/root", Options: []string{"x-snapd.origin=layout"}}}
	synth := mylog.Check2(chg.Perform(s.as))
	// The change succeeded even though we were unable to remove the /root
	// directory because it is backed by a squashfs, which is not modelled by
	// this test but is modelled by the integration test.
	c.Check(err, IsNil)
	c.Check(synth, HasLen, 0)

	// And this is exactly how we made that happen:
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `unmount "/root" UMOUNT_NOFOLLOW`},
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "root" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 4},
		{C: `fstat 4 <ptr>`, R: syscall.Stat_t{}},
		{C: `close 3`},
		{C: `fstatfs 4 <ptr>`, R: syscall.Statfs_t{Type: update.Ext4Magic}},
		{C: `remove "/root"`, E: syscall.EROFS},
		{C: `close 4`},
	})
}

// ########################
// Topic: ensuring dirs
// ########################

// Change.Perform wants to ensure a directory but name cannot be stat'ed
func (s *changeSuite) TestPerformEnsureDirNameLstatError(c *C) {
	s.sys.InsertFault(`lstat "/home/user/.local/share/missing"`, errTesting)
	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{
		Name:    "unused",
		Dir:     "/home/user/.local/share/missing",
		Options: []string{"x-snapd.kind=ensure-dir", "x-snapd.must-exist-dir=/home/user"},
	}}
	synth := mylog.Check2(chg.Perform(s.as))
	c.Assert(err, ErrorMatches, `cannot inspect "/home/user/.local/share/missing": testing`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		// Change.ensureTarget osLstat fails
		{C: `lstat "/home/user/.local/share/missing"`, E: errTesting},
	})
}

// Change.Perform wants to ensure a directory but there's a file in the way of the target
func (s *changeSuite) TestPerformEnsureDirFileInTheWayOfTarget(c *C) {
	s.sys.InsertOsLstatResult(`lstat "/home/user/.local/share/missing"`, testutil.FileInfoFile)

	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{
		Name:    "unused",
		Dir:     "/home/user/.local/share/missing",
		Options: []string{"x-snapd.kind=ensure-dir", "x-snapd.must-exist-dir=/home/user"},
	}}
	synth := mylog.Check2(chg.Perform(s.as))
	c.Assert(err, ErrorMatches, `cannot create ensure-dir target "/home/user/.local/share/missing": existing file in the way`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		// Change.ensureTarget osLstat succeeds and file is not a directory
		{C: `lstat "/home/user/.local/share/missing"`, R: testutil.FileInfoFile},
	})
}

// Change.Perform wants to ensure a directory with must-exist-dir missing
func (s *changeSuite) TestPerformEnsureDirMustExistDirMissing(c *C) {
	s.sys.InsertFault(`lstat "/home/user/missing"`, syscall.ENOENT)
	s.sys.InsertFault(`lstat "/home/user"`, syscall.ENOENT)

	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{
		Name:    "unused",
		Dir:     "/home/user/missing",
		Options: []string{"x-snapd.kind=ensure-dir", "x-snapd.must-exist-dir=/home/user"},
	}}

	synth := mylog.Check2(chg.Perform(s.as))
	c.Assert(err, ErrorMatches, `parent directory "/home/user" does not exist`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		// Change.ensureTarget osLstat succeeds and file does not exist, resulting in call to Change.createPath
		{C: `lstat "/home/user/missing"`, E: syscall.ENOENT},
		// Change.createPath case "ensure-dir" with uid != 0 resulting in call to utils.MkdirAllWithin
		// utils.MkdirAllWithin checks if "/home/user/missing" is missing
		{C: `lstat "/home/user/missing"`, E: syscall.ENOENT},
		// utils.MkdirAllWithin checks if must-exist-dir "/home/user" exists
		{C: `lstat "/home/user"`, E: syscall.ENOENT},
	})
}

// Change.Perform wants to ensure a directory but there's a file in the way of a parent
func (s *changeSuite) TestPerformEnsureDirFileInTheWayOfParent(c *C) {
	s.sys.InsertFault(`lstat "/home/user/missing"`, syscall.ENOENT)
	s.sys.InsertOsLstatResult(`lstat "/home/user"`, testutil.FileInfoFile)

	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{
		Name:    "unused",
		Dir:     "/home/user/missing",
		Options: []string{"x-snapd.kind=ensure-dir", "x-snapd.must-exist-dir=/home/user"},
	}}
	synth := mylog.Check2(chg.Perform(s.as))
	c.Assert(err, ErrorMatches, `cannot use parent path "/home/user": not a directory`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		// Change.ensureTarget osLstat succeeds and file does not exist, resulting in call to Change.createPath
		{C: `lstat "/home/user/missing"`, E: syscall.ENOENT},
		// Change.createPath case "ensure-dir" with uid != 0 resulting in call to utils.MkdirAllWithin
		// utils.MkdirAllWithin checks if "/home/user/missing" is missing
		{C: `lstat "/home/user/missing"`, E: syscall.ENOENT},
		// utils.MkdirAllWithin checks if must-exist-dir "/home/user" exists
		{C: `lstat "/home/user"`, R: testutil.FileInfoFile},
	})
}

// Change.Perform wants to ensure a directory but fails with an error
func (s *changeSuite) TestPerformEnsureDirError(c *C) {
	s.sys.InsertFault(`lstat "/home/user/missing"`, syscall.ENOENT)
	s.sys.InsertFault(`lstat "/home/user"`, errTesting)

	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{
		Name:    "unused",
		Dir:     "/home/user/missing",
		Options: []string{"x-snapd.kind=ensure-dir", "x-snapd.must-exist-dir=/home/user"},
	}}
	synth := mylog.Check2(chg.Perform(s.as))
	c.Assert(err, ErrorMatches, `cannot inspect parent path "/home/user": testing`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		// Change.ensureTarget osLstat succeeds and file does not exist, resulting in call to Change.createPath
		{C: `lstat "/home/user/missing"`, E: syscall.ENOENT},
		// Change.createPath case "ensure-dir" with uid != 0 resulting in call to utils.MkdirAllWithin
		// utils.MkdirAllWithin checks if "/home/user/missing" is missing
		{C: `lstat "/home/user/missing"`, E: syscall.ENOENT},
		// utils.MkdirAllWithin checks if must-exist-dir "/home/user" exists
		{C: `lstat "/home/user"`, E: errTesting},
	})
}

// Change.Perform wants to ensure a directory (scenario 1)
// Scenario: MustExistDir /home/user exists, but child directories .local, .local/share and .local/share/missing does not
func (s *changeSuite) TestPerformEnsureDirScenario1(c *C) {
	// Allow writing to /home/user
	defer s.as.MockUnrestrictedPaths("/home/user")()

	s.sys.InsertFault(`lstat "/home/user/.local/share/missing"`, syscall.ENOENT)
	s.sys.InsertFault(`lstat "/home/user/.local"`, syscall.ENOENT)
	s.sys.InsertOsLstatResult(`lstat "/home/user"`, testutil.FileInfoDir)

	restoreGetuid := update.MockGetuid(func() sys.UserID {
		return 1000
	})
	defer restoreGetuid()

	restoreGetgid := update.MockGetgid(func() sys.GroupID {
		return 1000
	})
	defer restoreGetgid()

	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{
		Name:    "unused",
		Dir:     "/home/user/.local/share/missing",
		Options: []string{"x-snapd.kind=ensure-dir", "x-snapd.must-exist-dir=/home/user"},
	}}
	synth := mylog.Check2(chg.Perform(s.as))

	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		// Change.ensureTarget osLstat succeeds and file does not exist, resulting in call to Change.createPath
		{C: `lstat "/home/user/.local/share/missing"`, E: syscall.ENOENT},
		// Change.createPath case "ensure-dir" with uid != 0 resulting in call to utils.MkdirAllWithin
		// utils.MkdirAllWithin checks if "/home/user/.local/share/missing" is missing
		{C: `lstat "/home/user/.local/share/missing"`, E: syscall.ENOENT},
		// utils.MkdirAllWithin checks if must-exist-dir "/home/user" exists
		{C: `lstat "/home/user"`, R: testutil.FileInfoDir, E: nil},
		// utils.MkdirAllWithin interates to find the first missing directory, in this case "/home/user/.local"
		{C: `lstat "/home/user/.local"`, E: syscall.ENOENT},
		// utils.MkdirAllWithin opens must-exist-dir "/home/user" and calls utils.Mkdir
		{C: `open "/home/user" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		// utils.Mkdir creates missing directory ".local"
		{C: `mkdirat 3 ".local" 0700`},
		{C: `openat 3 ".local" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `fchown 4 1000 1000`},
		// utils.MkdirAllWithin iterates through the remaining missing dirs "share/missing"
		// and calls utils.Mkdir to create them
		{C: `mkdirat 4 "share" 0700`},
		{C: `openat 4 "share" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 5},
		{C: `fchown 5 1000 1000`},
		{C: `mkdirat 5 "missing" 0700`},
		{C: `openat 5 "missing" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 6},
		{C: `fchown 6 1000 1000`},
		// Closing of file descriptors in reverse order
		{C: `close 6`},
		{C: `close 5`},
		{C: `close 4`},
		{C: `close 3`},
	})
}

// Change.Perform wants to ensure a directory (scenario 2)
// Scenario: MustExistDir /home/user and child directories .local and .local/share exists, but .local/share/missing does not
func (s *changeSuite) TestPerformEnsureDirScenario2(c *C) {
	// Allow writing to /home/user
	defer s.as.MockUnrestrictedPaths("/home/user")()

	s.sys.InsertFault(`lstat "/home/user/.local/share/missing"`, syscall.ENOENT)
	s.sys.InsertOsLstatResult(`lstat "/home/user/.local/share"`, testutil.FileInfoDir)
	s.sys.InsertOsLstatResult(`lstat "/home/user/.local"`, testutil.FileInfoDir)
	s.sys.InsertOsLstatResult(`lstat "/home/user"`, testutil.FileInfoDir)

	restoreGetuid := update.MockGetuid(func() sys.UserID {
		return 1000
	})
	defer restoreGetuid()

	restoreGetgid := update.MockGetgid(func() sys.GroupID {
		return 1000
	})
	defer restoreGetgid()

	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{
		Name:    "unused",
		Dir:     "/home/user/.local/share/missing",
		Options: []string{"x-snapd.kind=ensure-dir", "x-snapd.must-exist-dir=/home/user"},
	}}
	synth := mylog.Check2(chg.Perform(s.as))

	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		// Change.ensureTarget osLstat succeeds and file does not exist, resulting in call to Change.createPath
		{C: `lstat "/home/user/.local/share/missing"`, E: syscall.ENOENT},
		// Change.createPath case "ensure-dir" with uid != 0 resulting in call to utils.MkdirAllWithin
		// utils.MkdirAllWithin checks if "/home/user/.local/share/missing" is missing
		{C: `lstat "/home/user/.local/share/missing"`, E: syscall.ENOENT},
		// utils.MkdirAllWithin checks if must-exist-dir "/home/user" exists
		{C: `lstat "/home/user"`, R: testutil.FileInfoDir, E: nil},
		// utils.MkdirAllWithin interates to find the first missing directory, but does not check target
		// dir "/home/user/.local/share/missing", because at this point it is already confirmed missing
		{C: `lstat "/home/user/.local"`, R: testutil.FileInfoDir},
		{C: `lstat "/home/user/.local/share"`, R: testutil.FileInfoDir},
		// utils.MkdirAllWithin opens "/home/user/.local/share" and calls utils.Mkdir
		{C: `open "/home/user/.local/share" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		// utils.Mkdir creates missing directory "missing"
		{C: `mkdirat 3 "missing" 0700`},
		{C: `openat 3 "missing" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `fchown 4 1000 1000`},
		// Closing of file descriptors in reverse order
		{C: `close 4`},
		{C: `close 3`},
	})
}

// ###########
// Topic: misc
// ###########

// Change.Perform handles unknown actions.
func (s *changeSuite) TestPerformUnknownAction(c *C) {
	chg := &update.Change{Action: update.Action("42")}
	synth := mylog.Check2(chg.Perform(s.as))
	c.Assert(err, ErrorMatches, `cannot process mount change: unknown action: .*`)
	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), HasLen, 0)
}

// Change.Perform wants to keep a mount entry unchanged.
func (s *changeSuite) TestPerformKeep(c *C) {
	chg := &update.Change{Action: update.Keep}
	synth := mylog.Check2(chg.Perform(s.as))

	c.Assert(synth, HasLen, 0)
	c.Assert(s.sys.RCalls(), HasLen, 0)
}

// ############################################
// Topic: change history tracked in Assumptions
// ############################################

func (s *changeSuite) TestPerformedChangesAreTracked(c *C) {
	s.sys.InsertOsLstatResult(`lstat "/target"`, testutil.FileInfoDir)
	c.Assert(s.as.PastChanges(), HasLen, 0)

	chg := &update.Change{Action: update.Mount, Entry: osutil.MountEntry{Name: "device", Dir: "/target", Type: "type"}}
	_ := mylog.Check2(chg.Perform(s.as))

	c.Assert(s.as.PastChanges(), DeepEquals, []*update.Change{
		{Action: update.Mount, Entry: osutil.MountEntry{Name: "device", Dir: "/target", Type: "type"}},
	})

	s.sys.InsertFstatResult(`fstat 4 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFstatfsResult(`fstatfs 4 <ptr>`, syscall.Statfs_t{})
	chg = &update.Change{Action: update.Unmount, Entry: osutil.MountEntry{Name: "device", Dir: "/target", Type: "type"}}
	_ = mylog.Check2(chg.Perform(s.as))


	chg = &update.Change{Action: update.Keep, Entry: osutil.MountEntry{Name: "tmpfs", Dir: "/target", Type: "tmpfs"}}
	_ = mylog.Check2(chg.Perform(s.as))

	c.Assert(s.as.PastChanges(), DeepEquals, []*update.Change{
		// past changes stack in order.
		{Action: update.Mount, Entry: osutil.MountEntry{Name: "device", Dir: "/target", Type: "type"}},
		{Action: update.Unmount, Entry: osutil.MountEntry{Name: "device", Dir: "/target", Type: "type"}},
		{Action: update.Keep, Entry: osutil.MountEntry{Name: "tmpfs", Dir: "/target", Type: "tmpfs"}},
	})
}

func (s *changeSuite) TestComplexPropagatingChanges(c *C) {
	// This problem is more subtle. It is a variant of the regression test
	// implemented in tests/regression/lp-1831010. Here, we have four directories:
	//
	// - $SNAP/a
	// - $SNAP/b
	// - $SNAP/b/c
	// - $SNAP/d
	//
	// but snapd's mount profile contains only two entries:
	//
	// 1) recursive-bind $SNAP/a -> $SNAP/b/c  (ie, mount --rbind $SNAP/a $SNAP/b/c)
	// 2) recursive-bind $SNAP/b -> $SNAP/d    (ie, mount --rbind $SNAP/b $SNAP/d)
	//
	// Both mount operations are performed under a substrate that is MS_SHARED.
	// Therefore, due to the rules that decide upon propagation of bind mounts
	// the propagation of the new mount entries is also shared. This is
	// documented in section 5b of
	// https://www.kernel.org/doc/Documentation/filesystems/sharedsubtree.txt.
	//
	// Interactive experimentation shows that the following three mount points exist
	// after this operation, as illustrated by findmnt:
	//
	// TARGET                                SOURCE         FSTYPE      OPTIONS
	// ...
	// └─/snap/test-snapd-layout/x1          /dev/loop1     squashfs    ro,nodev,relatime
	//   ├─/snap/test-snapd-layout/x1/b/c    /dev/loop1[/a] squashfs    ro,nodev,relatime
	//   └─/snap/test-snapd-layout/x1/d      /dev/loop1[/b] squashfs    ro,nodev,relatime
	//     └─/snap/test-snapd-layout/x1/d/c  /dev/loop1[/a] squashfs    ro,nodev,relatime
	//
	// Note that after the first mount operation only one mount point is created, namely
	// $SNAP/a -> $SNAP/b/c. The second recursive bind mount not only creates
	// $SNAP/b -> $SNAP/d, but also replicates $SNAP/a -> $SNAP/b/c as
	// $SNAP/a -> $SNAP/d/c.
	//
	// The test will simulate a refresh of the snap from revision x1 to revision
	// x2. When this happens the mount profile associated with x1 must be undone
	// and the mount profile associated with x2 must be constructed. Because
	// ordering matters, let's first consider the order of construction of x1
	// itself. Starting from nothing, apply x1 as follows:
	x1 := &osutil.MountProfile{
		Entries: []osutil.MountEntry{
			{Name: "/snap/app/x1/a", Dir: "/snap/app/x1/b/c", Type: "none", Options: []string{"rbind", "rw", "x-snapd.origin=layout"}},
			{Name: "/snap/app/x1/b", Dir: "/snap/app/x1/d", Type: "none", Options: []string{"rbind", "rw", "x-snapd.origin=layout"}},
		},
	}
	changes := update.NeededChanges(&osutil.MountProfile{}, x1)
	c.Assert(changes, DeepEquals, []*update.Change{
		{Action: update.Mount, Entry: osutil.MountEntry{Name: "/snap/app/x1/a", Dir: "/snap/app/x1/b/c", Type: "none", Options: []string{"rbind", "rw", "x-snapd.origin=layout"}}},
		{Action: update.Mount, Entry: osutil.MountEntry{Name: "/snap/app/x1/b", Dir: "/snap/app/x1/d", Type: "none", Options: []string{"rbind", "rw", "x-snapd.origin=layout"}}},
	})
	// We can see that x1 is constructed in alphabetical order, first recursively
	// bind mount at $SNAP/a the directory $SNAP/b/c, second recursively bind
	// mount at $SNAP/b the directory $SNAP/d.
	x2 := &osutil.MountProfile{
		Entries: []osutil.MountEntry{
			{Name: "/snap/app/x2/a", Dir: "/snap/app/x2/b/c", Type: "none", Options: []string{"rbind", "rw", "x-snapd.origin=layout"}},
			{Name: "/snap/app/x2/b", Dir: "/snap/app/x2/d", Type: "none", Options: []string{"rbind", "rw", "x-snapd.origin=layout"}},
		},
	}
	// When we are asked to refresh to revision x2, using the same layout, we
	// simply undo x1 and then create x2, which apart from the difference in
	// revision name, is exactly the same. The undo code, however, does not take
	// the replicated mount point under consideration and therefore attempts to
	// detach "x1/d", which normally fails with EBUSY. To counter this, the
	// unmount operation first switches the mount point to recursive private
	// propagation, before actually unmounting it. This ensures that propagation
	// doesn't self-conflict, simply because there isn't any left.
	changes = update.NeededChanges(x1, x2)
	c.Assert(changes, DeepEquals, []*update.Change{
		{Action: update.Unmount, Entry: osutil.MountEntry{Name: "/snap/app/x1/b", Dir: "/snap/app/x1/d", Type: "none", Options: []string{"rbind", "rw", "x-snapd.origin=layout", "x-snapd.detach"}}},
		{Action: update.Unmount, Entry: osutil.MountEntry{Name: "/snap/app/x1/a", Dir: "/snap/app/x1/b/c", Type: "none", Options: []string{"rbind", "rw", "x-snapd.origin=layout", "x-snapd.detach"}}},
		{Action: update.Mount, Entry: osutil.MountEntry{Name: "/snap/app/x2/a", Dir: "/snap/app/x2/b/c", Type: "none", Options: []string{"rbind", "rw", "x-snapd.origin=layout"}}},
		{Action: update.Mount, Entry: osutil.MountEntry{Name: "/snap/app/x2/b", Dir: "/snap/app/x2/d", Type: "none", Options: []string{"rbind", "rw", "x-snapd.origin=layout"}}},
	})
}

func (s *changeSuite) TestUnmountFailsWithEINVALAndUnmounted(c *C) {
	// We wanted to unmount /target, which failed with EINVAL.
	// Because /target is no longer mounted, we consume the error and carry on.
	restore := osutil.MockMountInfo("")
	defer restore()
	s.sys.InsertFault(`unmount "/target" UMOUNT_NOFOLLOW`, syscall.EINVAL)
	s.sys.InsertFstatResult(`fstat 4 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFstatfsResult(`fstatfs 4 <ptr>`, syscall.Statfs_t{})
	chg := &update.Change{Action: update.Unmount, Entry: osutil.MountEntry{Name: "tmpfs", Dir: "/target", Type: "tmpfs"}}
	_ := mylog.Check2(chg.Perform(s.as))

	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `unmount "/target" UMOUNT_NOFOLLOW`, E: syscall.EINVAL},
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "target" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 4},
		{C: `fstat 4 <ptr>`, R: syscall.Stat_t{}},
		{C: `close 3`},
		{C: `fstatfs 4 <ptr>`, R: syscall.Statfs_t{}},
		{C: `remove "/target"`},
		{C: `close 4`},
	})
}

func (s *changeSuite) TestUnmountFailsWithEINVALButStillMounted(c *C) {
	// We wanted to unmount /target, which failed with EINVAL.
	// Because /target is still mounted, we propagate the error.
	restore := osutil.MockMountInfo("132 28 0:82 / /target rw,relatime shared:74 - tmpfs tmpfs rw")
	defer restore()
	s.sys.InsertFault(`unmount "/target" UMOUNT_NOFOLLOW`, syscall.EINVAL)
	chg := &update.Change{Action: update.Unmount, Entry: osutil.MountEntry{Name: "tmpfs", Dir: "/target", Type: "tmpfs"}}
	_ := mylog.Check2(chg.Perform(s.as))
	c.Assert(err, Equals, syscall.EINVAL)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `unmount "/target" UMOUNT_NOFOLLOW`, E: syscall.EINVAL},
	})
}

// Change.Perform sets x-snapd.needed-by to mount entry ID.
func (s *changeSuite) TestSyntheticNeededByUsesMountEntryID(c *C) {
	defer s.as.MockUnrestrictedPaths("/")() // Treat test path as unrestricted.
	s.sys.InsertOsLstatResult(`lstat "/usr/share/target"`, testutil.FileInfoFile)
	s.sys.InsertFault(`lstat "/snap/some-snap/x1/rofs/dir/target"`, syscall.ENOENT)
	s.sys.InsertFault(`mkdirat 3 "snap" 0755`, syscall.EEXIST)
	s.sys.InsertFault(`mkdirat 4 "some-snap" 0755`, syscall.EEXIST)
	s.sys.InsertFault(`mkdirat 5 "x1" 0755`, syscall.EEXIST)
	s.sys.InsertFault(`mkdirat 6 "rofs" 0755`, syscall.EEXIST)
	s.sys.InsertFault(`mkdirat 7 "dir" 0755`, syscall.EROFS, nil)
	s.sys.InsertSysLstatResult(`lstat "/snap/some-snap/x1/rofs" <ptr>`, syscall.Stat_t{})
	s.sys.InsertReadDirResult(`readdir "/snap/some-snap/x1/rofs"`, []fs.DirEntry{})
	s.sys.InsertOsLstatResult(`lstat "/tmp/.snap/snap/some-snap/x1/rofs"`, testutil.FileInfoDir)
	s.sys.InsertOsLstatResult(`lstat "/snap/some-snap/x1/rofs"`, testutil.FileInfoDir)
	s.sys.InsertFstatResult(`fstat 7 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFstatResult(`fstat 10 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFstatResult(`fstat 9 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFstatfsResult(`fstatfs 9 <ptr>`, syscall.Statfs_t{})
	s.sys.InsertFstatResult(`fstat 6 <ptr>`, syscall.Stat_t{})

	// layout mount
	chg := &update.Change{
		Action: update.Mount,
		Entry: osutil.MountEntry{
			Name:    "/snap/some-snap/x1/rofs/dir/target",
			Dir:     "/usr/share/target",
			Options: []string{"rbind", "rw", "x-snapd.id=test-id", osutil.XSnapdKindFile(), osutil.XSnapdOriginLayout()},
		},
	}

	synth := mylog.Check2(chg.Perform(s.as))

	c.Check(synth, HasLen, 1)
	c.Check(synth[0].Entry.XSnapdNeededBy(), Equals, "test-id")
}

// Change.Perform sets x-snapd.needed-by to default mount entry ID (i.e. target directory).
func (s *changeSuite) TestSyntheticNeededByUsesDefaultMountEntryID(c *C) {
	defer s.as.MockUnrestrictedPaths("/")() // Treat test path as unrestricted.
	s.sys.InsertOsLstatResult(`lstat "/usr/share/target"`, testutil.FileInfoFile)
	s.sys.InsertFault(`lstat "/snap/some-snap/x1/rofs/dir/target"`, syscall.ENOENT)
	s.sys.InsertFault(`mkdirat 3 "snap" 0755`, syscall.EEXIST)
	s.sys.InsertFault(`mkdirat 4 "some-snap" 0755`, syscall.EEXIST)
	s.sys.InsertFault(`mkdirat 5 "x1" 0755`, syscall.EEXIST)
	s.sys.InsertFault(`mkdirat 6 "rofs" 0755`, syscall.EEXIST)
	s.sys.InsertFault(`mkdirat 7 "dir" 0755`, syscall.EROFS, nil)
	s.sys.InsertSysLstatResult(`lstat "/snap/some-snap/x1/rofs" <ptr>`, syscall.Stat_t{})
	s.sys.InsertReadDirResult(`readdir "/snap/some-snap/x1/rofs"`, []fs.DirEntry{})
	s.sys.InsertOsLstatResult(`lstat "/tmp/.snap/snap/some-snap/x1/rofs"`, testutil.FileInfoDir)
	s.sys.InsertOsLstatResult(`lstat "/snap/some-snap/x1/rofs"`, testutil.FileInfoDir)
	s.sys.InsertFstatResult(`fstat 7 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFstatResult(`fstat 10 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFstatResult(`fstat 9 <ptr>`, syscall.Stat_t{})
	s.sys.InsertFstatfsResult(`fstatfs 9 <ptr>`, syscall.Statfs_t{})
	s.sys.InsertFstatResult(`fstat 6 <ptr>`, syscall.Stat_t{})

	// layout mount
	chg := &update.Change{
		Action: update.Mount,
		Entry: osutil.MountEntry{
			Name:    "/snap/some-snap/x1/rofs/dir/target",
			Dir:     "/usr/share/target",
			Options: []string{"rbind", "rw", osutil.XSnapdKindFile(), osutil.XSnapdOriginLayout()},
		},
	}

	synth := mylog.Check2(chg.Perform(s.as))

	c.Check(synth, HasLen, 1)
	// XSnapdEntryID defaults to entry target directory if x-snapd.id is unset
	c.Check(synth[0].Entry.XSnapdNeededBy(), Equals, "/usr/share/target")
}
