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
	"math"
	"os"
	"syscall"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/osutil"
)

type entrySuite struct{}

var _ = Suite(&entrySuite{})

func (s *entrySuite) TestString(c *C) {
	ent0 := osutil.MountEntry{}
	c.Assert(ent0.String(), Equals, "none none none defaults 0 0")
	ent1 := osutil.MountEntry{
		Name:    "/var/snap/foo/common",
		Dir:     "/var/snap/bar/common",
		Options: []string{"bind"},
	}
	c.Assert(ent1.String(), Equals,
		"/var/snap/foo/common /var/snap/bar/common none bind 0 0")
	ent2 := osutil.MountEntry{
		Name:    "/dev/sda5",
		Dir:     "/media/foo",
		Type:    "ext4",
		Options: []string{"rw,noatime"},
	}
	c.Assert(ent2.String(), Equals, "/dev/sda5 /media/foo ext4 rw,noatime 0 0")
	ent3 := osutil.MountEntry{
		Name:    "/dev/sda5",
		Dir:     "/media/My Files",
		Type:    "ext4",
		Options: []string{"rw,noatime"},
	}
	c.Assert(ent3.String(), Equals, `/dev/sda5 /media/My\040Files ext4 rw,noatime 0 0`)
	ent4 := osutil.MountEntry{
		Dir:     "/usr/lib/lib4d.so.1.1.0",
		Options: []string{"x-snapd.kind=symlink", "x-snapd.symlink=/snap/snapname/165/graphics/usr/lib/lib4d.so.1.1.0", "x-snapd.origin=layout"},
	}
	c.Assert(ent4.String(), Equals, "none /usr/lib/lib4d.so.1.1.0 none x-snapd.kind=symlink,x-snapd.symlink=/snap/snapname/165/graphics/usr/lib/lib4d.so.1.1.0,x-snapd.origin=layout 0 0")
	ent5 := osutil.MountEntry{
		Dir:     "$HOME/.local/share",
		Options: []string{"x-snapd.kind=ensure-dir", "x-snapd.must-exist-dir=$HOME"},
	}
	c.Assert(ent5.String(), Equals, "none $HOME/.local/share none x-snapd.kind=ensure-dir,x-snapd.must-exist-dir=$HOME 0 0")
}

func (s *entrySuite) TestReplaceOption(c *C) {
	ent1 := osutil.MountEntry{
		Dir:     "$HOME/.local/share",
		Options: []string{"x-snapd.kind=ensure-dir", "x-snapd.must-exist-dir=$HOME"},
	}
	osutil.ReplaceMountEntryOption(&ent1, osutil.XSnapdMustExistDir("/home/username"))
	c.Assert(ent1.String(), Equals, "none $HOME/.local/share none x-snapd.kind=ensure-dir,x-snapd.must-exist-dir=/home/username 0 0")

	ent2 := osutil.MountEntry{
		Dir:     "/usr/lib/lib4d.so.1.1.0",
		Options: []string{"x-snapd.kind=symlink", "x-snapd.symlink=/snap/snapname/165/graphics/usr/lib/lib4d.so.1.1.0", "x-snapd.origin=layout"},
	}
	osutil.ReplaceMountEntryOption(&ent2, osutil.XSnapdSymlink("/snap/snapname/200/graphics/usr/lib/lib4d.so.1.1.0"))
	osutil.ReplaceMountEntryOption(&ent2, osutil.XSnapdKindEnsureDir())
	c.Assert(ent2.String(), Equals, "none /usr/lib/lib4d.so.1.1.0 none x-snapd.kind=ensure-dir,x-snapd.symlink=/snap/snapname/200/graphics/usr/lib/lib4d.so.1.1.0,x-snapd.origin=layout 0 0")

	ent3 := osutil.MountEntry{
		Dir:     "/usr/lib/lib4d.so.1.1.0",
		Options: []string{"x-snapd.kind=symlink", "x-snapd.symlink=/snap/snapname/165/graphics/usr/lib/lib4d.so.1.1.0", "x-snapd.origin=layout"},
	}
	osutil.ReplaceMountEntryOption(&ent3, "x-snapd.kind=")
	c.Assert(ent3.String(), Equals, "none /usr/lib/lib4d.so.1.1.0 none x-snapd.kind=symlink,x-snapd.symlink=/snap/snapname/165/graphics/usr/lib/lib4d.so.1.1.0,x-snapd.origin=layout 0 0")

	ent4 := osutil.MountEntry{
		Dir:     "/usr/lib/lib4d.so.1.1.0",
		Options: []string{"x-snapd.kind=symlink", "x-snapd.symlink=/snap/snapname/165/graphics/usr/lib/lib4d.so.1.1.0", "x-snapd.origin=layout"},
	}
	osutil.ReplaceMountEntryOption(&ent4, "x-snapd.kind")
	c.Assert(ent4.String(), Equals, "none /usr/lib/lib4d.so.1.1.0 none x-snapd.kind=symlink,x-snapd.symlink=/snap/snapname/165/graphics/usr/lib/lib4d.so.1.1.0,x-snapd.origin=layout 0 0")

	var ent5 *osutil.MountEntry
	osutil.ReplaceMountEntryOption(ent5, osutil.XSnapdMustExistDir("doNotPanic"))
}

