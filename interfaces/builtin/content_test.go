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
	"fmt"
	"path/filepath"

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
	c.Assert(interfaces.BeforePreparePlug(s.iface, plug), ErrorMatches, "content interface target path is not clean:.*")
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

func (s *ContentSuite) TestResolveSpecialVariable(c *C) {
	info := snaptest.MockInfo(c, "{name: name, version: 0}", &snap.SideInfo{Revision: snap.R(42)})
	c.Check(builtin.ResolveSpecialVariable("foo", info), Equals, filepath.Join(dirs.CoreSnapMountDir, "name/42/foo"))
	c.Check(builtin.ResolveSpecialVariable("$SNAP/foo", info), Equals, filepath.Join(dirs.CoreSnapMountDir, "name/42/foo"))
	c.Check(builtin.ResolveSpecialVariable("$SNAP_DATA/foo", info), Equals, "/var/snap/name/42/foo")
	c.Check(builtin.ResolveSpecialVariable("$SNAP_COMMON/foo", info), Equals, "/var/snap/name/common/foo")
	c.Check(builtin.ResolveSpecialVariable("$SNAP", info), Equals, filepath.Join(dirs.CoreSnapMountDir, "name/42"))
	c.Check(builtin.ResolveSpecialVariable("$SNAP_DATA", info), Equals, "/var/snap/name/42")
	c.Check(builtin.ResolveSpecialVariable("$SNAP_COMMON", info), Equals, "/var/snap/name/common")
	c.Check(builtin.ResolveSpecialVariable("$SNAP//", info), Equals, filepath.Join(dirs.CoreSnapMountDir, "name/42")+"//")
	c.Check(builtin.ResolveSpecialVariable("$SNAP_DATA/", info), Equals, "/var/snap/name/42/")
	c.Check(builtin.ResolveSpecialVariable("$PRUNE/bar", info), Equals, "/snap/name/42/$PRUNE/bar")
	c.Check(builtin.ResolveSpecialVariable("foo/snap/$SNAP/bar", info), Equals, "/snap/name/42/foo/snap/$SNAP/bar")
	c.Check(builtin.ResolveSpecialVariable("$SNAP/foo/snap/$SNAP/bar", info), Equals, "/snap/name/42/foo/snap/$SNAP/bar")
	c.Check(builtin.ResolveSpecialVariable("$SNAP_DATA//", info), Equals, "/var/snap/name/42//")
}

