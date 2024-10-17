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

	// We are in sorched earth mode, rebuild everything every time we are asked
	// to do anything. The reason this is not doing any comparison is that the
	// current profile is not really representative of the desired profile as
	// it also contains the / tmpfs set up by snap-confine (so our naive
	// comparison would always be false), and any mimics that make the reality
	// more confusing.  Instead of trading one complexity for another we rely
	// on snapd calling snap-update-ns only when something changed (and snapd
	// compares old content of desired profile with new content of desired
	// profile, something that we cannot do).
	c.Assert(changes, DeepEquals, []*update.Change{
		/* 0 */ {Action: "unmount", Entry: withDetachOption(current.Entries[4])},
		/* 1 */ {Action: "unmount", Entry: current.Entries[3]}, // This is already detach by default.
		/* 2 */ {Action: "unmount", Entry: withDetachOption(current.Entries[2])},
		/* 3 */ {Action: "unmount", Entry: withDetachOption(current.Entries[1])},
		/* 4 */ {Action: "keep", Entry: current.Entries[0]}, // Keep the rootfs
		/* 5 */ {Action: "mount", Entry: desired.Entries[1]},
		/* 6 */ {Action: "mount", Entry: desired.Entries[0]},
	})
}

func (s *changeSuite) TestContentLayout2InitiallyConnectedThenDisconnected(c *C) {
	current, err := osutil.LoadMountProfile("testdata/usr-share-secureboot-potato/1-initially-connected.current.fstab")
	c.Assert(err, IsNil)
	desired, err := osutil.LoadMountProfile("testdata/usr-share-secureboot-potato/2-after-disconnect.desired.fstab")
	c.Assert(err, IsNil)
	changes := update.NeededChanges(current, desired)
	showCurrentDesiredAndChanges(c, current, desired, changes)

	// Like above, destroy and re-construct everything.
	c.Assert(changes, DeepEquals, []*update.Change{
		/* 0 */ {Action: "unmount", Entry: withDetachOption(current.Entries[4])},
		/* 1 */ {Action: "unmount", Entry: current.Entries[3]}, // This is already using detach.
		/* 2 */ {Action: "unmount", Entry: withDetachOption(current.Entries[2])},
		/* 3 */ {Action: "unmount", Entry: withDetachOption(current.Entries[1])},
		/* 4 */ {Action: "keep", Entry: current.Entries[0]}, // Keep the rootfs
		/* 5 */ {Action: "mount", Entry: desired.Entries[0]},
	})
}

func (s *changeSuite) TestContentLayout3InitiallyConnectedThenDisconnectedAndReconnected(c *C) {
	current, err := osutil.LoadMountProfile("testdata/usr-share-secureboot-potato/2-after-disconnect.current.fstab")
	c.Assert(err, IsNil)
	desired, err := osutil.LoadMountProfile("testdata/usr-share-secureboot-potato/3-after-reconnect.desired.fstab")
	c.Assert(err, IsNil)
	changes := update.NeededChanges(current, desired)
	showCurrentDesiredAndChanges(c, current, desired, changes)

	// Like above, destroy and re-construct everything.
	c.Assert(changes, DeepEquals, []*update.Change{
		/* 0 */ {Action: "unmount", Entry: withDetachOption(current.Entries[3])},
		/* 1 */ {Action: "unmount", Entry: current.Entries[2]}, // This is already using detach.
		/* 2 */ {Action: "unmount", Entry: withDetachOption(current.Entries[1])},
		/* 3 */ {Action: "keep", Entry: current.Entries[0]}, // Keep the rootfs
		/* 4 */ {Action: "mount", Entry: desired.Entries[1]},
		/* 5 */ {Action: "mount", Entry: desired.Entries[0]},
	})
}

func (s *changeSuite) TestContentLayout4InitiallyDisconnectedThenConnected(c *C) {
	current, err := osutil.LoadMountProfile("testdata/usr-share-secureboot-potato/4-initially-disconnected-then-connected.before.current.fstab")
	c.Assert(err, IsNil)
	desired, err := osutil.LoadMountProfile("testdata/usr-share-secureboot-potato/4-initially-disconnected-then-connected.desired.fstab")
	c.Assert(err, IsNil)
	changes := update.NeededChanges(current, desired)
	showCurrentDesiredAndChanges(c, current, desired, changes)

	// Like above, destroy and re-construct everything.
	c.Assert(changes, DeepEquals, []*update.Change{
		/* 0 */ {Action: "unmount", Entry: withDetachOption(current.Entries[3])},
		/* 1 */ {Action: "unmount", Entry: current.Entries[2]}, // This is already using detach.
		/* 2 */ {Action: "unmount", Entry: withDetachOption(current.Entries[1])},
		/* 3 */ {Action: "keep", Entry: current.Entries[0]}, // Keep the rootfs
		/* 4 */ {Action: "mount", Entry: desired.Entries[1]},
		/* 5 */ {Action: "mount", Entry: desired.Entries[0]},
	})
}

func (s *changeSuite) TestContentLayout5InitiallyConnectedThenContentRefreshed(c *C) {
	current, err := osutil.LoadMountProfile("testdata/usr-share-secureboot-potato/5-initially-connected-then-content-refreshed.before.current.fstab")
	c.Assert(err, IsNil)
	desired, err := osutil.LoadMountProfile("testdata/usr-share-secureboot-potato/5-initially-connected-then-content-refreshed.desired.fstab")
	c.Assert(err, IsNil)
	changes := update.NeededChanges(current, desired)
	showCurrentDesiredAndChanges(c, current, desired, changes)

	// Like above, destroy and re-construct everything.
	c.Assert(changes, DeepEquals, []*update.Change{
		/* 0 */ {Action: "unmount", Entry: withDetachOption(current.Entries[4])},
		/* 1 */ {Action: "unmount", Entry: current.Entries[3]}, // This is already using detach.
		/* 2 */ {Action: "unmount", Entry: withDetachOption(current.Entries[2])},
		/* 3 */ {Action: "unmount", Entry: withDetachOption(current.Entries[1])},
		/* 4 */ {Action: "keep", Entry: current.Entries[0]}, // Keep the rootfs
		/* 5 */ {Action: "mount", Entry: desired.Entries[1]},
		/* 6 */ {Action: "mount", Entry: desired.Entries[0]},
	})
}

func (s *changeSuite) TestContentLayout6InitiallyConnectedThenAppRefreshed(c *C) {
	current, err := osutil.LoadMountProfile("testdata/usr-share-secureboot-potato/6-initially-connected-then-app-refreshed.before.current.fstab")
	c.Assert(err, IsNil)
	desired, err := osutil.LoadMountProfile("testdata/usr-share-secureboot-potato/6-initially-connected-then-app-refreshed.desired.fstab")
	c.Assert(err, IsNil)
	changes := update.NeededChanges(current, desired)
	showCurrentDesiredAndChanges(c, current, desired, changes)

	// Like above, destroy and re-construct everything.
	c.Assert(changes, DeepEquals, []*update.Change{
		/* 0 */ {Action: "unmount", Entry: withDetachOption(current.Entries[4])},
		/* 1 */ {Action: "unmount", Entry: current.Entries[3]}, // This is already using detach.
		/* 2 */ {Action: "unmount", Entry: withDetachOption(current.Entries[2])},
		/* 3 */ {Action: "unmount", Entry: withDetachOption(current.Entries[1])},
		/* 4 */ {Action: "keep", Entry: current.Entries[0]}, // Keep the rootfs
		/* 5 */ {Action: "mount", Entry: desired.Entries[1]},
		/* 6 */ {Action: "mount", Entry: desired.Entries[0]},
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
