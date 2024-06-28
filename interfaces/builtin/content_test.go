// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package builtin_test

import (
	"path/filepath"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/mount"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type ContentSuite struct {
	iface interfaces.Interface
}

var _ = Suite(&ContentSuite{
	iface: builtin.MustInterface("content"),
})

func (s *ContentSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "content")
}

func (s *ContentSuite) TestSanitizeSlotSimple(c *C) {
	const mockSnapYaml = `name: content-slot-snap
version: 1.0
slots:
 content-slot:
  interface: content
  content: mycont
  read:
   - shared/read
`
	info := snaptest.MockInfo(c, mockSnapYaml, nil)
	slot := info.Slots["content-slot"]
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), IsNil)
}

func (s *ContentSuite) TestSanitizeSlotContentLabelDefault(c *C) {
	const mockSnapYaml = `name: content-slot-snap
version: 1.0
slots:
 content-slot:
  interface: content
  read:
   - shared/read
`
	info := snaptest.MockInfo(c, mockSnapYaml, nil)
	slot := info.Slots["content-slot"]
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), IsNil)
	c.Assert(slot.Attrs["content"], Equals, slot.Name)
}

func (s *ContentSuite) TestSanitizeSlotNoPaths(c *C) {
	const mockSnapYaml = `name: content-slot-snap
version: 1.0
slots:
 content-slot:
  interface: content
  content: mycont
`
	info := snaptest.MockInfo(c, mockSnapYaml, nil)
	slot := info.Slots["content-slot"]
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches, "read or write path must be set")
}

func (s *ContentSuite) TestSanitizeSlotEmptyPaths(c *C) {
	const mockSnapYaml = `name: content-slot-snap
version: 1.0
slots:
 content-slot:
  interface: content
  content: mycont
  read: []
  write: []
`
	info := snaptest.MockInfo(c, mockSnapYaml, nil)
	slot := info.Slots["content-slot"]
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches, "read or write path must be set")
}

func (s *ContentSuite) TestSanitizeSlotHasRelativePath(c *C) {
	const mockSnapYaml = `name: content-slot-snap
version: 1.0
slots:
 content-slot:
  interface: content
  content: mycont
`
	for _, rw := range []string{"read: [../foo]", "write: [../bar]"} {
		info := snaptest.MockInfo(c, mockSnapYaml+"  "+rw, nil)
		slot := info.Slots["content-slot"]
		c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches, "content interface path is not clean:.*")
	}
}

func (s *ContentSuite) TestSanitizeSlotSourceAndLegacy(c *C) {
	slot := MockSlot(c, `name: snap
version: 0
slots:
  content:
    source:
      write: [$SNAP_DATA/stuff]
    read: [$SNAP/shared]
`, nil, "content")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches, `move the "read" attribute into the "source" section`)
	slot = MockSlot(c, `name: snap
version: 0
slots:
  content:
    source:
      read: [$SNAP/shared]
    write: [$SNAP_DATA/stuff]
`, nil, "content")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches, `move the "write" attribute into the "source" section`)
}

func (s *ContentSuite) TestSanitizePlugSimple(c *C) {
	const mockSnapYaml = `name: content-slot-snap
version: 1.0
plugs:
 content-plug:
  interface: content
  content: mycont
  target: import
`
	info := snaptest.MockInfo(c, mockSnapYaml, nil)
	plug := info.Plugs["content-plug"]
	c.Assert(interfaces.BeforePreparePlug(s.iface, plug), IsNil)
}

func (s *ContentSuite) TestSanitizePlugContentLabelDefault(c *C) {
	const mockSnapYaml = `name: content-slot-snap
version: 1.0
plugs:
 content-plug:
  interface: content
  target: import
`
	info := snaptest.MockInfo(c, mockSnapYaml, nil)
	plug := info.Plugs["content-plug"]
	c.Assert(interfaces.BeforePreparePlug(s.iface, plug), IsNil)
	c.Assert(plug.Attrs["content"], Equals, plug.Name)
}

func (s *ContentSuite) TestSanitizePlugSimpleNoTarget(c *C) {
	const mockSnapYaml = `name: content-slot-snap
version: 1.0
plugs:
 content-plug:
  interface: content
  content: mycont
`
	info := snaptest.MockInfo(c, mockSnapYaml, nil)
	plug := info.Plugs["content-plug"]
	c.Assert(interfaces.BeforePreparePlug(s.iface, plug), ErrorMatches, "content plug must contain target path")
}

func (s *ContentSuite) TestSanitizePlugSimpleTargetRelative(c *C) {
	const mockSnapYaml = `name: content-slot-snap
version: 1.0
plugs:
 content-plug:
  interface: content
  content: mycont
  target: ../foo
`
	info := snaptest.MockInfo(c, mockSnapYaml, nil)
	plug := info.Plugs["content-plug"]
	c.Assert(interfaces.BeforePreparePlug(s.iface, plug), ErrorMatches, "content interface path is not clean:.*")
}

func (s *ContentSuite) TestSanitizePlugApparmorInterpretedChar(c *C) {
	const mockSnapYaml = `name: content-slot-snap
version: 1.0
plugs:
 content-plug:
  interface: content
  content: mycont
  target: foo"bar
`
	info := snaptest.MockInfo(c, mockSnapYaml, nil)
	plug := info.Plugs["content-plug"]
	c.Assert(interfaces.BeforePreparePlug(s.iface, plug), ErrorMatches,
		`content interface path is invalid: "foo\\"bar" contains a reserved apparmor char.*`)
}