func (s *entrySuite) TestEqual(c *C) {
	var a, b *osutil.MountEntry
	a = &osutil.MountEntry{}
	b = &osutil.MountEntry{}
	c.Assert(a.Equal(b), Equals, true)
	a = &osutil.MountEntry{Dir: "foo"}
	b = &osutil.MountEntry{Dir: "foo"}
	c.Assert(a.Equal(b), Equals, true)
	a = &osutil.MountEntry{Options: []string{"ro"}}
	b = &osutil.MountEntry{Options: []string{"ro"}}
	c.Assert(a.Equal(b), Equals, true)
	a = &osutil.MountEntry{Dir: "foo"}
	b = &osutil.MountEntry{Dir: "bar"}
	c.Assert(a.Equal(b), Equals, false)
	a = &osutil.MountEntry{}
	b = &osutil.MountEntry{Options: []string{"ro"}}
	c.Assert(a.Equal(b), Equals, false)
	a = &osutil.MountEntry{Options: []string{"ro"}}
	b = &osutil.MountEntry{Options: []string{"rw"}}
	c.Assert(a.Equal(b), Equals, false)
}

// Test that typical fstab entry is parsed correctly.
func (s *entrySuite) TestParseMountEntry1(c *C) {
	e, err := osutil.ParseMountEntry("UUID=394f32c0-1f94-4005-9717-f9ab4a4b570b /               ext4    errors=remount-ro 0       1")
	c.Assert(err, IsNil)
	c.Assert(e.Name, Equals, "UUID=394f32c0-1f94-4005-9717-f9ab4a4b570b")
	c.Assert(e.Dir, Equals, "/")
	c.Assert(e.Type, Equals, "ext4")
	c.Assert(e.Options, DeepEquals, []string{"errors=remount-ro"})
	c.Assert(e.DumpFrequency, Equals, 0)
	c.Assert(e.CheckPassNumber, Equals, 1)

	e, err = osutil.ParseMountEntry("none /tmp tmpfs")
	c.Assert(err, IsNil)
	c.Assert(e.Name, Equals, "none")
	c.Assert(e.Dir, Equals, "/tmp")
	c.Assert(e.Type, Equals, "tmpfs")
	c.Assert(e.Options, IsNil)
	c.Assert(e.DumpFrequency, Equals, 0)
	c.Assert(e.CheckPassNumber, Equals, 0)
}

// Test that hash inside a field value is supported.
func (s *entrySuite) TestHashInFieldValue(c *C) {
	e, err := osutil.ParseMountEntry("mhddfs#/mnt/dir1,/mnt/dir2 /mnt/dir fuse defaults,allow_other 0 0")
	c.Assert(err, IsNil)
	c.Assert(e.Name, Equals, "mhddfs#/mnt/dir1,/mnt/dir2")
	c.Assert(e.Dir, Equals, "/mnt/dir")
	c.Assert(e.Type, Equals, "fuse")
	c.Assert(e.Options, DeepEquals, []string{"defaults", "allow_other"})
	c.Assert(e.DumpFrequency, Equals, 0)
	c.Assert(e.CheckPassNumber, Equals, 0)
}

// Test that options are parsed correctly
func (s *entrySuite) TestParseMountEntry2(c *C) {
	e, err := osutil.ParseMountEntry("name dir type options,comma,separated 0 0")
	c.Assert(err, IsNil)
	c.Assert(e.Name, Equals, "name")
	c.Assert(e.Dir, Equals, "dir")
	c.Assert(e.Type, Equals, "type")
	c.Assert(e.Options, DeepEquals, []string{"options", "comma", "separated"})
	c.Assert(e.DumpFrequency, Equals, 0)
	c.Assert(e.CheckPassNumber, Equals, 0)
}

