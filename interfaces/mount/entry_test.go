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
	"syscall"

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
	var a, b *mount.Entry
	a = &mount.Entry{}
	b = &mount.Entry{}
	c.Assert(a.Equal(b), Equals, true)
	a = &mount.Entry{Dir: "foo"}
	b = &mount.Entry{Dir: "foo"}
	c.Assert(a.Equal(b), Equals, true)
	a = &mount.Entry{Options: []string{"ro"}}
	b = &mount.Entry{Options: []string{"ro"}}
	c.Assert(a.Equal(b), Equals, true)
	a = &mount.Entry{Dir: "foo"}
	b = &mount.Entry{Dir: "bar"}
	c.Assert(a.Equal(b), Equals, false)
	a = &mount.Entry{}
	b = &mount.Entry{Options: []string{"ro"}}
	c.Assert(a.Equal(b), Equals, false)
	a = &mount.Entry{Options: []string{"ro"}}
	b = &mount.Entry{Options: []string{"rw"}}
	c.Assert(a.Equal(b), Equals, false)
}

// Test that typical fstab entry is parsed correctly.
func (s *entrySuite) TestParseEntry1(c *C) {
	e, err := mount.ParseEntry("UUID=394f32c0-1f94-4005-9717-f9ab4a4b570b /               ext4    errors=remount-ro 0       1")
	c.Assert(err, IsNil)
	c.Assert(e.Name, Equals, "UUID=394f32c0-1f94-4005-9717-f9ab4a4b570b")
	c.Assert(e.Dir, Equals, "/")
	c.Assert(e.Type, Equals, "ext4")
	c.Assert(e.Options, DeepEquals, []string{"errors=remount-ro"})
	c.Assert(e.DumpFrequency, Equals, 0)
	c.Assert(e.CheckPassNumber, Equals, 1)
}

// Test that options are parsed correctly
func (s *entrySuite) TestParseEntry2(c *C) {
	e, err := mount.ParseEntry("name dir type options,comma,separated 0 0")
	c.Assert(err, IsNil)
	c.Assert(e.Name, Equals, "name")
	c.Assert(e.Dir, Equals, "dir")
	c.Assert(e.Type, Equals, "type")
	c.Assert(e.Options, DeepEquals, []string{"options", "comma", "separated"})
	c.Assert(e.DumpFrequency, Equals, 0)
	c.Assert(e.CheckPassNumber, Equals, 0)
}

// Test that whitespace escape codes are honored
func (s *entrySuite) TestParseEntry3(c *C) {
	e, err := mount.ParseEntry(`na\040me d\011ir ty\012pe optio\134ns 0 0`)
	c.Assert(err, IsNil)
	c.Assert(e.Name, Equals, "na me")
	c.Assert(e.Dir, Equals, "d\tir")
	c.Assert(e.Type, Equals, "ty\npe")
	c.Assert(e.Options, DeepEquals, []string{`optio\ns`})
	c.Assert(e.DumpFrequency, Equals, 0)
	c.Assert(e.CheckPassNumber, Equals, 0)
}

// Test that number of fields is checked
func (s *entrySuite) TestParseEntry4(c *C) {
	for _, s := range []string{
		"", "1", "1 2", "1 2 3" /* skip 4, 5 and 6 fields (valid case) */, "1 2 3 4 5 6 7",
	} {
		_, err := mount.ParseEntry(s)
		c.Assert(err, ErrorMatches, "expected between 4 and 6 fields, found [01237]")
	}
}

// Test that integers are parsed and error checked
func (s *entrySuite) TestParseEntry5(c *C) {
	_, err := mount.ParseEntry("name dir type options foo 0")
	c.Assert(err, ErrorMatches, "cannot parse dump frequency: .*")
	_, err = mount.ParseEntry("name dir type options 0 foo")
	c.Assert(err, ErrorMatches, "cannot parse check pass number: .*")
}