func (s *ContentSuite) TestSanitizePlugNilAttrMap(c *C) {
	const mockSnapYaml = `name: content-slot-snap
version: 1.0
apps:
  foo:
    command: foo
    plugs: [content]
`
	info := snaptest.MockInfo(c, mockSnapYaml, nil)
	plug := info.Plugs["content"]
	c.Assert(interfaces.BeforePreparePlug(s.iface, plug), ErrorMatches, "content plug must contain target path")
}

func (s *ContentSuite) TestSanitizeSlotNilAttrMap(c *C) {
	const mockSnapYaml = `name: content-slot-snap
version: 1.0
apps:
  foo:
    command: foo
    slots: [content]
`
	info := snaptest.MockInfo(c, mockSnapYaml, nil)
	slot := info.Slots["content"]
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches, "read or write path must be set")
}

func (s *ContentSuite) TestSanitizeSlotApparmorInterpretedChar(c *C) {
	const mockSnapYaml = `name: content-slot-snap
version: 1.0
slots:
 content-plug:
  interface: content
  source:
   read: [$SNAP/shared]
   write: ["$SNAP_DATA/foo}bar"]
`
	info := snaptest.MockInfo(c, mockSnapYaml, nil)
	slot := info.Slots["content-plug"]
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		`content interface path is invalid: "\$SNAP_DATA/foo}bar" contains a reserved apparmor char.*`)
}

func (s *ContentSuite) TestResolveSpecialVariable(c *C) {
	info := snaptest.MockInfo(c, "{name: name, version: 0}", &snap.SideInfo{Revision: snap.R(42)})
	c.Check(builtin.ResolveSpecialVariable("$SNAP/foo", info), Equals, filepath.Join(dirs.CoreSnapMountDir, "name/42/foo"))
	c.Check(builtin.ResolveSpecialVariable("$SNAP_DATA/foo", info), Equals, "/var/snap/name/42/foo")
	c.Check(builtin.ResolveSpecialVariable("$SNAP_COMMON/foo", info), Equals, "/var/snap/name/common/foo")
	c.Check(builtin.ResolveSpecialVariable("$SNAP", info), Equals, filepath.Join(dirs.CoreSnapMountDir, "name/42"))
	c.Check(builtin.ResolveSpecialVariable("$SNAP_DATA", info), Equals, "/var/snap/name/42")
	c.Check(builtin.ResolveSpecialVariable("$SNAP_COMMON", info), Equals, "/var/snap/name/common")
	c.Check(builtin.ResolveSpecialVariable("$SNAP_DATA/", info), Equals, "/var/snap/name/42/")
	// automatically prefixed with $SNAP
	c.Check(builtin.ResolveSpecialVariable("foo", info), Equals, filepath.Join(dirs.CoreSnapMountDir, "name/42/foo"))
	c.Check(builtin.ResolveSpecialVariable("foo/snap/bar", info), Equals, "/snap/name/42/foo/snap/bar")
	// contain invalid variables
	c.Check(builtin.ResolveSpecialVariable("$PRUNE/bar", info), Equals, "/snap/name/42//bar")
	c.Check(builtin.ResolveSpecialVariable("bar/$PRUNE/foo", info), Equals, "/snap/name/42/bar//foo")
}

// Check that legacy syntax works and allows sharing read-only snap content
func (s *ContentSuite) TestConnectedPlugSnippetSharingLegacy(c *C) {
	const consumerYaml = `name: consumer
version: 0
plugs:
 content:
  target: import
`
	plug, _ := MockConnectedPlug(c, consumerYaml, &snap.SideInfo{Revision: snap.R(7)}, "content")
	const producerYaml = `name: producer
version: 0
slots:
 content:
  read:
   - export
`
	slot, _ := MockConnectedSlot(c, producerYaml, &snap.SideInfo{Revision: snap.R(5)}, "content")

	spec := &mount.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, plug, slot), IsNil)
	expectedMnt := []osutil.MountEntry{{
		Name:    filepath.Join(dirs.CoreSnapMountDir, "producer/5/export"),
		Dir:     filepath.Join(dirs.CoreSnapMountDir, "consumer/7/import"),
		Options: []string{"bind", "ro"},
	}}
	c.Assert(spec.MountEntries(), DeepEquals, expectedMnt)
}