// Test that whitespace escape codes are honored
func (s *entrySuite) TestParseMountEntry3(c *C) {
	e, err := osutil.ParseMountEntry(`na\040me d\011ir ty\012pe optio\134ns 0 0`)
	c.Assert(err, IsNil)
	c.Assert(e.Name, Equals, "na me")
	c.Assert(e.Dir, Equals, "d\tir")
	c.Assert(e.Type, Equals, "ty\npe")
	c.Assert(e.Options, DeepEquals, []string{`optio\ns`})
	c.Assert(e.DumpFrequency, Equals, 0)
	c.Assert(e.CheckPassNumber, Equals, 0)
}

// Test that number of fields is checked
func (s *entrySuite) TestParseMountEntry4(c *C) {
	for _, s := range []string{
		"", "1", "1 2" /* skip 3, 4, 5 and 6 fields (valid case) */, "1 2 3 4 5 6 7",
	} {
		_, err := osutil.ParseMountEntry(s)
		c.Assert(err, ErrorMatches, "expected between 3 and 6 fields, found [01237]")
	}
}

// Test that integers are parsed and error checked
func (s *entrySuite) TestParseMountEntry5(c *C) {
	_, err := osutil.ParseMountEntry("name dir type options foo 0")
	c.Assert(err, ErrorMatches, "cannot parse dump frequency: .*")
	_, err = osutil.ParseMountEntry("name dir type options 0 foo")
	c.Assert(err, ErrorMatches, "cannot parse check pass number: .*")
}

// Test that last two integer fields default to zero if not present.
func (s *entrySuite) TestParseMountEntry6(c *C) {
	e, err := osutil.ParseMountEntry("name dir type options")
	c.Assert(err, IsNil)
	c.Assert(e.DumpFrequency, Equals, 0)
	c.Assert(e.CheckPassNumber, Equals, 0)

	e, err = osutil.ParseMountEntry("name dir type options 5")
	c.Assert(err, IsNil)
	c.Assert(e.DumpFrequency, Equals, 5)
	c.Assert(e.CheckPassNumber, Equals, 0)

	e, err = osutil.ParseMountEntry("name dir type options 5 7")
	c.Assert(err, IsNil)
	c.Assert(e.DumpFrequency, Equals, 5)
	c.Assert(e.CheckPassNumber, Equals, 7)
}

// Test that the typical ensure-dir fstab entry is parsed correctly.
func (s *entrySuite) TestParseMountEntryEnsureDir(c *C) {
	e, err := osutil.ParseMountEntry("none $HOME/.local/share none x-snapd.kind=ensure-dir,x-snapd.must-exist-dir=$HOME 0 0")
	c.Assert(err, IsNil)
	c.Assert(e.Name, Equals, "none")
	c.Assert(e.Dir, Equals, "$HOME/.local/share")
	c.Assert(e.Type, Equals, "none")
	c.Assert(e.Options, DeepEquals, []string{"x-snapd.kind=ensure-dir", "x-snapd.must-exist-dir=$HOME"})
	c.Assert(e.DumpFrequency, Equals, 0)
	c.Assert(e.CheckPassNumber, Equals, 0)
}

// Test (string) options -> (int) flag conversion code.
func (s *entrySuite) TestMountOptsToFlags(c *C) {
	flags, err := osutil.MountOptsToFlags(nil)
	c.Assert(err, IsNil)
	c.Assert(flags, Equals, 0)
	flags, err = osutil.MountOptsToFlags([]string{"ro", "nodev", "nosuid"})
	c.Assert(err, IsNil)
	c.Assert(flags, Equals, syscall.MS_RDONLY|syscall.MS_NODEV|syscall.MS_NOSUID)
	_, err = osutil.MountOptsToFlags([]string{"bogus"})
	c.Assert(err, ErrorMatches, `unsupported mount option: "bogus"`)
	// The x-snapd-prefix is reserved for non-kernel parameters that do not
	// translate to kernel level mount flags. This is similar to systemd or
	// udisks that use fstab options to convey additional data.
	flags, err = osutil.MountOptsToFlags([]string{"x-snapd.foo"})
	c.Assert(err, IsNil)
	c.Assert(flags, Equals, 0)
}