// Check that legacy syntax works and allows sharing read-only snap content
func (s *ContentSuite) TestConnectedPlugSnippetSharingLegacy(c *C) {
	const consumerYaml = `name: consumer
version: 0
plugs:
 content:
  target: import
`
	consumerInfo := snaptest.MockInfo(c, consumerYaml, &snap.SideInfo{Revision: snap.R(7)})
	plug := interfaces.NewConnectedPlug(consumerInfo.Plugs["content"], nil)
	const producerYaml = `name: producer
version: 0
slots:
 content:
  read:
   - export
`
	producerInfo := snaptest.MockInfo(c, producerYaml, &snap.SideInfo{Revision: snap.R(5)})
	slot := interfaces.NewConnectedSlot(producerInfo.Slots["content"], nil)

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
	consumerInfo := snaptest.MockInfo(c, consumerYaml, &snap.SideInfo{Revision: snap.R(7)})
	plug := interfaces.NewConnectedPlug(consumerInfo.Plugs["content"], nil)
	const producerYaml = `name: producer
version: 0
slots:
 content:
  read:
   - $SNAP/export
`
	producerInfo := snaptest.MockInfo(c, producerYaml, &snap.SideInfo{Revision: snap.R(5)})
	slot := interfaces.NewConnectedSlot(producerInfo.Slots["content"], nil)

	spec := &mount.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, plug, slot), IsNil)
	expectedMnt := []osutil.MountEntry{{
		Name:    filepath.Join(dirs.CoreSnapMountDir, "producer/5/export"),
		Dir:     filepath.Join(dirs.CoreSnapMountDir, "consumer/7/import"),
		Options: []string{"bind", "ro"},
	}}
	c.Assert(spec.MountEntries(), DeepEquals, expectedMnt)

	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, plug, slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	expected := `
# In addition to the bind mount, add any AppArmor rules so that
# snaps may directly access the slot implementation's files
# read-only.
/snap/producer/5/export/** mrkix,
`
	c.Assert(apparmorSpec.SnippetForTag("snap.consumer.app"), Equals, expected)

	updateNS := apparmorSpec.UpdateNS()
	profile0 := `  # Read-only content sharing consumer:content -> producer:content (r#0)
  mount options=(bind) /snap/producer/5/export/ -> /snap/consumer/7/import/,
  remount options=(bind, ro) /snap/consumer/7/import/,
  umount /snap/consumer/7/import/,
  # Writable mimic /snap/producer/5
  mount options=(rbind, rw) /snap/producer/5/ -> /tmp/.snap/snap/producer/5/,
  mount fstype=tmpfs options=(rw) tmpfs -> /snap/producer/5/,
  mount options=(rbind, rw) /tmp/.snap/snap/producer/5/** -> /snap/producer/5/**,
  mount options=(bind, rw) /tmp/.snap/snap/producer/5/* -> /snap/producer/5/*,
  umount /tmp/.snap/snap/producer/5/,
  umount /snap/producer/5{,/**},
  /snap/producer/5/** rw,
  /snap/producer/5/ rw,
  /snap/producer/ rw,
  /tmp/.snap/snap/producer/5/** rw,
  /tmp/.snap/snap/producer/5/ rw,
  /tmp/.snap/snap/producer/ rw,
  /tmp/.snap/snap/ rw,
  /tmp/.snap/ rw,
  # Writable mimic /snap/consumer/7
  mount options=(rbind, rw) /snap/consumer/7/ -> /tmp/.snap/snap/consumer/7/,
  mount fstype=tmpfs options=(rw) tmpfs -> /snap/consumer/7/,
  mount options=(rbind, rw) /tmp/.snap/snap/consumer/7/** -> /snap/consumer/7/**,
  mount options=(bind, rw) /tmp/.snap/snap/consumer/7/* -> /snap/consumer/7/*,
  umount /tmp/.snap/snap/consumer/7/,
  umount /snap/consumer/7{,/**},
  /snap/consumer/7/** rw,
  /snap/consumer/7/ rw,
  /snap/consumer/ rw,
  /tmp/.snap/snap/consumer/7/** rw,
  /tmp/.snap/snap/consumer/7/ rw,
  /tmp/.snap/snap/consumer/ rw,
  /tmp/.snap/snap/ rw,
  /tmp/.snap/ rw,
`
	c.Assert(updateNS[0], Equals, profile0)
	c.Assert(updateNS, DeepEquals, []string{profile0})
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
	consumerInfo := snaptest.MockInfo(c, consumerYaml, &snap.SideInfo{Revision: snap.R(7)})
	plug := interfaces.NewConnectedPlug(consumerInfo.Plugs["content"], nil)
	const producerYaml = `name: producer
version: 0
slots:
 content:
  write:
   - $SNAP_DATA/export
`
	producerInfo := snaptest.MockInfo(c, producerYaml, &snap.SideInfo{Revision: snap.R(5)})
	slot := interfaces.NewConnectedSlot(producerInfo.Slots["content"], nil)

	spec := &mount.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, plug, slot), IsNil)
	expectedMnt := []osutil.MountEntry{{
		Name:    "/var/snap/producer/5/export",
		Dir:     "/var/snap/consumer/7/import",
		Options: []string{"bind"},
	}}
	c.Assert(spec.MountEntries(), DeepEquals, expectedMnt)

	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, plug, slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	expected := `
# In addition to the bind mount, add any AppArmor rules so that
# snaps may directly access the slot implementation's files. Due
# to a limitation in the kernel's LSM hooks for AF_UNIX, these
# are needed for using named sockets within the exported
# directory.
/var/snap/producer/5/export/** mrwklix,
`
	c.Assert(apparmorSpec.SnippetForTag("snap.consumer.app"), Equals, expected)

	updateNS := apparmorSpec.UpdateNS()
	profile0 := `  # Read-write content sharing consumer:content -> producer:content (w#0)
  mount options=(bind, rw) /var/snap/producer/5/export/ -> /var/snap/consumer/7/import/,
  umount /var/snap/consumer/7/import/,
  # Writable directory /var/snap/producer/5/export
  /var/snap/producer/5/export/ rw,
  /var/snap/producer/5/ rw,
  /var/snap/producer/ rw,
  # Writable directory /var/snap/consumer/7/import
  /var/snap/consumer/7/import/ rw,
  /var/snap/consumer/7/ rw,
  /var/snap/consumer/ rw,
`
	c.Assert(updateNS[0], Equals, profile0)
	c.Assert(updateNS, DeepEquals, []string{profile0})
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
	consumerInfo := snaptest.MockInfo(c, consumerYaml, &snap.SideInfo{Revision: snap.R(7)})
	plug := interfaces.NewConnectedPlug(consumerInfo.Plugs["content"], nil)
	const producerYaml = `name: producer
version: 0
slots:
 content:
  write:
   - $SNAP_COMMON/export
`
	producerInfo := snaptest.MockInfo(c, producerYaml, &snap.SideInfo{Revision: snap.R(5)})
	slot := interfaces.NewConnectedSlot(producerInfo.Slots["content"], nil)

	spec := &mount.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, plug, slot), IsNil)
	expectedMnt := []osutil.MountEntry{{
		Name:    "/var/snap/producer/common/export",
		Dir:     "/var/snap/consumer/common/import",
		Options: []string{"bind"},
	}}
	c.Assert(spec.MountEntries(), DeepEquals, expectedMnt)

	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, plug, slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	expected := `
# In addition to the bind mount, add any AppArmor rules so that
# snaps may directly access the slot implementation's files. Due
# to a limitation in the kernel's LSM hooks for AF_UNIX, these
# are needed for using named sockets within the exported
# directory.
/var/snap/producer/common/export/** mrwklix,
`
	c.Assert(apparmorSpec.SnippetForTag("snap.consumer.app"), Equals, expected)

	updateNS := apparmorSpec.UpdateNS()
	profile0 := `  # Read-write content sharing consumer:content -> producer:content (w#0)
  mount options=(bind, rw) /var/snap/producer/common/export/ -> /var/snap/consumer/common/import/,
  umount /var/snap/consumer/common/import/,
  # Writable directory /var/snap/producer/common/export
  /var/snap/producer/common/export/ rw,
  /var/snap/producer/common/ rw,
  /var/snap/producer/ rw,
  # Writable directory /var/snap/consumer/common/import
  /var/snap/consumer/common/import/ rw,
  /var/snap/consumer/common/ rw,
  /var/snap/consumer/ rw,
`
	c.Assert(updateNS[0], Equals, profile0)
	c.Assert(updateNS, DeepEquals, []string{profile0})
}