// Check that sharing of read-only snap content is possible
func (s *ContentSuite) TestConnectedPlugSnippetSharingSnap(c *C) {
	const consumerYaml = `name: consumer
version: 0
plugs:
 content:
  target: $SNAP/import
apps:
 app:
  command: foo
`
	plug, _ := MockConnectedPlug(c, consumerYaml, &snap.SideInfo{Revision: snap.R(7)}, "content")
	const producerYaml = `name: producer
version: 0
slots:
 content:
  read:
   - $SNAP/export
`
	slot, _ := MockConnectedSlot(c, producerYaml, &snap.SideInfo{Revision: snap.R(5)}, "content")

	spec := &mount.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, plug, slot), IsNil)
	expectedMnt := []osutil.MountEntry{{
		Name:    filepath.Join(dirs.CoreSnapMountDir, "producer/5/export"),
		Dir:     filepath.Join(dirs.CoreSnapMountDir, "consumer/7/import"),
		Options: []string{"bind", "ro"},
	}}
	c.Assert(spec.MountEntries(), DeepEquals, expectedMnt)

	apparmorSpec := apparmor.NewSpecification(plug.AppSet())
	err := apparmorSpec.AddConnectedPlug(s.iface, plug, slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	expected := `
# In addition to the bind mount, add any AppArmor rules so that
# snaps may directly access the slot implementation's files
# read-only.
"/snap/producer/5/export/**" mrkix,
`
	c.Assert(apparmorSpec.SnippetForTag("snap.consumer.app"), Equals, expected)

	updateNS := apparmorSpec.UpdateNS()
	profile0 := `  # Read-only content sharing consumer:content -> producer:content (r#0)
  mount options=(bind) "/snap/producer/5/export/" -> "/snap/consumer/7/import{,-[0-9]*}/",
  remount options=(bind, ro) "/snap/consumer/7/import{,-[0-9]*}/",
  mount options=(rprivate) -> "/snap/consumer/7/import{,-[0-9]*}/",
  umount "/snap/consumer/7/import{,-[0-9]*}/",
  # Writable mimic /snap/producer/5
  # .. permissions for traversing the prefix that is assumed to exist
  # .. variant with mimic at /
  # Allow reading the mimic directory, it must exist in the first place.
  "/" r,
  # Allow setting the read-only directory aside via a bind mount.
  "/tmp/.snap/" rw,
  mount options=(rbind, rw) "/" -> "/tmp/.snap/",
  # Allow mounting tmpfs over the read-only directory.
  mount fstype=tmpfs options=(rw) tmpfs -> "/",
  # Allow creating empty files and directories for bind mounting things
  # to reconstruct the now-writable parent directory.
  "/tmp/.snap/*/" rw,
  "/*/" rw,
  mount options=(rbind, rw) "/tmp/.snap/*/" -> "/*/",
  "/tmp/.snap/*" rw,
  "/*" rw,
  mount options=(bind, rw) "/tmp/.snap/*" -> "/*",
  # Allow unmounting the auxiliary directory.
  # TODO: use fstype=tmpfs here for more strictness (LP: #1613403)
  mount options=(rprivate) -> "/tmp/.snap/",
  umount "/tmp/.snap/",
  # Allow unmounting the destination directory as well as anything
  # inside.  This lets us perform the undo plan in case the writable
  # mimic fails.
  mount options=(rprivate) -> "/",
  mount options=(rprivate) -> "/*",
  mount options=(rprivate) -> "/*/",
  umount "/",
  umount "/*",
  umount "/*/",
  # .. variant with mimic at /snap/
  "/snap/" r,
  "/tmp/.snap/snap/" rw,
  mount options=(rbind, rw) "/snap/" -> "/tmp/.snap/snap/",
  mount fstype=tmpfs options=(rw) tmpfs -> "/snap/",
  "/tmp/.snap/snap/*/" rw,
  "/snap/*/" rw,
  mount options=(rbind, rw) "/tmp/.snap/snap/*/" -> "/snap/*/",
  "/tmp/.snap/snap/*" rw,
  "/snap/*" rw,
  mount options=(bind, rw) "/tmp/.snap/snap/*" -> "/snap/*",
  mount options=(rprivate) -> "/tmp/.snap/snap/",
  umount "/tmp/.snap/snap/",
  mount options=(rprivate) -> "/snap/",
  mount options=(rprivate) -> "/snap/*",
  mount options=(rprivate) -> "/snap/*/",
  umount "/snap/",
  umount "/snap/*",
  umount "/snap/*/",
  # .. variant with mimic at /snap/producer/
  "/snap/producer/" r,
  "/tmp/.snap/snap/producer/" rw,
  mount options=(rbind, rw) "/snap/producer/" -> "/tmp/.snap/snap/producer/",
  mount fstype=tmpfs options=(rw) tmpfs -> "/snap/producer/",
  "/tmp/.snap/snap/producer/*/" rw,
  "/snap/producer/*/" rw,
  mount options=(rbind, rw) "/tmp/.snap/snap/producer/*/" -> "/snap/producer/*/",
  "/tmp/.snap/snap/producer/*" rw,
  "/snap/producer/*" rw,
  mount options=(bind, rw) "/tmp/.snap/snap/producer/*" -> "/snap/producer/*",
  mount options=(rprivate) -> "/tmp/.snap/snap/producer/",
  umount "/tmp/.snap/snap/producer/",
  mount options=(rprivate) -> "/snap/producer/",
  mount options=(rprivate) -> "/snap/producer/*",
  mount options=(rprivate) -> "/snap/producer/*/",
  umount "/snap/producer/",
  umount "/snap/producer/*",
  umount "/snap/producer/*/",
  # .. variant with mimic at /snap/producer/5/
  "/snap/producer/5/" r,
  "/tmp/.snap/snap/producer/5/" rw,
  mount options=(rbind, rw) "/snap/producer/5/" -> "/tmp/.snap/snap/producer/5/",
  mount fstype=tmpfs options=(rw) tmpfs -> "/snap/producer/5/",
  "/tmp/.snap/snap/producer/5/*/" rw,
  "/snap/producer/5/*/" rw,
  mount options=(rbind, rw) "/tmp/.snap/snap/producer/5/*/" -> "/snap/producer/5/*/",
  "/tmp/.snap/snap/producer/5/*" rw,
  "/snap/producer/5/*" rw,
  mount options=(bind, rw) "/tmp/.snap/snap/producer/5/*" -> "/snap/producer/5/*",
  mount options=(rprivate) -> "/tmp/.snap/snap/producer/5/",
  umount "/tmp/.snap/snap/producer/5/",
  mount options=(rprivate) -> "/snap/producer/5/",
  mount options=(rprivate) -> "/snap/producer/5/*",
  mount options=(rprivate) -> "/snap/producer/5/*/",
  umount "/snap/producer/5/",
  umount "/snap/producer/5/*",
  umount "/snap/producer/5/*/",
  # Writable mimic /snap/consumer/7
  # .. variant with mimic at /snap/consumer/
  "/snap/consumer/" r,
  "/tmp/.snap/snap/consumer/" rw,
  mount options=(rbind, rw) "/snap/consumer/" -> "/tmp/.snap/snap/consumer/",
  mount fstype=tmpfs options=(rw) tmpfs -> "/snap/consumer/",
  "/tmp/.snap/snap/consumer/*/" rw,
  "/snap/consumer/*/" rw,
  mount options=(rbind, rw) "/tmp/.snap/snap/consumer/*/" -> "/snap/consumer/*/",
  "/tmp/.snap/snap/consumer/*" rw,
  "/snap/consumer/*" rw,
  mount options=(bind, rw) "/tmp/.snap/snap/consumer/*" -> "/snap/consumer/*",
  mount options=(rprivate) -> "/tmp/.snap/snap/consumer/",
  umount "/tmp/.snap/snap/consumer/",
  mount options=(rprivate) -> "/snap/consumer/",
  mount options=(rprivate) -> "/snap/consumer/*",
  mount options=(rprivate) -> "/snap/consumer/*/",
  umount "/snap/consumer/",
  umount "/snap/consumer/*",
  umount "/snap/consumer/*/",
  # .. variant with mimic at /snap/consumer/7/
  "/snap/consumer/7/" r,
  "/tmp/.snap/snap/consumer/7/" rw,
  mount options=(rbind, rw) "/snap/consumer/7/" -> "/tmp/.snap/snap/consumer/7/",
  mount fstype=tmpfs options=(rw) tmpfs -> "/snap/consumer/7/",
  "/tmp/.snap/snap/consumer/7/*/" rw,
  "/snap/consumer/7/*/" rw,
  mount options=(rbind, rw) "/tmp/.snap/snap/consumer/7/*/" -> "/snap/consumer/7/*/",
  "/tmp/.snap/snap/consumer/7/*" rw,
  "/snap/consumer/7/*" rw,
  mount options=(bind, rw) "/tmp/.snap/snap/consumer/7/*" -> "/snap/consumer/7/*",
  mount options=(rprivate) -> "/tmp/.snap/snap/consumer/7/",
  umount "/tmp/.snap/snap/consumer/7/",
  mount options=(rprivate) -> "/snap/consumer/7/",
  mount options=(rprivate) -> "/snap/consumer/7/*",
  mount options=(rprivate) -> "/snap/consumer/7/*/",
  umount "/snap/consumer/7/",
  umount "/snap/consumer/7/*",
  umount "/snap/consumer/7/*/",
`
	c.Assert(strings.Join(updateNS[:], ""), Equals, profile0)
}