// Test (string) options -> (int, unparsed) flag conversion code.
func (s *entrySuite) TestMountOptsToCommonFlags(c *C) {
	flags, unparsed := osutil.MountOptsToCommonFlags(nil)
	c.Assert(flags, Equals, 0)
	c.Assert(unparsed, HasLen, 0)
	flags, unparsed = osutil.MountOptsToCommonFlags([]string{"ro", "nodev", "nosuid"})
	c.Assert(flags, Equals, syscall.MS_RDONLY|syscall.MS_NODEV|syscall.MS_NOSUID)
	c.Assert(unparsed, HasLen, 0)
	flags, unparsed = osutil.MountOptsToCommonFlags([]string{"bogus"})
	c.Assert(flags, Equals, 0)
	c.Assert(unparsed, DeepEquals, []string{"bogus"})
	// The x-snapd-prefix is reserved for non-kernel parameters that do not
	// translate to kernel level mount flags. This is similar to systemd or
	// udisks that use fstab options to convey additional data. Those are not
	// returned as "unparsed" as we don't want to pass them to the kernel.
	flags, unparsed = osutil.MountOptsToCommonFlags([]string{"x-snapd.foo"})
	c.Assert(flags, Equals, 0)
	c.Assert(unparsed, HasLen, 0)
	// The "rw" flag is recognized but doesn't translate to an actual value
	// since read-write is the implicit default and there are no kernel level
	// flags to express it.
	flags, unparsed = osutil.MountOptsToCommonFlags([]string{"rw"})
	c.Assert(flags, Equals, 0)
	c.Assert(unparsed, DeepEquals, []string(nil))
}

func (s *entrySuite) TestOptStr(c *C) {
	e := &osutil.MountEntry{Options: []string{"key=value"}}
	val, ok := e.OptStr("key")
	c.Assert(ok, Equals, true)
	c.Assert(val, Equals, "value")

	val, ok = e.OptStr("missing")
	c.Assert(ok, Equals, false)
	c.Assert(val, Equals, "")
}

func (s *entrySuite) TestOptBool(c *C) {
	e := &osutil.MountEntry{Options: []string{"key"}}
	val := e.OptBool("key")
	c.Assert(val, Equals, true)

	val = e.OptBool("missing")
	c.Assert(val, Equals, false)
}

func (s *entrySuite) TestOptionHelpers(c *C) {
	c.Assert(osutil.XSnapdUser(1000), Equals, "x-snapd.user=1000")
	c.Assert(osutil.XSnapdGroup(1000), Equals, "x-snapd.group=1000")
	c.Assert(osutil.XSnapdMode(0755), Equals, "x-snapd.mode=0755")
	c.Assert(osutil.XSnapdSymlink("oldname"), Equals, "x-snapd.symlink=oldname")
	c.Assert(osutil.XSnapdMustExistDir("$HOME"), Equals, "x-snapd.must-exist-dir=$HOME")
}

func (s *entrySuite) TestXSnapdMode(c *C) {
	// Mode has a default value.
	e := &osutil.MountEntry{}
	mode, err := e.XSnapdMode()
	c.Assert(err, IsNil)
	c.Assert(mode, Equals, os.FileMode(0755))

	// Mode is parsed from the x-snapd.mode= option.
	e = &osutil.MountEntry{Options: []string{"x-snapd.mode=0700"}}
	mode, err = e.XSnapdMode()
	c.Assert(err, IsNil)
	c.Assert(mode, Equals, os.FileMode(0700))

	// Empty value is invalid.
	e = &osutil.MountEntry{Options: []string{"x-snapd.mode="}}
	_, err = e.XSnapdMode()
	c.Assert(err, ErrorMatches, `cannot parse octal file mode from ""`)

	// As well as other bogus values.
	e = &osutil.MountEntry{Options: []string{"x-snapd.mode=pasta"}}
	_, err = e.XSnapdMode()
	c.Assert(err, ErrorMatches, `cannot parse octal file mode from "pasta"`)

	// And even valid values with trailing garbage.
	e = &osutil.MountEntry{Options: []string{"x-snapd.mode=0700pasta"}}
	mode, err = e.XSnapdMode()
	c.Assert(err, ErrorMatches, `cannot parse octal file mode from "0700pasta"`)
	c.Assert(mode, Equals, os.FileMode(0))
}

