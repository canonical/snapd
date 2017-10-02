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
	"os"
	"os/user"
	"strconv"
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
	// udisks use fstab options to convey additional data.
	flags, err = mount.OptsToFlags([]string{"x-snapd-foo"})
	c.Assert(err, IsNil)
	c.Assert(flags, Equals, 0)
}

func (s *entrySuite) TestXSnapdMode(c *C) {
	// Mode has a default value.
	e := &mount.Entry{}
	mode, err := e.XSnapdMode()
	c.Assert(err, IsNil)
	c.Assert(mode, Equals, os.FileMode(0755))

	// Mode is parsed from the x-snapd-mode= option.
	e = &mount.Entry{Options: []string{"x-snapd-mode=0700"}}
	mode, err = e.XSnapdMode()
	c.Assert(err, IsNil)
	c.Assert(mode, Equals, os.FileMode(0700))

	// Empty value is invalid.
	e = &mount.Entry{Options: []string{"x-snapd-mode="}}
	_, err = e.XSnapdMode()
	c.Assert(err, ErrorMatches, `cannot parse octal file mode from ""`)

	// As well as other bogus values.
	e = &mount.Entry{Options: []string{"x-snapd-mode=pasta"}}
	_, err = e.XSnapdMode()
	c.Assert(err, ErrorMatches, `cannot parse octal file mode from "pasta"`)
}

func (s *entrySuite) TestXSnapdMkdirUid(c *C) {
	// User has a default value.
	e := &mount.Entry{}
	uid, err := e.XSnapdMkdirUid()
	c.Assert(err, IsNil)
	c.Assert(uid, Equals, uint64(0))

	// User is parsed from the x-snapd-user= option.
	daemon, err := user.Lookup("daemon")
	c.Assert(err, IsNil)
	daemonUid, err := strconv.ParseUint(daemon.Uid, 10, 64)
	c.Assert(err, IsNil)
	e = &mount.Entry{Options: []string{"x-snapd-mkdir-uid=daemon"}}
	uid, err = e.XSnapdMkdirUid()
	c.Assert(err, IsNil)
	c.Assert(uid, Equals, daemonUid)

	// Unknown user names are invalid.
	e = &mount.Entry{Options: []string{"x-snapd-mkdir-uid=.bogus"}}
	_, err = e.XSnapdMkdirUid()
	c.Assert(err, ErrorMatches, `cannot resolve user name ".bogus"`)
}

func (s *entrySuite) TestXSnapdMkdirGid(c *C) {
	// Group has a default value.
	e := &mount.Entry{}
	gid, err := e.XSnapdMkdirGid()
	c.Assert(err, IsNil)
	c.Assert(gid, Equals, uint64(0))

	// Group is parsed from the x-snapd-group= option.
	daemon, err := user.LookupGroup("daemon")
	c.Assert(err, IsNil)
	daemonGid, err := strconv.ParseUint(daemon.Gid, 10, 64)
	c.Assert(err, IsNil)
	e = &mount.Entry{Options: []string{"x-snapd-mkdir-gid=daemon"}}
	gid, err = e.XSnapdMkdirGid()
	c.Assert(err, IsNil)
	c.Assert(gid, Equals, daemonGid)

	// Unknown group names are invalid.
	e = &mount.Entry{Options: []string{"x-snapd-mkdir-gid=.bogus"}}
	_, err = e.XSnapdMkdirGid()
	c.Assert(err, ErrorMatches, `cannot resolve group name ".bogus"`)
}