// Check that sharing of writable data is possible
func (s *ContentSuite) TestConnectedPlugSnippetSharingSnapData(c *C) {
	const consumerYaml = `name: consumer
version: 0
plugs:
 content:
  target: $SNAP_DATA/import
apps:
 app:
  command: foo
`
	plug, _ := MockConnectedPlug(c, consumerYaml, &snap.SideInfo{Revision: snap.R(7)}, "content")
	const producerYaml = `name: producer
version: 0
slots:
 content:
  write:
   - $SNAP_DATA/export
`
	slot, _ := MockConnectedSlot(c, producerYaml, &snap.SideInfo{Revision: snap.R(5)}, "content")

	spec := &mount.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, plug, slot), IsNil)
	expectedMnt := []osutil.MountEntry{{
		Name:    "/var/snap/producer/5/export",
		Dir:     "/var/snap/consumer/7/import",
		Options: []string{"bind"},
	}}
	c.Assert(spec.MountEntries(), DeepEquals, expectedMnt)

	apparmorSpec := apparmor.NewSpecification(plug.AppSet())
	err := apparmorSpec.AddConnectedPlug(s.iface, plug, slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	expected := `
# In addition to the bind mount, add any AppArmor rules so that
# snaps may directly access the slot implementation's files. Due
# to a limitation in the kernel's LSM hooks for AF_UNIX, these
# are needed for using named sockets within the exported
# directory.
"/var/snap/producer/5/export/**" mrwklix,
`
	c.Assert(apparmorSpec.SnippetForTag("snap.consumer.app"), Equals, expected)

	updateNS := apparmorSpec.UpdateNS()
	profile0 := `  # Read-write content sharing consumer:content -> producer:content (w#0)
  mount options=(bind, rw) "/var/snap/producer/5/export/" -> "/var/snap/consumer/7/import{,-[0-9]*}/",
  mount options=(rprivate) -> "/var/snap/consumer/7/import{,-[0-9]*}/",
  umount "/var/snap/consumer/7/import{,-[0-9]*}/",
  # Writable directory /var/snap/producer/5/export
  "/var/snap/producer/5/export/" rw,
  "/var/snap/producer/5/" rw,
  "/var/snap/producer/" rw,
  # Writable directory /var/snap/consumer/7/import
  "/var/snap/consumer/7/import/" rw,
  "/var/snap/consumer/7/" rw,
  "/var/snap/consumer/" rw,
  # Writable directory /var/snap/consumer/7/import-[0-9]*
  "/var/snap/consumer/7/import-[0-9]*/" rw,
`
	c.Assert(strings.Join(updateNS[:], ""), Equals, profile0)
}

