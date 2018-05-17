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

package osutil_test

import (
	"io/ioutil"
	"path/filepath"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/osutil"
)

type mountinfoSuite struct{}

var _ = Suite(&mountinfoSuite{})

// Check that parsing the example from kernel documentation works correctly.
func (s *mountinfoSuite) TestParseMountInfoEntry1(c *C) {
	real := "36 35 98:0 /mnt1 /mnt2 rw,noatime master:1 - ext3 /dev/root rw,errors=continue"
	canonical := "36 35 98:0 /mnt1 /mnt2 noatime,rw master:1 - ext3 /dev/root errors=continue,rw"
	entry, err := osutil.ParseMountInfoEntry(real)
	c.Assert(err, IsNil)
	c.Assert(entry.String(), Equals, canonical)

	c.Assert(entry.MountID, Equals, 36)
	c.Assert(entry.ParentID, Equals, 35)
	c.Assert(entry.DevMajor, Equals, 98)
	c.Assert(entry.DevMinor, Equals, 0)
	c.Assert(entry.Root, Equals, "/mnt1")
	c.Assert(entry.MountDir, Equals, "/mnt2")
	c.Assert(entry.MountOptions, DeepEquals, map[string]string{"rw": "", "noatime": ""})
	c.Assert(entry.OptionalFields, DeepEquals, []string{"master:1"})
	c.Assert(entry.FsType, Equals, "ext3")
	c.Assert(entry.MountSource, Equals, "/dev/root")
	c.Assert(entry.SuperOptions, DeepEquals, map[string]string{"rw": "", "errors": "continue"})
}

// Check that various combinations of optional fields are parsed correctly.
func (s *mountinfoSuite) TestParseMountInfoEntry2(c *C) {
	// No optional fields.
	real := "36 35 98:0 /mnt1 /mnt2 rw,noatime - ext3 /dev/root rw,errors=continue"
	canonical := "36 35 98:0 /mnt1 /mnt2 noatime,rw - ext3 /dev/root errors=continue,rw"
	entry, err := osutil.ParseMountInfoEntry(real)
	c.Assert(err, IsNil)
	c.Assert(entry.String(), Equals, canonical)

	c.Assert(entry.MountOptions, DeepEquals, map[string]string{"rw": "", "noatime": ""})
	c.Assert(entry.OptionalFields, HasLen, 0)
	c.Assert(entry.FsType, Equals, "ext3")
	// One optional field.
	entry, err = osutil.ParseMountInfoEntry(
		"36 35 98:0 /mnt1 /mnt2 rw,noatime master:1 - ext3 /dev/root rw,errors=continue")
	c.Assert(err, IsNil)
	c.Assert(entry.MountOptions, DeepEquals, map[string]string{"rw": "", "noatime": ""})
	c.Assert(entry.OptionalFields, DeepEquals, []string{"master:1"})
	c.Assert(entry.FsType, Equals, "ext3")
	// Two optional fields.
	entry, err = osutil.ParseMountInfoEntry(
		"36 35 98:0 /mnt1 /mnt2 rw,noatime master:1 slave:2 - ext3 /dev/root rw,errors=continue")
	c.Assert(err, IsNil)
	c.Assert(entry.MountOptions, DeepEquals, map[string]string{"rw": "", "noatime": ""})
	c.Assert(entry.OptionalFields, DeepEquals, []string{"master:1", "slave:2"})
	c.Assert(entry.FsType, Equals, "ext3")
}

// Check that white-space is unescaped correctly.
func (s *mountinfoSuite) TestParseMountInfoEntry3(c *C) {
	real := `36 35 98:0 /mnt\0401 /mnt\0402 noatime,rw\040 mas\040ter:1 - ext\0403 /dev/ro\040ot rw\040,errors=continue`
	canonical := `36 35 98:0 /mnt\0401 /mnt\0402 noatime,rw\040 mas\040ter:1 - ext\0403 /dev/ro\040ot errors=continue,rw\040`
	entry, err := osutil.ParseMountInfoEntry(real)
	c.Assert(err, IsNil)
	c.Assert(entry.String(), Equals, canonical)

	c.Assert(entry.MountID, Equals, 36)
	c.Assert(entry.ParentID, Equals, 35)
	c.Assert(entry.DevMajor, Equals, 98)
	c.Assert(entry.DevMinor, Equals, 0)
	c.Assert(entry.Root, Equals, "/mnt 1")
	c.Assert(entry.MountDir, Equals, "/mnt 2")
	c.Assert(entry.MountOptions, DeepEquals, map[string]string{"rw ": "", "noatime": ""})
	// This field is still escaped as it is space-separated and needs further parsing.
	c.Assert(entry.OptionalFields, DeepEquals, []string{"mas ter:1"})
	c.Assert(entry.FsType, Equals, "ext 3")
	c.Assert(entry.MountSource, Equals, "/dev/ro ot")
	c.Assert(entry.SuperOptions, DeepEquals, map[string]string{"rw ": "", "errors": "continue"})
}