func (s *ContentSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}

func (s *ContentSuite) TestModernContentInterface(c *C) {
	plug := MockPlug(c, `name: consumer
version: 0
plugs:
 content:
  target: $SNAP_COMMON/import
apps:
 app:
  command: foo
`, &snap.SideInfo{Revision: snap.R(1)}, "content")
	connectedPlug := interfaces.NewConnectedPlug(plug, nil)

	slot := MockSlot(c, `name: producer
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
`, &snap.SideInfo{Revision: snap.R(2)}, "content")
	connectedSlot := interfaces.NewConnectedSlot(slot, nil)

	// Create the mount and apparmor specifications.
	mountSpec := &mount.Specification{}
	c.Assert(mountSpec.AddConnectedPlug(s.iface, connectedPlug, connectedSlot), IsNil)
	apparmorSpec := &apparmor.Specification{}
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
/var/snap/producer/common/write-common/** mrwklix,
/var/snap/producer/2/write-data/** mrwklix,

# In addition to the bind mount, add any AppArmor rules so that
# snaps may directly access the slot implementation's files
# read-only.
/var/snap/producer/common/read-common/** mrkix,
/var/snap/producer/2/read-data/** mrkix,
/snap/producer/2/read-snap/** mrkix,
`
	c.Assert(apparmorSpec.SnippetForTag("snap.consumer.app"), Equals, expected)
	fmt.Printf("")
	updateNS := apparmorSpec.UpdateNS()
	profile0 := `  # Read-write content sharing consumer:content -> producer:content (w#0)
  mount options=(bind, rw) /var/snap/producer/common/write-common/ -> /var/snap/consumer/common/import/write-common/,
  umount /var/snap/consumer/common/import/write-common/,
  # Writable directory /var/snap/producer/common/write-common
  /var/snap/producer/common/write-common/ rw,
  /var/snap/producer/common/ rw,
  /var/snap/producer/ rw,
  # Writable directory /var/snap/consumer/common/import/write-common
  /var/snap/consumer/common/import/write-common/ rw,
  /var/snap/consumer/common/import/ rw,
  /var/snap/consumer/common/ rw,
  /var/snap/consumer/ rw,
`
	c.Assert(updateNS[0], Equals, profile0)

	profile1 := `  # Read-write content sharing consumer:content -> producer:content (w#1)
  mount options=(bind, rw) /var/snap/producer/2/write-data/ -> /var/snap/consumer/common/import/write-data/,
  umount /var/snap/consumer/common/import/write-data/,
  # Writable directory /var/snap/producer/2/write-data
  /var/snap/producer/2/write-data/ rw,
  /var/snap/producer/2/ rw,
  /var/snap/producer/ rw,
  # Writable directory /var/snap/consumer/common/import/write-data
  /var/snap/consumer/common/import/write-data/ rw,
  /var/snap/consumer/common/import/ rw,
  /var/snap/consumer/common/ rw,
  /var/snap/consumer/ rw,
`
	c.Assert(updateNS[1], Equals, profile1)

	profile2 := `  # Read-only content sharing consumer:content -> producer:content (r#0)
  mount options=(bind) /var/snap/producer/common/read-common/ -> /var/snap/consumer/common/import/read-common/,
  remount options=(bind, ro) /var/snap/consumer/common/import/read-common/,
  umount /var/snap/consumer/common/import/read-common/,
  # Writable directory /var/snap/producer/common/read-common
  /var/snap/producer/common/read-common/ rw,
  /var/snap/producer/common/ rw,
  /var/snap/producer/ rw,
  # Writable directory /var/snap/consumer/common/import/read-common
  /var/snap/consumer/common/import/read-common/ rw,
  /var/snap/consumer/common/import/ rw,
  /var/snap/consumer/common/ rw,
  /var/snap/consumer/ rw,
`
	c.Assert(updateNS[2], Equals, profile2)

	profile3 := `  # Read-only content sharing consumer:content -> producer:content (r#1)
  mount options=(bind) /var/snap/producer/2/read-data/ -> /var/snap/consumer/common/import/read-data/,
  remount options=(bind, ro) /var/snap/consumer/common/import/read-data/,
  umount /var/snap/consumer/common/import/read-data/,
  # Writable directory /var/snap/producer/2/read-data
  /var/snap/producer/2/read-data/ rw,
  /var/snap/producer/2/ rw,
  /var/snap/producer/ rw,
  # Writable directory /var/snap/consumer/common/import/read-data
  /var/snap/consumer/common/import/read-data/ rw,
  /var/snap/consumer/common/import/ rw,
  /var/snap/consumer/common/ rw,
  /var/snap/consumer/ rw,
`
	c.Assert(updateNS[3], Equals, profile3)

	profile4 := `  # Read-only content sharing consumer:content -> producer:content (r#2)
  mount options=(bind) /snap/producer/2/read-snap/ -> /var/snap/consumer/common/import/read-snap/,
  remount options=(bind, ro) /var/snap/consumer/common/import/read-snap/,
  umount /var/snap/consumer/common/import/read-snap/,
  # Writable mimic /snap/producer/2
  mount options=(rbind, rw) /snap/producer/2/ -> /tmp/.snap/snap/producer/2/,
  mount fstype=tmpfs options=(rw) tmpfs -> /snap/producer/2/,
  mount options=(rbind, rw) /tmp/.snap/snap/producer/2/** -> /snap/producer/2/**,
  mount options=(bind, rw) /tmp/.snap/snap/producer/2/* -> /snap/producer/2/*,
  umount /tmp/.snap/snap/producer/2/,
  umount /snap/producer/2{,/**},
  /snap/producer/2/** rw,
  /snap/producer/2/ rw,
  /snap/producer/ rw,
  /tmp/.snap/snap/producer/2/** rw,
  /tmp/.snap/snap/producer/2/ rw,
  /tmp/.snap/snap/producer/ rw,
  /tmp/.snap/snap/ rw,
  /tmp/.snap/ rw,
  # Writable directory /var/snap/consumer/common/import/read-snap
  /var/snap/consumer/common/import/read-snap/ rw,
  /var/snap/consumer/common/import/ rw,
  /var/snap/consumer/common/ rw,
  /var/snap/consumer/ rw,
`
	c.Assert(updateNS[4], Equals, profile4)
	c.Assert(updateNS, DeepEquals, []string{profile0, profile1, profile2, profile3, profile4})
}

func (s *ContentSuite) TestModernContentInterfacePlugins(c *C) {
	// Define one app snap and two snaps plugin snaps.
	plug := MockPlug(c, `name: app
version: 0
plugs:
 plugins:
  interface: content
  content: plugin-for-app
  target: $SNAP/plugins
apps:
 app:
  command: foo

`, &snap.SideInfo{Revision: snap.R(1)}, "plugins")
	connectedPlug := interfaces.NewConnectedPlug(plug, nil)

	// XXX: realistically the plugin may be a single file and we don't support
	// those very well.
	slotOne := MockSlot(c, `name: plugin-one
version: 0
slots:
 plugin-for-app:
  interface: content
  source:
    read: [$SNAP/plugin]
`, &snap.SideInfo{Revision: snap.R(1)}, "plugin-for-app")
	connectedSlotOne := interfaces.NewConnectedSlot(slotOne, nil)

	slotTwo := MockSlot(c, `name: plugin-two
version: 0
slots:
 plugin-for-app:
  interface: content
  source:
    read: [$SNAP/plugin]
`, &snap.SideInfo{Revision: snap.R(1)}, "plugin-for-app")
	connectedSlotTwo := interfaces.NewConnectedSlot(slotTwo, nil)

	// Create the mount and apparmor specifications.
	mountSpec := &mount.Specification{}
	apparmorSpec := &apparmor.Specification{}
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
/snap/plugin-one/1/plugin/** mrkix,


# In addition to the bind mount, add any AppArmor rules so that
# snaps may directly access the slot implementation's files
# read-only.
/snap/plugin-two/1/plugin/** mrkix,
`
	c.Assert(apparmorSpec.SnippetForTag("snap.app.app"), Equals, expected)
}

func (s *ContentSuite) TestModernContentSameReadAndWriteClash(c *C) {
	plug := MockPlug(c, `name: consumer
version: 0
plugs:
 content:
  target: $SNAP_COMMON/import
apps:
 app:
  command: foo
`, &snap.SideInfo{Revision: snap.R(1)}, "content")
	connectedPlug := interfaces.NewConnectedPlug(plug, nil)

	slot := MockSlot(c, `name: producer
version: 0
slots:
 content:
  source:
    read:
     - $SNAP_DATA/directory
    write:
     - $SNAP_DATA/directory
`, &snap.SideInfo{Revision: snap.R(2)}, "content")
	connectedSlot := interfaces.NewConnectedSlot(slot, nil)

	// Create the mount and apparmor specifications.
	mountSpec := &mount.Specification{}
	c.Assert(mountSpec.AddConnectedPlug(s.iface, connectedPlug, connectedSlot), IsNil)
	apparmorSpec := &apparmor.Specification{}
	c.Assert(apparmorSpec.AddConnectedPlug(s.iface, connectedPlug, connectedSlot), IsNil)

	// Analyze the mount specification
	expectedMnt := []osutil.MountEntry{{
		Name:    "/var/snap/producer/2/directory",
		Dir:     "/var/snap/consumer/common/import/directory",
		Options: []string{"bind", "ro"},
	}, {
		Name:    "/var/snap/producer/2/directory",
		Dir:     "/var/snap/consumer/common/import/directory-2",
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
/var/snap/producer/2/directory/** mrwklix,

# In addition to the bind mount, add any AppArmor rules so that
# snaps may directly access the slot implementation's files
# read-only.
/var/snap/producer/2/directory/** mrkix,
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
	consumerInfo := snaptest.MockInfo(c, consumerYaml, &snap.SideInfo{Revision: snap.R(7)})
	plug := interfaces.NewConnectedPlug(consumerInfo.Plugs["content"], nil)
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
	producerInfo := snaptest.MockInfo(c, producerYaml, &snap.SideInfo{Revision: snap.R(5)})
	slot := interfaces.NewConnectedSlot(producerInfo.Slots["content"], nil)

	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedSlot(s.iface, plug, slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.producer.app"})
	expected := `
# When the content interface is writable, allow this slot
# implementation to access the slot's exported files at the plugging
# snap's mountpoint to accommodate software where the plugging app
# tells the slotting app about files to share.
/var/snap/consumer/common/import/** mrwklix,
`
	c.Assert(apparmorSpec.SnippetForTag("snap.producer.app"), Equals, expected)
}