// Check that sharing of writable common data is possible
func (s *ContentSuite) TestConnectedPlugSnippetSharingSnapCommon(c *C) {
	const consumerYaml = `name: consumer
version: 0
plugs:
 content:
  target: $SNAP_COMMON/import
apps:
 app:
  command: foo
`
	plug, _ := MockConnectedPlug(c, consumerYaml, &snap.SideInfo{Revision: snap.R(7)}, "content")
	const producerYaml = `name: producer
version: 0
slots:
 content:
  write:
   - $SNAP_COMMON/export
`
	slot, _ := MockConnectedSlot(c, producerYaml, &snap.SideInfo{Revision: snap.R(5)}, "content")

	spec := &mount.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, plug, slot), IsNil)
	expectedMnt := []osutil.MountEntry{{
		Name:    "/var/snap/producer/common/export",
		Dir:     "/var/snap/consumer/common/import",
		Options: []string{"bind"},
	}}
	c.Assert(spec.MountEntries(), DeepEquals, expectedMnt)

	apparmorSpec := apparmor.NewSpecification(plug.AppSet())
	err := apparmorSpec.AddConnectedPlug(s.iface, plug, slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	expected := `
# In addition to the bind mount, add any AppArmor rules so that
# snaps may directly access the slot implementation's files. Due
# to a limitation in the kernel's LSM hooks for AF_UNIX, these
# are needed for using named sockets within the exported
# directory.
"/var/snap/producer/common/export/**" mrwklix,
`
	c.Assert(apparmorSpec.SnippetForTag("snap.consumer.app"), Equals, expected)

	updateNS := apparmorSpec.UpdateNS()
	profile0 := `  # Read-write content sharing consumer:content -> producer:content (w#0)
  mount options=(bind, rw) "/var/snap/producer/common/export/" -> "/var/snap/consumer/common/import{,-[0-9]*}/",
  mount options=(rprivate) -> "/var/snap/consumer/common/import{,-[0-9]*}/",
  umount "/var/snap/consumer/common/import{,-[0-9]*}/",
  # Writable directory /var/snap/producer/common/export
  "/var/snap/producer/common/export/" rw,
  "/var/snap/producer/common/" rw,
  "/var/snap/producer/" rw,
  # Writable directory /var/snap/consumer/common/import
  "/var/snap/consumer/common/import/" rw,
  "/var/snap/consumer/common/" rw,
  "/var/snap/consumer/" rw,
  # Writable directory /var/snap/consumer/common/import-[0-9]*
  "/var/snap/consumer/common/import-[0-9]*/" rw,
`
	c.Assert(strings.Join(updateNS[:], ""), Equals, profile0)
}

func (s *ContentSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}