// Test that last two integer fields default to zero if not present.
func (s *entrySuite) TestParseEntry6(c *C) {
	e, err := mount.ParseEntry("name dir type options")
	c.Assert(err, IsNil)
	c.Assert(e.DumpFrequency, Equals, 0)
	c.Assert(e.CheckPassNumber, Equals, 0)

	e, err = mount.ParseEntry("name dir type options 5")
	c.Assert(err, IsNil)
	c.Assert(e.DumpFrequency, Equals, 5)
	c.Assert(e.CheckPassNumber, Equals, 0)

	e, err = mount.ParseEntry("name dir type options 5 7")
	c.Assert(err, IsNil)
	c.Assert(e.DumpFrequency, Equals, 5)
	c.Assert(e.CheckPassNumber, Equals, 7)
}

// Test (string) options -> (int) flag conversion code.
func (s *entrySuite) TestOptsToFlags(c *C) {
	flags, err := mount.OptsToFlags(nil)
	c.Assert(err, IsNil)
	c.Assert(flags, Equals, 0)
	flags, err = mount.OptsToFlags([]string{"ro", "nodev", "nosuid"})
	c.Assert(err, IsNil)
	c.Assert(flags, Equals, syscall.MS_RDONLY|syscall.MS_NODEV|syscall.MS_NOSUID)
	_, err = mount.OptsToFlags([]string{"bogus"})
	c.Assert(err, ErrorMatches, `unsupported mount option: "bogus"`)
	// The x-snapd-prefix is reserved for non-kernel parameters that do not
	// translate to kernel level mount flags. This is similar to systemd or
	// udisks that use fstab options to convey additional data.
	flags, err = mount.OptsToFlags([]string{"x-snapd.foo"})
	c.Assert(err, IsNil)
	c.Assert(flags, Equals, 0)
}

// Test (string) options -> (int, unparsed) flag conversion code.
func (s *entrySuite) TestOptsToCommonFlags(c *C) {
	flags, unparsed := mount.OptsToCommonFlags(nil)
	c.Assert(flags, Equals, 0)
	c.Assert(unparsed, HasLen, 0)
	flags, unparsed = mount.OptsToCommonFlags([]string{"ro", "nodev", "nosuid"})
	c.Assert(flags, Equals, syscall.MS_RDONLY|syscall.MS_NODEV|syscall.MS_NOSUID)
	c.Assert(unparsed, HasLen, 0)
	flags, unparsed = mount.OptsToCommonFlags([]string{"bogus"})
	c.Assert(flags, Equals, 0)
	c.Assert(unparsed, DeepEquals, []string{"bogus"})
	// The x-snapd-prefix is reserved for non-kernel parameters that do not
	// translate to kernel level mount flags. This is similar to systemd or
	// udisks that use fstab options to convey additional data. Those are not
	// returned as "unparsed" as we don't want to pass them to the kernel.
	flags, unparsed = mount.OptsToCommonFlags([]string{"x-snapd.foo"})
	c.Assert(flags, Equals, 0)
	c.Assert(unparsed, HasLen, 0)
}

func (s *entrySuite) TestOptStr(c *C) {
	e := &mount.Entry{Options: []string{"key=value"}}
	val, ok := e.OptStr("key")
	c.Assert(ok, Equals, true)
	c.Assert(val, Equals, "value")

	val, ok = e.OptStr("missing")
	c.Assert(ok, Equals, false)
	c.Assert(val, Equals, "")
}

func (s *entrySuite) TestOptBool(c *C) {
	e := &mount.Entry{Options: []string{"key"}}
	val := e.OptBool("key")
	c.Assert(val, Equals, true)

	val = e.OptBool("missing")
	c.Assert(val, Equals, false)
}

func (s *entrySuite) TestOptionHelpers(c *C) {
	c.Assert(mount.XSnapdKindSymlink(), Equals, "x-snapd.kind=symlink")
	c.Assert(mount.XSnapdKindFile(), Equals, "x-snapd.kind=file")
	c.Assert(mount.XSnapdUser(1000), Equals, "x-snapd.user=1000")
	c.Assert(mount.XSnapdGroup(1000), Equals, "x-snapd.group=1000")
	c.Assert(mount.XSnapdMode(0755), Equals, "x-snapd.mode=0755")
	c.Assert(mount.XSnapdSymlink("oldname"), Equals, "x-snapd.symlink=oldname")
}
