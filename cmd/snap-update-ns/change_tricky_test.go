// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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
	. "gopkg.in/check.v1"

	update "github.com/snapcore/snapd/cmd/snap-update-ns"
	"github.com/snapcore/snapd/osutil"
)

func (s *changeSuite) TestContentLayout1InitiallyConnected(c *C) {
	// NOTE: This doesn't measure what is going on during construction. It
	// merely measures what is constructed is stable and that it does not cause
	// further changes to be created.
	current, err := osutil.LoadMountProfile("testdata/usr-share-secureboot-potato/1-initially-connected.current.fstab")
	c.Assert(err, IsNil)
	desired, err := osutil.LoadMountProfile("testdata/usr-share-secureboot-potato/1-initially-connected.desired.fstab")
	c.Assert(err, IsNil)
	changes := update.NeededChanges(current, desired)
	showCurrentDesiredAndChanges(c, current, desired, changes)

	// The change plan is to do nothing.
	// Note that the order of entries is back to front.
	//
	// At this time, the mount namespace is correct:
	// zyga@wyzima:/run/snapd/ns$ sudo nsenter -mtest-snapd-layout.mnt
	// root@wyzima:/# ls -l /usr/share/secureboot/potato
	// total 1
	// -rw-rw-r-- 1 root root 22 Aug 30 09:36 canary.txt
	// drwxrwxr-x 2 root root 32 Aug 30 09:36 meta
	// root@wyzima:/# ls -l /snap/test-snapd-layout/
	// current/ x1/      x2/
	// root@wyzima:/# ls -l /snap/test-snapd-layout/x2/attached-content/
	// total 1
	// -rw-rw-r-- 1 root root 22 Aug 30 09:36 canary.txt
	// drwxrwxr-x 2 root root 32 Aug 30 09:36 meta
	c.Assert(changes, DeepEquals, []*update.Change{
		{Action: "keep", Entry: current.Entries[4]},
		{Action: "keep", Entry: current.Entries[3]},
		{Action: "keep", Entry: current.Entries[2]},
		{Action: "keep", Entry: current.Entries[1]},
		{Action: "keep", Entry: current.Entries[0]},
	})
}

func (s *changeSuite) TestContentLayout2InitiallyConnectedThenDisconnected(c *C) {
	current, err := osutil.LoadMountProfile("testdata/usr-share-secureboot-potato/1-initially-connected.current.fstab")
	c.Assert(err, IsNil)
	desired, err := osutil.LoadMountProfile("testdata/usr-share-secureboot-potato/2-after-disconnect.desired.fstab")
	c.Assert(err, IsNil)
	changes := update.NeededChanges(current, desired)
	showCurrentDesiredAndChanges(c, current, desired, changes)

	// The change plan is to detach the content entry and to re-make the layout
	// entry, as the layout is sourced from the content mount point. The layout
	// is re-created from the now unconnected source directory, which yields the
	// same state as a fresh bootstrap would. This keeps the layout from pinning
	// the detached content mount, which was the case historically and is
	// tracked as https://warthogs.atlassian.net/browse/SNAPDENG-31645
	//
	// Note that the order of entries is back to front.
	c.Assert(changes, DeepEquals, []*update.Change{
		{Action: "unmount", Entry: withDetachOption(current.Entries[4])},
		{Action: "unmount", Entry: withDetachOption(current.Entries[3])},
		{Action: "keep", Entry: current.Entries[2]},
		{Action: "keep", Entry: current.Entries[1]},
		{Action: "keep", Entry: current.Entries[0]},
		{Action: "mount", Entry: desired.Entries[0]},
	})

	// The actual entries for clarity.
	c.Assert(changes[0].Entry, DeepEquals, osutil.MountEntry{
		Name:    "/snap/test-snapd-layout/x2/attached-content",
		Dir:     "/usr/share/secureboot/potato",
		Type:    "none",
		Options: []string{"rbind", "rw", "x-snapd.origin=layout", "x-snapd.detach"},
	})
	c.Assert(changes[1].Entry, DeepEquals, osutil.MountEntry{
		Name:    "/snap/test-snapd-content/x1",
		Dir:     "/snap/test-snapd-layout/x2/attached-content",
		Type:    "none",
		Options: []string{"bind", "ro", "x-snapd.detach"},
	})
}

func (s *changeSuite) TestContentLayout3InitiallyConnectedThenDisconnectedAndReconnected(c *C) {
	current, err := osutil.LoadMountProfile("testdata/usr-share-secureboot-potato/2-after-disconnect.current.fstab")
	c.Assert(err, IsNil)
	desired, err := osutil.LoadMountProfile("testdata/usr-share-secureboot-potato/3-after-reconnect.desired.fstab")
	c.Assert(err, IsNil)
	changes := update.NeededChanges(current, desired)
	showCurrentDesiredAndChanges(c, current, desired, changes)

	// Since the layout entry is sourced from the content mount point, it is
	// re-made when the content connection is (re-)established. The namespace
	// returns to the initial state, which was not the case historically, as
	// the layout entry was kept and kept pinning whatever it was bound to.
	// This is tracked as https://warthogs.atlassian.net/browse/SNAPDENG-31645
	c.Assert(changes, DeepEquals, []*update.Change{
		{Action: "unmount", Entry: withDetachOption(current.Entries[3])},
		{Action: "keep", Entry: current.Entries[2]},
		{Action: "keep", Entry: current.Entries[1]},
		{Action: "keep", Entry: current.Entries[0]},
		{Action: "mount", Entry: desired.Entries[1]},
		{Action: "mount", Entry: desired.Entries[0]},
	})

	// The actual entries for clarity. The content is mounted first and only
	// then the layout is constructed from it.
	c.Assert(changes[4].Entry, DeepEquals, osutil.MountEntry{
		Name:    "/snap/test-snapd-content/x1",
		Dir:     "/snap/test-snapd-layout/x2/attached-content",
		Type:    "none",
		Options: []string{"bind", "ro"},
	})
	c.Assert(changes[5].Entry, DeepEquals, osutil.MountEntry{
		Name:    "/snap/test-snapd-layout/x2/attached-content",
		Dir:     "/usr/share/secureboot/potato",
		Type:    "none",
		Options: []string{"rbind", "rw", "x-snapd.origin=layout"},
	})
}