func (s *ContentSuite) TestModernContentInterface(c *C) {
	const consumerYaml = `name: consumer
version: 0
plugs:
 content:
  target: $SNAP_COMMON/import
apps:
 app:
  command: foo
`
	connectedPlug, _ := MockConnectedPlug(c, consumerYaml, &snap.SideInfo{Revision: snap.R(1)}, "content")

	const produerYaml = `name: producer
version: 0
slots:
 content:
  source:
   read:
    - $SNAP_COMMON/read-common
    - $SNAP_DATA/read-data
    - $SNAP/read-snap
   write:
    - $SNAP_COMMON/write-common
    - $SNAP_DATA/write-data
`
	connectedSlot, _ := MockConnectedSlot(c, produerYaml, &snap.SideInfo{Revision: snap.R(2)}, "content")

	// Create the mount and apparmor specifications.
	mountSpec := &mount.Specification{}
	c.Assert(mountSpec.AddConnectedPlug(s.iface, connectedPlug, connectedSlot), IsNil)

	apparmorSpec := apparmor.NewSpecification(connectedPlug.AppSet())
	c.Assert(apparmorSpec.AddConnectedPlug(s.iface, connectedPlug, connectedSlot), IsNil)

	// Analyze the mount specification.
	expectedMnt := []osutil.MountEntry{{
		Name:    "/var/snap/producer/common/read-common",
		Dir:     "/var/snap/consumer/common/import/read-common",
		Options: []string{"bind", "ro"},
	}, {
		Name:    "/var/snap/producer/2/read-data",
		Dir:     "/var/snap/consumer/common/import/read-data",
		Options: []string{"bind", "ro"},
	}, {
		Name:    "/snap/producer/2/read-snap",
		Dir:     "/var/snap/consumer/common/import/read-snap",
		Options: []string{"bind", "ro"},
	}, {
		Name:    "/var/snap/producer/common/write-common",
		Dir:     "/var/snap/consumer/common/import/write-common",
		Options: []string{"bind"},
	}, {
		Name:    "/var/snap/producer/2/write-data",
		Dir:     "/var/snap/consumer/common/import/write-data",
		Options: []string{"bind"},
	}}
	c.Assert(mountSpec.MountEntries(), DeepEquals, expectedMnt)

	// Analyze the apparmor specification.
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	expected := `
# In addition to the bind mount, add any AppArmor rules so that
# snaps may directly access the slot implementation's files. Due
# to a limitation in the kernel's LSM hooks for AF_UNIX, these
# are needed for using named sockets within the exported
# directory.
"/var/snap/producer/common/write-common/**" mrwklix,
"/var/snap/producer/2/write-data/**" mrwklix,

# In addition to the bind mount, add any AppArmor rules so that
# snaps may directly access the slot implementation's files
# read-only.
"/var/snap/producer/common/read-common/**" mrkix,
"/var/snap/producer/2/read-data/**" mrkix,
"/snap/producer/2/read-snap/**" mrkix,
`
	c.Assert(apparmorSpec.SnippetForTag("snap.consumer.app"), Equals, expected)

	updateNS := apparmorSpec.UpdateNS()
	profile0 := `  # Read-write content sharing consumer:content -> producer:content (w#0)
  mount options=(bind, rw) "/var/snap/producer/common/write-common/" -> "/var/snap/consumer/common/import/write-common{,-[0-9]*}/",
  mount options=(rprivate) -> "/var/snap/consumer/common/import/write-common{,-[0-9]*}/",
  umount "/var/snap/consumer/common/import/write-common{,-[0-9]*}/",
  # Writable directory /var/snap/producer/common/write-common
  "/var/snap/producer/common/write-common/" rw,
  "/var/snap/producer/common/" rw,
  "/var/snap/producer/" rw,
  # Writable directory /var/snap/consumer/common/import/write-common
  "/var/snap/consumer/common/import/write-common/" rw,
  "/var/snap/consumer/common/import/" rw,
  "/var/snap/consumer/common/" rw,
  "/var/snap/consumer/" rw,
  # Writable directory /var/snap/consumer/common/import/write-common-[0-9]*
  "/var/snap/consumer/common/import/write-common-[0-9]*/" rw,
`
	// Find the slice that describes profile0 by looking for the first unique
	// line of the next profile.
	start := 0
	end, _ := apparmorSpec.UpdateNSIndexOf("  # Read-write content sharing consumer:content -> producer:content (w#1)\n")
	c.Assert(strings.Join(updateNS[start:end], ""), Equals, profile0)

	profile1 := `  # Read-write content sharing consumer:content -> producer:content (w#1)
  mount options=(bind, rw) "/var/snap/producer/2/write-data/" -> "/var/snap/consumer/common/import/write-data{,-[0-9]*}/",
  mount options=(rprivate) -> "/var/snap/consumer/common/import/write-data{,-[0-9]*}/",
  umount "/var/snap/consumer/common/import/write-data{,-[0-9]*}/",
  # Writable directory /var/snap/producer/2/write-data
  "/var/snap/producer/2/write-data/" rw,
  "/var/snap/producer/2/" rw,
  # Writable directory /var/snap/consumer/common/import/write-data
  "/var/snap/consumer/common/import/write-data/" rw,
  # Writable directory /var/snap/consumer/common/import/write-data-[0-9]*
  "/var/snap/consumer/common/import/write-data-[0-9]*/" rw,
`
	// Find the slice that describes profile1 by looking for the first unique
	// line of the next profile.
	start = end
	end, _ = apparmorSpec.UpdateNSIndexOf("  # Read-only content sharing consumer:content -> producer:content (r#0)\n")
	c.Assert(strings.Join(updateNS[start:end], ""), Equals, profile1)

	profile2 := `  # Read-only content sharing consumer:content -> producer:content (r#0)
  mount options=(bind) "/var/snap/producer/common/read-common/" -> "/var/snap/consumer/common/import/read-common{,-[0-9]*}/",
  remount options=(bind, ro) "/var/snap/consumer/common/import/read-common{,-[0-9]*}/",
  mount options=(rprivate) -> "/var/snap/consumer/common/import/read-common{,-[0-9]*}/",
  umount "/var/snap/consumer/common/import/read-common{,-[0-9]*}/",
  # Writable directory /var/snap/producer/common/read-common
  "/var/snap/producer/common/read-common/" rw,
  # Writable directory /var/snap/consumer/common/import/read-common
  "/var/snap/consumer/common/import/read-common/" rw,
  # Writable directory /var/snap/consumer/common/import/read-common-[0-9]*
  "/var/snap/consumer/common/import/read-common-[0-9]*/" rw,
`
	// Find the slice that describes profile2 by looking for the first unique
	// line of the next profile.
	start = end
	end, _ = apparmorSpec.UpdateNSIndexOf("  # Read-only content sharing consumer:content -> producer:content (r#1)\n")
	c.Assert(strings.Join(updateNS[start:end], ""), Equals, profile2)

	profile3 := `  # Read-only content sharing consumer:content -> producer:content (r#1)
  mount options=(bind) "/var/snap/producer/2/read-data/" -> "/var/snap/consumer/common/import/read-data{,-[0-9]*}/",
  remount options=(bind, ro) "/var/snap/consumer/common/import/read-data{,-[0-9]*}/",
  mount options=(rprivate) -> "/var/snap/consumer/common/import/read-data{,-[0-9]*}/",
  umount "/var/snap/consumer/common/import/read-data{,-[0-9]*}/",
  # Writable directory /var/snap/producer/2/read-data
  "/var/snap/producer/2/read-data/" rw,
  # Writable directory /var/snap/consumer/common/import/read-data
  "/var/snap/consumer/common/import/read-data/" rw,
  # Writable directory /var/snap/consumer/common/import/read-data-[0-9]*
  "/var/snap/consumer/common/import/read-data-[0-9]*/" rw,
`
	// Find the slice that describes profile3 by looking for the first unique
	// line of the next profile.
	start = end
	end, _ = apparmorSpec.UpdateNSIndexOf("  # Read-only content sharing consumer:content -> producer:content (r#2)\n")
	c.Assert(strings.Join(updateNS[start:end], ""), Equals, profile3)

	profile4 := `  # Read-only content sharing consumer:content -> producer:content (r#2)
  mount options=(bind) "/snap/producer/2/read-snap/" -> "/var/snap/consumer/common/import/read-snap{,-[0-9]*}/",
  remount options=(bind, ro) "/var/snap/consumer/common/import/read-snap{,-[0-9]*}/",
  mount options=(rprivate) -> "/var/snap/consumer/common/import/read-snap{,-[0-9]*}/",
  umount "/var/snap/consumer/common/import/read-snap{,-[0-9]*}/",
  # Writable mimic /snap/producer/2
  # .. permissions for traversing the prefix that is assumed to exist
  # .. variant with mimic at /
  # Allow reading the mimic directory, it must exist in the first place.
  "/" r,
  # Allow setting the read-only directory aside via a bind mount.
  "/tmp/.snap/" rw,
  mount options=(rbind, rw) "/" -> "/tmp/.snap/",
  # Allow mounting tmpfs over the read-only directory.
  mount fstype=tmpfs options=(rw) tmpfs -> "/",
  # Allow creating empty files and directories for bind mounting things
  # to reconstruct the now-writable parent directory.
  "/tmp/.snap/*/" rw,
  "/*/" rw,
  mount options=(rbind, rw) "/tmp/.snap/*/" -> "/*/",
  "/tmp/.snap/*" rw,
  "/*" rw,
  mount options=(bind, rw) "/tmp/.snap/*" -> "/*",
  # Allow unmounting the auxiliary directory.
  # TODO: use fstype=tmpfs here for more strictness (LP: #1613403)
  mount options=(rprivate) -> "/tmp/.snap/",
  umount "/tmp/.snap/",
  # Allow unmounting the destination directory as well as anything
  # inside.  This lets us perform the undo plan in case the writable
  # mimic fails.
  mount options=(rprivate) -> "/",
  mount options=(rprivate) -> "/*",
  mount options=(rprivate) -> "/*/",
  umount "/",
  umount "/*",
  umount "/*/",
  # .. variant with mimic at /snap/
  "/snap/" r,
  "/tmp/.snap/snap/" rw,
  mount options=(rbind, rw) "/snap/" -> "/tmp/.snap/snap/",
  mount fstype=tmpfs options=(rw) tmpfs -> "/snap/",
  "/tmp/.snap/snap/*/" rw,
  "/snap/*/" rw,
  mount options=(rbind, rw) "/tmp/.snap/snap/*/" -> "/snap/*/",
  "/tmp/.snap/snap/*" rw,
  "/snap/*" rw,
  mount options=(bind, rw) "/tmp/.snap/snap/*" -> "/snap/*",
  mount options=(rprivate) -> "/tmp/.snap/snap/",
  umount "/tmp/.snap/snap/",
  mount options=(rprivate) -> "/snap/",
  mount options=(rprivate) -> "/snap/*",
  mount options=(rprivate) -> "/snap/*/",
  umount "/snap/",
  umount "/snap/*",
  umount "/snap/*/",
  # .. variant with mimic at /snap/producer/
  "/snap/producer/" r,
  "/tmp/.snap/snap/producer/" rw,
  mount options=(rbind, rw) "/snap/producer/" -> "/tmp/.snap/snap/producer/",
  mount fstype=tmpfs options=(rw) tmpfs -> "/snap/producer/",
  "/tmp/.snap/snap/producer/*/" rw,
  "/snap/producer/*/" rw,
  mount options=(rbind, rw) "/tmp/.snap/snap/producer/*/" -> "/snap/producer/*/",
  "/tmp/.snap/snap/producer/*" rw,
  "/snap/producer/*" rw,
  mount options=(bind, rw) "/tmp/.snap/snap/producer/*" -> "/snap/producer/*",
  mount options=(rprivate) -> "/tmp/.snap/snap/producer/",
  umount "/tmp/.snap/snap/producer/",
  mount options=(rprivate) -> "/snap/producer/",
  mount options=(rprivate) -> "/snap/producer/*",
  mount options=(rprivate) -> "/snap/producer/*/",
  umount "/snap/producer/",
  umount "/snap/producer/*",
  umount "/snap/producer/*/",
  # .. variant with mimic at /snap/producer/2/
  "/snap/producer/2/" r,
  "/tmp/.snap/snap/producer/2/" rw,
  mount options=(rbind, rw) "/snap/producer/2/" -> "/tmp/.snap/snap/producer/2/",
  mount fstype=tmpfs options=(rw) tmpfs -> "/snap/producer/2/",
  "/tmp/.snap/snap/producer/2/*/" rw,
  "/snap/producer/2/*/" rw,
  mount options=(rbind, rw) "/tmp/.snap/snap/producer/2/*/" -> "/snap/producer/2/*/",
  "/tmp/.snap/snap/producer/2/*" rw,
  "/snap/producer/2/*" rw,
  mount options=(bind, rw) "/tmp/.snap/snap/producer/2/*" -> "/snap/producer/2/*",
  mount options=(rprivate) -> "/tmp/.snap/snap/producer/2/",
  umount "/tmp/.snap/snap/producer/2/",
  mount options=(rprivate) -> "/snap/producer/2/",
  mount options=(rprivate) -> "/snap/producer/2/*",
  mount options=(rprivate) -> "/snap/producer/2/*/",
  umount "/snap/producer/2/",
  umount "/snap/producer/2/*",
  umount "/snap/producer/2/*/",
  # Writable directory /var/snap/consumer/common/import/read-snap
  "/var/snap/consumer/common/import/read-snap/" rw,
  # Writable directory /var/snap/consumer/common/import/read-snap-[0-9]*
  "/var/snap/consumer/common/import/read-snap-[0-9]*/" rw,
`
	// Find the slice that describes profile4 by looking till the end of the list.
	start = end
	c.Assert(strings.Join(updateNS[start:], ""), Equals, profile4)
	c.Assert(strings.Join(updateNS, ""), DeepEquals, strings.Join([]string{profile0, profile1, profile2, profile3, profile4}, ""))
}