func (s *entrySuite) TestXSnapdUID(c *C) {
	// User has a default value.
	e := &osutil.MountEntry{}
	uid, err := e.XSnapdUID()
	c.Assert(err, IsNil)
	c.Assert(uid, Equals, uint64(0))

	// User is parsed from the x-snapd.uid= option.
	e = &osutil.MountEntry{Options: []string{"x-snapd.uid=root"}}
	uid, err = e.XSnapdUID()
	c.Assert(err, ErrorMatches, `cannot parse user name "root"`)
	c.Assert(uid, Equals, uint64(math.MaxUint64))

	// Numeric names are used as-is.
	e = &osutil.MountEntry{Options: []string{"x-snapd.uid=123"}}
	uid, err = e.XSnapdUID()
	c.Assert(err, IsNil)
	c.Assert(uid, Equals, uint64(123))

	// And even valid values with trailing garbage.
	e = &osutil.MountEntry{Options: []string{"x-snapd.uid=0bogus"}}
	uid, err = e.XSnapdUID()
	c.Assert(err, ErrorMatches, `cannot parse user name "0bogus"`)
	c.Assert(uid, Equals, uint64(math.MaxUint64))
}

func (s *entrySuite) TestXSnapdGID(c *C) {
	// Group has a default value.
	e := &osutil.MountEntry{}
	gid, err := e.XSnapdGID()
	c.Assert(err, IsNil)
	c.Assert(gid, Equals, uint64(0))

	e = &osutil.MountEntry{Options: []string{"x-snapd.gid=root"}}
	gid, err = e.XSnapdGID()
	c.Assert(err, ErrorMatches, `cannot parse group name "root"`)
	c.Assert(gid, Equals, uint64(math.MaxUint64))

	// Numeric names are used as-is.
	e = &osutil.MountEntry{Options: []string{"x-snapd.gid=456"}}
	gid, err = e.XSnapdGID()
	c.Assert(err, IsNil)
	c.Assert(gid, Equals, uint64(456))

	// And even valid values with trailing garbage.
	e = &osutil.MountEntry{Options: []string{"x-snapd.gid=0bogus"}}
	gid, err = e.XSnapdGID()
	c.Assert(err, ErrorMatches, `cannot parse group name "0bogus"`)
	c.Assert(gid, Equals, uint64(math.MaxUint64))
}

func (s *entrySuite) TestXSnapdEntryID(c *C) {
	// Entry ID is optional and defaults to the mount point.
	e := &osutil.MountEntry{Dir: "/foo"}
	c.Assert(e.XSnapdEntryID(), Equals, "/foo")

	// Entry ID is parsed from the x-snapd.id= option.
	e = &osutil.MountEntry{Dir: "/foo", Options: []string{"x-snapd.id=foo"}}
	c.Assert(e.XSnapdEntryID(), Equals, "foo")
}

func (s *entrySuite) TestXSnapdNeededBy(c *C) {
	// The needed-by attribute is optional.
	e := &osutil.MountEntry{}
	c.Assert(e.XSnapdNeededBy(), Equals, "")

	// The needed-by attribute parsed from the x-snapd.needed-by= option.
	e = &osutil.MountEntry{Options: []string{"x-snap.id=foo", "x-snapd.needed-by=bar"}}
	c.Assert(e.XSnapdNeededBy(), Equals, "bar")

	// There's a helper function that returns this option string.
	c.Assert(osutil.XSnapdNeededBy("foo"), Equals, "x-snapd.needed-by=foo")
}

func (s *entrySuite) TestXSnapdSynthetic(c *C) {
	// Entries are not synthetic unless tagged as such.
	e := &osutil.MountEntry{}
	c.Assert(e.XSnapdSynthetic(), Equals, false)

	// Tagging is done with x-snapd.synthetic option.
	e = &osutil.MountEntry{Options: []string{"x-snapd.synthetic"}}
	c.Assert(e.XSnapdSynthetic(), Equals, true)

	// There's a helper function that returns this option string.
	c.Assert(osutil.XSnapdSynthetic(), Equals, "x-snapd.synthetic")
}

