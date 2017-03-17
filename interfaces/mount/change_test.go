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

package mount_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces/mount"
)

type changeSuite struct{}

var _ = Suite(&changeSuite{})

// When there are no profiles we don't do anything.
func (s *changeSuite) TestNeededChangesNoProfiles(c *C) {
	changes := mount.NeededChanges(nil, nil)
	c.Assert(changes, IsNil)
}

// When the profiles are the same we don't do anything.
func (s *changeSuite) TestNeededChangesNoChange(c *C) {
	current := []mount.Entry{{Dir: "/var/snap/foo/common/stuf"}}
	desired := []mount.Entry{{Dir: "/var/snap/foo/common/stuf"}}
	changes := mount.NeededChanges(current, desired)
	c.Assert(changes, IsNil)
}

// When the content interface is connected we should mount the new entry.
func (s *changeSuite) TestNeededChangesTrivialMount(c *C) {
	var current []mount.Entry
	desired := []mount.Entry{{Dir: "/var/snap/foo/common/stuf"}}
	changes := mount.NeededChanges(current, desired)
	c.Assert(changes, DeepEquals, []mount.Change{
		{Action: mount.Mount, Entry: desired[0]},
	})
}

// When the content interface is disconnected we should unmount the mounted entry.
func (s *changeSuite) TestNeededChangesTrivialUnmount(c *C) {
	current := []mount.Entry{{Dir: "/var/snap/foo/common/stuf"}}
	var desired []mount.Entry
	changes := mount.NeededChanges(current, desired)
	c.Assert(changes, DeepEquals, []mount.Change{
		{Action: mount.Unmount, Entry: current[0]},
	})
}

// When umounting we unmount children before parents.
func (s *changeSuite) TestNeededChangesUnmountOrder(c *C) {
	current := []mount.Entry{
		{Dir: "/var/snap/foo/common/stuf/extra"},
		{Dir: "/var/snap/foo/common/stuf"},
	}
	var desired []mount.Entry
	changes := mount.NeededChanges(current, desired)
	c.Assert(changes, DeepEquals, []mount.Change{
		{Action: mount.Unmount, Entry: mount.Entry{Dir: "/var/snap/foo/common/stuf/extra"}},
		{Action: mount.Unmount, Entry: mount.Entry{Dir: "/var/snap/foo/common/stuf"}},
	})
}

// When mounting we mount the parents before the children.
func (s *changeSuite) TestNeededChangesMountOrder(c *C) {
	var current []mount.Entry
	desired := []mount.Entry{
		{Dir: "/var/snap/foo/common/stuf/extra"},
		{Dir: "/var/snap/foo/common/stuf"},
	}
	changes := mount.NeededChanges(current, desired)
	c.Assert(changes, DeepEquals, []mount.Change{
		{Action: mount.Mount, Entry: mount.Entry{Dir: "/var/snap/foo/common/stuf"}},
		{Action: mount.Mount, Entry: mount.Entry{Dir: "/var/snap/foo/common/stuf/extra"}},
	})
}