func (s *ContentSuite) TestModernContentInterfacePlugins(c *C) {
	// Define one app snap and two snaps plugin snaps.
	const consumerYaml = `name: app
version: 0
plugs:
 plugins:
  interface: content
  content: plugin-for-app
  target: $SNAP/plugins
apps:
 app:
  command: foo
`
	connectedPlug, _ := MockConnectedPlug(c, consumerYaml, &snap.SideInfo{Revision: snap.R(1)}, "plugins")

	// XXX: realistically the plugin may be a single file and we don't support
	// those very well.
	const pluginOneYaml = `name: plugin-one
version: 0
slots:
 plugin-for-app:
  interface: content
  source:
   read: [$SNAP/plugin]
`
	connectedSlotOne, _ := MockConnectedSlot(c, pluginOneYaml, &snap.SideInfo{Revision: snap.R(1)}, "plugin-for-app")

	const pluginTwoYaml = `name: plugin-two
version: 0
slots:
 plugin-for-app:
  interface: content
  source:
   read: [$SNAP/plugin]
`
	connectedSlotTwo, _ := MockConnectedSlot(c, pluginTwoYaml, &snap.SideInfo{Revision: snap.R(1)}, "plugin-for-app")

	// Create the mount and apparmor specifications.
	mountSpec := &mount.Specification{}
	apparmorSpec := apparmor.NewSpecification(connectedPlug.AppSet())
	for _, connectedSlot := range []*interfaces.ConnectedSlot{connectedSlotOne, connectedSlotTwo} {
		c.Assert(mountSpec.AddConnectedPlug(s.iface, connectedPlug, connectedSlot), IsNil)
		c.Assert(apparmorSpec.AddConnectedPlug(s.iface, connectedPlug, connectedSlot), IsNil)
	}

	// Analyze the mount specification.
	expectedMnt := []osutil.MountEntry{{
		Name:    "/snap/plugin-one/1/plugin",
		Dir:     "/snap/app/1/plugins/plugin",
		Options: []string{"bind", "ro"},
	}, {
		Name:    "/snap/plugin-two/1/plugin",
		Dir:     "/snap/app/1/plugins/plugin-2",
		Options: []string{"bind", "ro"},
	}}
	c.Assert(mountSpec.MountEntries(), DeepEquals, expectedMnt)

	// Analyze the apparmor specification.
	//
	// NOTE: the paths below refer to the original locations and are *NOT*
	// altered like the mount entries above. This is intended. See the comment
	// below for explanation as to why those are necessary.
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.app.app"})
	expected := `
# In addition to the bind mount, add any AppArmor rules so that
# snaps may directly access the slot implementation's files
# read-only.
"/snap/plugin-one/1/plugin/**" mrkix,


# In addition to the bind mount, add any AppArmor rules so that
# snaps may directly access the slot implementation's files
# read-only.
"/snap/plugin-two/1/plugin/**" mrkix,
`
	c.Assert(apparmorSpec.SnippetForTag("snap.app.app"), Equals, expected)
}

