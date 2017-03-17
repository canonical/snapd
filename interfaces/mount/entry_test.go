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

type entrySuite struct{}

var _ = Suite(&entrySuite{})

func (s *entrySuite) TestString(c *C) {
	ent0 := mount.Entry{}
	c.Assert(ent0.String(), Equals, "none none none defaults 0 0")
	ent1 := mount.Entry{
		Name:    "/var/snap/foo/common",
		Dir:     "/var/snap/bar/common",
		Options: []string{"bind"},
	}
	c.Assert(ent1.String(), Equals,
		"/var/snap/foo/common /var/snap/bar/common none bind 0 0")
	ent2 := mount.Entry{
		Name:    "/dev/sda5",
		Dir:     "/media/foo",
		Type:    "ext4",
		Options: []string{"rw,noatime"},
	}
	c.Assert(ent2.String(), Equals, "/dev/sda5 /media/foo ext4 rw,noatime 0 0")
	ent3 := mount.Entry{
		Name:    "/dev/sda5",
		Dir:     "/media/My Files",
		Type:    "ext4",
		Options: []string{"rw,noatime"},
	}
	c.Assert(ent3.String(), Equals, `/dev/sda5 /media/My\040Files ext4 rw,noatime 0 0`)
}

func (s *entrySuite) TestEqual(c *C) {
	c.Assert(mount.EqualEntries(&mount.Entry{}, &mount.Entry{}), Equals, true)
	c.Assert(mount.EqualEntries(&mount.Entry{Dir: "foo"}, &mount.Entry{Dir: "foo"}), Equals, true)
	c.Assert(mount.EqualEntries(&mount.Entry{Options: []string{"ro"}},
		&mount.Entry{Options: []string{"ro"}}), Equals, true)

	c.Assert(mount.EqualEntries(&mount.Entry{Dir: "foo"}, &mount.Entry{Dir: "bar"}), Equals, false)
	c.Assert(mount.EqualEntries(&mount.Entry{}, &mount.Entry{Options: []string{"ro"}}), Equals, false)
	c.Assert(mount.EqualEntries(&mount.Entry{Options: []string{"ro"}},
		&mount.Entry{Options: []string{"rw"}}), Equals, false)
}