// Check that various malformed entries are detected.
func (s *mountinfoSuite) TestParseMountInfoEntry4(c *C) {
	var err error
	_, err = osutil.ParseMountInfoEntry("36 35 98:0 /mnt1 /mnt2 rw,noatime master:1 - ext3 /dev/root rw,errors=continue foo")
	c.Assert(err, ErrorMatches, "incorrect number of tail fields, expected 3 but found 4")
	_, err = osutil.ParseMountInfoEntry("36 35 98:0 /mnt1 /mnt2 rw,noatime master:1 - ext3 /dev/root")
	c.Assert(err, ErrorMatches, "incorrect number of tail fields, expected 3 but found 2")
	_, err = osutil.ParseMountInfoEntry("36 35 98:0 /mnt1 /mnt2 rw,noatime master:1 - ext3")
	c.Assert(err, ErrorMatches, "incorrect number of fields, expected at least 10 but found 9")
	_, err = osutil.ParseMountInfoEntry("36 35 98:0 /mnt1 /mnt2 rw,noatime master:1 -")
	c.Assert(err, ErrorMatches, "incorrect number of fields, expected at least 10 but found 8")
	_, err = osutil.ParseMountInfoEntry("36 35 98:0 /mnt1 /mnt2 rw,noatime master:1")
	c.Assert(err, ErrorMatches, "incorrect number of fields, expected at least 10 but found 7")
	_, err = osutil.ParseMountInfoEntry("36 35 98:0 /mnt1 /mnt2 rw,noatime master:1 garbage1 garbage2 garbage3")
	c.Assert(err, ErrorMatches, "list of optional fields is not terminated properly")
	_, err = osutil.ParseMountInfoEntry("foo 35 98:0 /mnt1 /mnt2 rw,noatime master:1 - ext3 /dev/root rw,errors=continue foo")
	c.Assert(err, ErrorMatches, `cannot parse mount ID: "foo"`)
	_, err = osutil.ParseMountInfoEntry("36 bar 98:0 /mnt1 /mnt2 rw,noatime master:1 - ext3 /dev/root rw,errors=continue foo")
	c.Assert(err, ErrorMatches, `cannot parse parent mount ID: "bar"`)
	_, err = osutil.ParseMountInfoEntry("36 35 froz:0 /mnt1 /mnt2 rw,noatime master:1 - ext3 /dev/root rw,errors=continue foo")
	c.Assert(err, ErrorMatches, `cannot parse device major number: "froz"`)
	_, err = osutil.ParseMountInfoEntry("36 35 98:bot /mnt1 /mnt2 rw,noatime master:1 - ext3 /dev/root rw,errors=continue foo")
	c.Assert(err, ErrorMatches, `cannot parse device minor number: "bot"`)
	_, err = osutil.ParseMountInfoEntry("36 35 corrupt /mnt1 /mnt2 rw,noatime master:1 - ext3 /dev/root rw,errors=continue foo")
	c.Assert(err, ErrorMatches, `cannot parse device major:minor number pair: "corrupt"`)
}

// Test that empty mountinfo is parsed without errors.
func (s *mountinfoSuite) TestReadMountInfo1(c *C) {
	entries, err := osutil.ReadMountInfo(strings.NewReader(""))
	c.Assert(err, IsNil)
	c.Assert(entries, HasLen, 0)
}

const mountInfoSample = "" +
	"19 25 0:18 / /sys rw,nosuid,nodev,noexec,relatime shared:7 - sysfs sysfs rw\n" +
	"20 25 0:4 / /proc rw,nosuid,nodev,noexec,relatime shared:13 - proc proc rw\n" +
	"21 25 0:6 / /dev rw,nosuid,relatime shared:2 - devtmpfs udev rw,size=1937696k,nr_inodes=484424,mode=755\n"

// Test that mountinfo is parsed without errors.
func (s *mountinfoSuite) TestReadMountInfo2(c *C) {
	entries, err := osutil.ReadMountInfo(strings.NewReader(mountInfoSample))
	c.Assert(err, IsNil)
	c.Assert(entries, HasLen, 3)
}

// Test that loading mountinfo from a file works as expected.
func (s *mountinfoSuite) TestLoadMountInfo1(c *C) {
	fname := filepath.Join(c.MkDir(), "mountinfo")
	err := ioutil.WriteFile(fname, []byte(mountInfoSample), 0644)
	c.Assert(err, IsNil)
	entries, err := osutil.LoadMountInfo(fname)
	c.Assert(err, IsNil)
	c.Assert(entries, HasLen, 3)
}

// Test that loading mountinfo from a missing file reports an error.
func (s *mountinfoSuite) TestLoadMountInfo2(c *C) {
	fname := filepath.Join(c.MkDir(), "mountinfo")
	_, err := osutil.LoadMountInfo(fname)
	c.Assert(err, ErrorMatches, "*. no such file or directory")
}