func (s *entrySuite) TestXSnapdOrigin(c *C) {
	// Entries have no origin by default.
	e := &osutil.MountEntry{}
	c.Assert(e.XSnapdOrigin(), Equals, "")

	// Origin can be indicated with the x-snapd.origin= option.
	e = &osutil.MountEntry{Options: []string{osutil.XSnapdOriginLayout()}}
	c.Assert(e.XSnapdOrigin(), Equals, "layout")

	// There's a helper function that returns this option string.
	c.Assert(osutil.XSnapdOriginLayout(), Equals, "x-snapd.origin=layout")

	// Origin can also indicate a parallel snap instance setup
	e = &osutil.MountEntry{Options: []string{osutil.XSnapdOriginOvername()}}
	c.Assert(e.XSnapdOrigin(), Equals, "overname")
	c.Assert(osutil.XSnapdOriginOvername(), Equals, "x-snapd.origin=overname")
}

func (s *entrySuite) TestXSnapdDetach(c *C) {
	// Entries are not detached by default.
	e := &osutil.MountEntry{}
	c.Assert(e.XSnapdDetach(), Equals, false)

	// Detach can be requested with the x-snapd.detach option.
	e = &osutil.MountEntry{Options: []string{osutil.XSnapdDetach()}}
	c.Assert(e.XSnapdDetach(), Equals, true)

	// There's a helper function that returns this option string.
	c.Assert(osutil.XSnapdDetach(), Equals, "x-snapd.detach")
}

func (s *entrySuite) TestXSnapdKind(c *C) {
	// Entries have a kind (directory, file or symlink). Directory is spelled
	// as an empty string though, for backwards compatibility.
	e := &osutil.MountEntry{}
	c.Assert(e.XSnapdKind(), Equals, "")

	// A bind mount entry can refer to a file using the x-snapd.kind=file option string.
	e = &osutil.MountEntry{Options: []string{osutil.XSnapdKindFile()}}
	c.Assert(e.XSnapdKind(), Equals, "file")

	// There's a helper function that returns this option string.
	c.Assert(osutil.XSnapdKindFile(), Equals, "x-snapd.kind=file")

	// A mount entry can create a symlink by using the x-snapd.kind=symlink option string.
	e = &osutil.MountEntry{Options: []string{osutil.XSnapdKindSymlink()}}
	c.Assert(e.XSnapdKind(), Equals, "symlink")

	// There's a helper function that returns this option string.
	c.Assert(osutil.XSnapdKindSymlink(), Equals, "x-snapd.kind=symlink")

	// A mount entry can request creation of missing directories within the mount directory.
	e = &osutil.MountEntry{Options: []string{"x-snapd.kind=ensure-dir"}}
	c.Assert(e.XSnapdKind(), Equals, "ensure-dir")

	// There is a helper function that returns this option string.
	c.Assert(osutil.XSnapdKindEnsureDir(), Equals, "x-snapd.kind=ensure-dir")
}

func (s *entrySuite) TestXSnapdSymlink(c *C) {
	// Entries without the x-snapd.symlink key return an empty string
	e := &osutil.MountEntry{}
	c.Assert(e.XSnapdSymlink(), Equals, "")

	// A mount entry can list a symlink target
	e = &osutil.MountEntry{Options: []string{osutil.XSnapdSymlink("target")}}
	c.Assert(e.XSnapdSymlink(), Equals, "target")
}

func (s *entrySuite) TestXSnapdIgnoreMissing(c *C) {
	// By default entries will not have the ignore missing flag set
	e := &osutil.MountEntry{}
	c.Assert(e.XSnapdIgnoreMissing(), Equals, false)

	// A mount entry can specify that it should be ignored if the
	// mount source or target are missing with the
	// x-snapd.ignore-missing option.
	e = &osutil.MountEntry{Options: []string{osutil.XSnapdIgnoreMissing()}}
	c.Assert(e.XSnapdIgnoreMissing(), Equals, true)

	// There's a helper function that returns this option string.
	c.Assert(osutil.XSnapdIgnoreMissing(), Equals, "x-snapd.ignore-missing")
}

func (s *entrySuite) TestXSnapdMustExistDir(c *C) {
	// Entries without the x-snapd.must-exist-dir key return an empty string
	e := &osutil.MountEntry{}
	c.Assert(e.XSnapdMustExistDir(), Equals, "")

	// A mount entry can list a symlink target
	e = &osutil.MountEntry{Options: []string{osutil.XSnapdMustExistDir("$HOME")}}
	c.Assert(e.XSnapdMustExistDir(), Equals, "$HOME")
}