func (s *changeSuite) TestContentLayout4InitiallyDisconnectedThenConnected(c *C) {
	current, err := osutil.LoadMountProfile("testdata/usr-share-secureboot-potato/4-initially-disconnected-then-connected.before.current.fstab")
	c.Assert(err, IsNil)
	desired, err := osutil.LoadMountProfile("testdata/usr-share-secureboot-potato/4-initially-disconnected-then-connected.desired.fstab")
	c.Assert(err, IsNil)
	changes := update.NeededChanges(current, desired)
	showCurrentDesiredAndChanges(c, current, desired, changes)

	// This is the layout-poisoning scenario: the layout entry was created while
	// the content was not connected, so it replicates whatever the source path
	// resolved to at that time. Connecting the content must re-make the layout
	// entry so that it picks up the connected content. Historically the layout
	// entry was kept and the poisoning persisted until the namespace was
	// discarded. This is tracked as
	// https://warthogs.atlassian.net/browse/SNAPDENG-31645
	c.Assert(changes, DeepEquals, []*update.Change{
		{Action: "unmount", Entry: withDetachOption(current.Entries[3])},
		{Action: "keep", Entry: current.Entries[2]},
		{Action: "keep", Entry: current.Entries[1]},
		{Action: "keep", Entry: current.Entries[0]},
		{Action: "mount", Entry: desired.Entries[1]},
		{Action: "mount", Entry: desired.Entries[0]},
	})
}

func (s *changeSuite) TestContentLayout5InitiallyConnectedThenContentRefreshed(c *C) {
	current, err := osutil.LoadMountProfile("testdata/usr-share-secureboot-potato/5-initially-connected-then-content-refreshed.before.current.fstab")
	c.Assert(err, IsNil)
	desired, err := osutil.LoadMountProfile("testdata/usr-share-secureboot-potato/5-initially-connected-then-content-refreshed.desired.fstab")
	c.Assert(err, IsNil)
	changes := update.NeededChanges(current, desired)
	showCurrentDesiredAndChanges(c, current, desired, changes)

	// When the content provider is refreshed, the layout entry is re-made as
	// well, so that the layout does not stay pinned to the old provider
	// revision. Historically the layout stayed propagated to the old content
	// mount. This is tracked as
	// https://warthogs.atlassian.net/browse/SNAPDENG-31645
	c.Assert(changes, DeepEquals, []*update.Change{
		{Action: "unmount", Entry: withDetachOption(current.Entries[4])},
		{Action: "unmount", Entry: withDetachOption(current.Entries[3])},
		{Action: "keep", Entry: current.Entries[2]},
		{Action: "keep", Entry: current.Entries[1]},
		{Action: "keep", Entry: current.Entries[0]},
		{Action: "mount", Entry: desired.Entries[1]},
		{Action: "mount", Entry: desired.Entries[0]},
	})
}

func (s *changeSuite) TestContentLayout6InitiallyConnectedThenAppRefreshed(c *C) {
	current, err := osutil.LoadMountProfile("testdata/usr-share-secureboot-potato/6-initially-connected-then-app-refreshed.before.current.fstab")
	c.Assert(err, IsNil)
	desired, err := osutil.LoadMountProfile("testdata/usr-share-secureboot-potato/6-initially-connected-then-app-refreshed.desired.fstab")
	c.Assert(err, IsNil)
	changes := update.NeededChanges(current, desired)
	showCurrentDesiredAndChanges(c, current, desired, changes)

	// In this case, because the attached content is mounted to $SNAP (and not $SNAP_COMMON), the path changes
	// and both the layout and content are re-made.
	c.Assert(changes, DeepEquals, []*update.Change{
		{Action: "unmount", Entry: withDetachOption(current.Entries[4])},
		{Action: "unmount", Entry: withDetachOption(current.Entries[3])},
		{Action: "keep", Entry: current.Entries[2]},
		{Action: "keep", Entry: current.Entries[1]},
		{Action: "keep", Entry: current.Entries[0]},
		// It is interesting to note that we first mount the content to $SNAP/attached-content
		// and only then construct the layout from $SNAP/attached-content to /usr/share/secureboot/potato.
		// This is correct but the ordering is fraigle.
		{Action: "mount", Entry: desired.Entries[1]},
		{Action: "mount", Entry: desired.Entries[0]},
	})
}

// withDetachOption returns a copy of the given mount entry with the x-snapd.detach option.
func withDetachOption(e osutil.MountEntry) osutil.MountEntry {
	e.Options = append([]string{}, e.Options...)
	e.Options = append(e.Options, "x-snapd.detach")
	return e
}

func showCurrentDesiredAndChanges(c *C, current, desired *osutil.MountProfile, changes []*update.Change) {
	c.Logf("Number of current entries: %d", len(current.Entries))
	for i, entry := range current.Entries {
		c.Logf("- current[%d] : %v", i, entry)
	}
	c.Logf("Number of desired entries: %d", len(desired.Entries))
	for i, entry := range desired.Entries {
		c.Logf("- desired[%d]: %v", i, entry)
	}
	c.Logf("Number of changes: %d", len(changes))
	for i, change := range changes {
		c.Logf("- change[%d]: %v", i, change)
	}
}
