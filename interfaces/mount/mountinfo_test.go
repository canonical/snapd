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

type mountinfoSuite struct{}

var _ = Suite(&mountinfoSuite{})

// Check that parsing the example from kernel documentation works correctly.
func (s *mountinfoSuite) TestParseInfoEntry1(c *C) {
	entry, err := mount.ParseInfoEntry(
		"36 35 98:0 /mnt1 /mnt2 rw,noatime master:1 - ext3 /dev/root rw,errors=continue")
	c.Assert(err, IsNil)
	c.Assert(entry.MountID, Equals, 36)
	c.Assert(entry.ParentID, Equals, 35)
	c.Assert(entry.DevMajor, Equals, 98)
	c.Assert(entry.DevMinor, Equals, 0)
	c.Assert(entry.Root, Equals, "/mnt1")
	c.Assert(entry.MountDir, Equals, "/mnt2")
	c.Assert(entry.MountOpts, Equals, "rw,noatime")
	c.Assert(entry.OptionalFlds, Equals, "master:1")
	c.Assert(entry.FsType, Equals, "ext3")
	c.Assert(entry.MountSource, Equals, "/dev/root")
	c.Assert(entry.SuperOpts, Equals, "rw,errors=continue")
}

// Check that various combinations of optional fields are parsed correctly.
func (s *mountinfoSuite) TestParseInfoEntry2(c *C) {
	// No optional fields.
	entry, err := mount.ParseInfoEntry(
		"36 35 98:0 /mnt1 /mnt2 rw,noatime - ext3 /dev/root rw,errors=continue")
	c.Assert(err, IsNil)
	c.Assert(entry.MountOpts, Equals, "rw,noatime")
	c.Assert(entry.OptionalFlds, Equals, "")
	c.Assert(entry.FsType, Equals, "ext3")
	// One optional field.
	entry, err = mount.ParseInfoEntry(
		"36 35 98:0 /mnt1 /mnt2 rw,noatime master:1 - ext3 /dev/root rw,errors=continue")
	c.Assert(err, IsNil)
	c.Assert(entry.MountOpts, Equals, "rw,noatime")
	c.Assert(entry.OptionalFlds, Equals, "master:1")
	c.Assert(entry.FsType, Equals, "ext3")
	// Two optional fields.
	entry, err = mount.ParseInfoEntry(
		"36 35 98:0 /mnt1 /mnt2 rw,noatime master:1 slave:2 - ext3 /dev/root rw,errors=continue")
	c.Assert(err, IsNil)
	c.Assert(entry.MountOpts, Equals, "rw,noatime")
	c.Assert(entry.OptionalFlds, Equals, "master:1 slave:2")
	c.Assert(entry.FsType, Equals, "ext3")
}

// Check that white-space is unescaped correctly, except for OptionalFlds which are space-separated.
func (s *mountinfoSuite) TestParseInfoEntry3(c *C) {
	entry, err := mount.ParseInfoEntry(
		`36 35 98:0 /mnt\0401 /mnt\0402 rw\040,noatime mas\040ter:1 - ext\0403 /dev/ro\040ot rw\040,errors=continue`)
	c.Assert(err, IsNil)
	c.Assert(entry.MountID, Equals, 36)
	c.Assert(entry.ParentID, Equals, 35)
	c.Assert(entry.DevMajor, Equals, 98)
	c.Assert(entry.DevMinor, Equals, 0)
	c.Assert(entry.Root, Equals, "/mnt 1")
	c.Assert(entry.MountDir, Equals, "/mnt 2")
	c.Assert(entry.MountOpts, Equals, "rw ,noatime")
	// This field is still escaped as it is space-separated and needs further parsing.
	c.Assert(entry.OptionalFlds, Equals, `mas\040ter:1`)
	c.Assert(entry.FsType, Equals, "ext 3")
	c.Assert(entry.MountSource, Equals, "/dev/ro ot")
	c.Assert(entry.SuperOpts, Equals, "rw ,errors=continue")
}

// Check that various malformed entries are detected.
func (s *mountinfoSuite) TestParseInfoEntry4(c *C) {
	var err error
	_, err = mount.ParseInfoEntry("36 35 98:0 /mnt1 /mnt2 rw,noatime master:1 - ext3 /dev/root rw,errors=continue foo")
	c.Assert(err, ErrorMatches, "incorrect number of tail fields, expected 3 but found 4")
	_, err = mount.ParseInfoEntry("36 35 98:0 /mnt1 /mnt2 rw,noatime master:1 - ext3 /dev/root")
	c.Assert(err, ErrorMatches, "incorrect number of tail fields, expected 3 but found 2")
	_, err = mount.ParseInfoEntry("36 35 98:0 /mnt1 /mnt2 rw,noatime master:1 - ext3")
	c.Assert(err, ErrorMatches, "incorrect number of fields, expected at least 10 but found 9")
	_, err = mount.ParseInfoEntry("36 35 98:0 /mnt1 /mnt2 rw,noatime master:1 -")
	c.Assert(err, ErrorMatches, "incorrect number of fields, expected at least 10 but found 8")
	_, err = mount.ParseInfoEntry("36 35 98:0 /mnt1 /mnt2 rw,noatime master:1")
	c.Assert(err, ErrorMatches, "incorrect number of fields, expected at least 10 but found 7")
	_, err = mount.ParseInfoEntry("36 35 98:0 /mnt1 /mnt2 rw,noatime master:1 garbage1 garbage2 garbage3")
	c.Assert(err, ErrorMatches, "list of optional fields is not terminated properly")
	_, err = mount.ParseInfoEntry("foo 35 98:0 /mnt1 /mnt2 rw,noatime master:1 - ext3 /dev/root rw,errors=continue foo")
	c.Assert(err, ErrorMatches, `cannot parse mount ID: "foo"`)
	_, err = mount.ParseInfoEntry("36 bar 98:0 /mnt1 /mnt2 rw,noatime master:1 - ext3 /dev/root rw,errors=continue foo")
	c.Assert(err, ErrorMatches, `cannot parse parent mount ID: "bar"`)
	_, err = mount.ParseInfoEntry("36 35 froz:0 /mnt1 /mnt2 rw,noatime master:1 - ext3 /dev/root rw,errors=continue foo")
	c.Assert(err, ErrorMatches, `cannot parse device major number: "froz"`)
	_, err = mount.ParseInfoEntry("36 35 98:bot /mnt1 /mnt2 rw,noatime master:1 - ext3 /dev/root rw,errors=continue foo")
	c.Assert(err, ErrorMatches, `cannot parse device minor number: "bot"`)
	_, err = mount.ParseInfoEntry("36 35 corrupt /mnt1 /mnt2 rw,noatime master:1 - ext3 /dev/root rw,errors=continue foo")
	c.Assert(err, ErrorMatches, `cannot parse device major:minor number pair: "corrupt"`)
}
