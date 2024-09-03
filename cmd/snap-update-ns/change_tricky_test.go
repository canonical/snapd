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
	// zyga@wyzima:/run/snapd/ns$ sudo nsenter -mtest-snapd-content-layout.mnt
	// root@wyzima:/# ls -l /usr/share/secureboot/potato
	// total 1
	// -rw-rw-r-- 1 root root 22 Aug 30 09:36 canary.txt
	// drwxrwxr-x 2 root root 32 Aug 30 09:36 meta
	// root@wyzima:/# ls -l /snap/test-snapd-content-layout/
	// current/ x1/      x2/
	// root@wyzima:/# ls -l /snap/test-snapd-content-layout/x2/attached-content/
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

	// The change plan is to do detach both the content and the layout
	// entry and later on to re-construct the layout using a different
	// device exposed at the unchanged path.
	c.Assert(changes, DeepEquals, []*update.Change{
		{Action: "unmount", Entry: withDetachOption(current.Entries[4])}, // Layout is not reused
		{Action: "keep", Entry: current.Entries[3]},                      // Mimic is reused
		{Action: "keep", Entry: current.Entries[2]},                      // Mimic is reused
		{Action: "unmount", Entry: withDetachOption(current.Entries[1])}, // Content is not reused
		{Action: "keep", Entry: current.Entries[0]},                      // Rootfs tmpfs is reused
		{Action: "mount", Entry: current.Entries[4]},                     // Layout is re-created to the now-detached content attachment point
	})
}

func (s *changeSuite) TestContentLayout3InitiallyConnectedThenDisconnectedAndReconnected(c *C) {
	current, err := osutil.LoadMountProfile("testdata/usr-share-secureboot-potato/2-after-disconnect.current.fstab")
	c.Assert(err, IsNil)
	desired, err := osutil.LoadMountProfile("testdata/usr-share-secureboot-potato/3-after-reconnect.desired.fstab")
	c.Assert(err, IsNil)
	changes := update.NeededChanges(current, desired)
	showCurrentDesiredAndChanges(c, current, desired, changes)

	c.Assert(changes, DeepEquals, []*update.Change{
		{Action: "keep", Entry: current.Entries[3]},
		{Action: "keep", Entry: current.Entries[2]},
		{Action: "keep", Entry: current.Entries[1]},
		{Action: "keep", Entry: current.Entries[0]},
		{Action: "mount", Entry: desired.Entries[1]}, /* changes[4] */
	})

	// The actual entry for clarity.
	c.Assert(changes[4].Entry, DeepEquals, osutil.MountEntry{
		Name:    "/snap/test-snapd-content/x1",
		Dir:     "/snap/test-snapd-content-layout/x2/attached-content",
		Type:    "none",
		Options: []string{"bind", "ro"},
	})
}

func (s *changeSuite) TestContentLayout4InitiallyDisconnectedThenConnected(c *C) {
	current, err := osutil.LoadMountProfile("testdata/usr-share-secureboot-potato/4-initially-disconnected-then-connected.before.current.fstab")
	c.Assert(err, IsNil)
	desired, err := osutil.LoadMountProfile("testdata/usr-share-secureboot-potato/4-initially-disconnected-then-connected.desired.fstab")
	c.Assert(err, IsNil)
	changes := update.NeededChanges(current, desired)
	showCurrentDesiredAndChanges(c, current, desired, changes)

	c.Assert(changes, DeepEquals, []*update.Change{
		{Action: "keep", Entry: current.Entries[3]},
		{Action: "keep", Entry: current.Entries[2]},
		{Action: "keep", Entry: current.Entries[1]},
		{Action: "keep", Entry: current.Entries[0]},
		{Action: "mount", Entry: desired.Entries[1]},
	})

	// The actual entry for clarity.
	c.Assert(changes[4].Entry, DeepEquals, osutil.MountEntry{
		Name:    "/snap/test-snapd-content/x1",
		Dir:     "/snap/test-snapd-content-layout/x2/attached-content",
		Type:    "none",
		Options: []string{"bind", "ro"},
	})
}

func (s *changeSuite) TestContentLayout5InitiallyConnectedThenContentRefreshed(c *C) {
	current, err := osutil.LoadMountProfile("testdata/usr-share-secureboot-potato/5-initially-connected-then-content-refreshed.before.current.fstab")
	c.Assert(err, IsNil)
	desired, err := osutil.LoadMountProfile("testdata/usr-share-secureboot-potato/5-initially-connected-then-content-refreshed.desired.fstab")
	c.Assert(err, IsNil)
	changes := update.NeededChanges(current, desired)
	showCurrentDesiredAndChanges(c, current, desired, changes)

	// This test shows similar behavior to -2- test - the layout stays propagated.
	c.Assert(changes, DeepEquals, []*update.Change{
		{Action: "unmount", Entry: withDetachOption(current.Entries[4])},
		{Action: "keep", Entry: current.Entries[3]},
		{Action: "keep", Entry: current.Entries[2]},
		{Action: "unmount", Entry: withDetachOption(current.Entries[1])},
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
		{Action: "keep", Entry: current.Entries[3]},
		{Action: "keep", Entry: current.Entries[2]},
		{Action: "unmount", Entry: withDetachOption(current.Entries[1])},
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
	c.Logf("Number of current entires: %d", len(current.Entries))
	for _, entry := range current.Entries {
		c.Logf("- current : %v", entry)
	}
	c.Logf("Number of desired entires: %d", len(desired.Entries))
	for _, entry := range desired.Entries {
		c.Logf("- desired: %v", entry)
	}
	c.Logf("Number of changes: %d", len(changes))
	for _, change := range changes {
		c.Logf("- change: %v", change)
	}
}