func (s *ContentSuite) TestModernContentSameReadAndWriteClash(c *C) {
	const consumerYaml = `name: consumer
version: 0
plugs:
 content:
  target: $SNAP_COMMON/import
apps:
 app:
  command: foo
`
	connectedPlug, _ := MockConnectedPlug(c, consumerYaml, &snap.SideInfo{Revision: snap.R(1)}, "content")

	const producerYaml = `name: producer
version: 0
slots:
 content:
  source:
   read:
    - $SNAP_DATA/directory
   write:
    - $SNAP_DATA/directory
`
	connectedSlot, _ := MockConnectedSlot(c, producerYaml, &snap.SideInfo{Revision: snap.R(2)}, "content")

	// Create the mount and apparmor specifications.
	mountSpec := &mount.Specification{}
	c.Assert(mountSpec.AddConnectedPlug(s.iface, connectedPlug, connectedSlot), IsNil)
	apparmorSpec := apparmor.NewSpecification(connectedPlug.AppSet())
	c.Assert(apparmorSpec.AddConnectedPlug(s.iface, connectedPlug, connectedSlot), IsNil)

	// Analyze the mount specification
	expectedMnt := []osutil.MountEntry{{
		Name:    "/var/snap/producer/2/directory",
		Dir:     "/var/snap/consumer/common/import/directory",
		Options: []string{"bind"},
	}}
	c.Assert(mountSpec.MountEntries(), DeepEquals, expectedMnt)

	// Analyze the apparmor specification.
	//
	// NOTE: Although there are duplicate entries with different permissions
	// one is a superset of the other so they do not conflict.
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	expected := `
# In addition to the bind mount, add any AppArmor rules so that
# snaps may directly access the slot implementation's files. Due
# to a limitation in the kernel's LSM hooks for AF_UNIX, these
# are needed for using named sockets within the exported
# directory.
"/var/snap/producer/2/directory/**" mrwklix,

# In addition to the bind mount, add any AppArmor rules so that
# snaps may directly access the slot implementation's files
# read-only.
"/var/snap/producer/2/directory/**" mrkix,
`
	c.Assert(apparmorSpec.SnippetForTag("snap.consumer.app"), Equals, expected)
}

// Check that slot can access shared directory in plug's namespace
func (s *ContentSuite) TestSlotCanAccessConnectedPlugSharedDirectory(c *C) {
	const consumerYaml = `name: consumer
version: 0
plugs:
 content:
  target: $SNAP_COMMON/import
`
	plug, _ := MockConnectedPlug(c, consumerYaml, &snap.SideInfo{Revision: snap.R(7)}, "content")
	const producerYaml = `name: producer
version: 0
slots:
 content:
  write:
   - $SNAP_COMMON/export
apps:
  app:
    command: bar
`
	slot, _ := MockConnectedSlot(c, producerYaml, &snap.SideInfo{Revision: snap.R(5)}, "content")

	apparmorSpec := apparmor.NewSpecification(slot.AppSet())
	err := apparmorSpec.AddConnectedSlot(s.iface, plug, slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.producer.app"})
	expected := `
# When the content interface is writable, allow this slot
# implementation to access the slot's exported files at the plugging
# snap's mountpoint to accommodate software where the plugging app
# tells the slotting app about files to share.
"/var/snap/consumer/common/import/**" mrwklix,
`
	c.Assert(apparmorSpec.SnippetForTag("snap.producer.app"), Equals, expected)
}

func (s *ContentSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, false)
	c.Assert(si.ImplicitOnClassic, Equals, false)
	c.Assert(si.Summary, Equals, `allows sharing code and data with other snaps`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "content: $SLOT(content)")
	c.Assert(si.AffectsPlugOnRefresh, Equals, true)
}
