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
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/mount"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
)

type ContentSuite struct {
	iface *builtin.ContentInterface
}

var _ = Suite(&ContentSuite{
	iface: &builtin.ContentInterface{},
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
	slot := &interfaces.Slot{SlotInfo: info.Slots["content-slot"]}
	err := s.iface.SanitizeSlot(slot)
	c.Assert(err, IsNil)
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
	slot := &interfaces.Slot{SlotInfo: info.Slots["content-slot"]}
	err := s.iface.SanitizeSlot(slot)
	c.Assert(err, IsNil)
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
	slot := &interfaces.Slot{SlotInfo: info.Slots["content-slot"]}
	err := s.iface.SanitizeSlot(slot)
	c.Assert(err, ErrorMatches, "read or write path must be set")
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
	slot := &interfaces.Slot{SlotInfo: info.Slots["content-slot"]}
	err := s.iface.SanitizeSlot(slot)
	c.Assert(err, ErrorMatches, "read or write path must be set")
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
		slot := &interfaces.Slot{SlotInfo: info.Slots["content-slot"]}
		err := s.iface.SanitizeSlot(slot)
		c.Assert(err, ErrorMatches, "content interface path is not clean:.*")
	}
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
	plug := &interfaces.Plug{PlugInfo: info.Plugs["content-plug"]}
	err := s.iface.SanitizePlug(plug)
	c.Assert(err, IsNil)
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
	plug := &interfaces.Plug{PlugInfo: info.Plugs["content-plug"]}
	err := s.iface.SanitizePlug(plug)
	c.Assert(err, IsNil)
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
	plug := &interfaces.Plug{PlugInfo: info.Plugs["content-plug"]}
	err := s.iface.SanitizePlug(plug)
	c.Assert(err, ErrorMatches, "content plug must contain target path")
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
	plug := &interfaces.Plug{PlugInfo: info.Plugs["content-plug"]}
	err := s.iface.SanitizePlug(plug)
	c.Assert(err, ErrorMatches, "content interface target path is not clean:.*")
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
	plug := &interfaces.Plug{PlugInfo: info.Plugs["content"]}
	err := s.iface.SanitizePlug(plug)
	c.Assert(err, ErrorMatches, "content plug must contain target path")
}

func (s *ContentSuite) TestResolveSpecialVariable(c *C) {
	info := snaptest.MockInfo(c, "name: name", &snap.SideInfo{Revision: snap.R(42)})
	c.Check(builtin.ResolveSpecialVariable("foo", info), Equals, "/snap/name/42/foo")
	c.Check(builtin.ResolveSpecialVariable("$SNAP/foo", info), Equals, "/snap/name/42/foo")
	c.Check(builtin.ResolveSpecialVariable("$SNAP_DATA/foo", info), Equals, "/var/snap/name/42/foo")
	c.Check(builtin.ResolveSpecialVariable("$SNAP_COMMON/foo", info), Equals, "/var/snap/name/common/foo")
	c.Check(builtin.ResolveSpecialVariable("$SNAP", info), Equals, "/snap/name/42")
	c.Check(builtin.ResolveSpecialVariable("$SNAP_DATA", info), Equals, "/var/snap/name/42")
	c.Check(builtin.ResolveSpecialVariable("$SNAP_COMMON", info), Equals, "/var/snap/name/common")
}

// Check that legacy syntax works and allows sharing read-only snap content
func (s *ContentSuite) TestConnectedPlugSnippetSharingLegacy(c *C) {
	const consumerYaml = `name: consumer 
plugs:
 content:
  target: import
`
	consumerInfo := snaptest.MockInfo(c, consumerYaml, &snap.SideInfo{Revision: snap.R(7)})
	plug := &interfaces.Plug{PlugInfo: consumerInfo.Plugs["content"]}
	const producerYaml = `name: producer
slots:
 content:
  read:
   - export
`
	producerInfo := snaptest.MockInfo(c, producerYaml, &snap.SideInfo{Revision: snap.R(5)})
	slot := &interfaces.Slot{SlotInfo: producerInfo.Slots["content"]}

	spec := &mount.Specification{}
	c.Assert(s.iface.MountConnectedPlug(spec, plug, nil, slot, nil), IsNil)
	expectedMnt := []mount.Entry{{
		Name:    "/snap/producer/5/export",
		Dir:     "/snap/consumer/7/import",
		Options: []string{"bind", "ro"},
	}}
	c.Assert(spec.MountEntries(), DeepEquals, expectedMnt)
}

// Check that sharing of read-only snap content is possible
func (s *ContentSuite) TestConnectedPlugSnippetSharingSnap(c *C) {
	const consumerYaml = `name: consumer 
plugs:
 content:
  target: $SNAP/import
apps:
 app:
  command: foo
`
	consumerInfo := snaptest.MockInfo(c, consumerYaml, &snap.SideInfo{Revision: snap.R(7)})
	plug := &interfaces.Plug{PlugInfo: consumerInfo.Plugs["content"]}
	const producerYaml = `name: producer
slots:
 content:
  read:
   - $SNAP/export
`
	producerInfo := snaptest.MockInfo(c, producerYaml, &snap.SideInfo{Revision: snap.R(5)})
	slot := &interfaces.Slot{SlotInfo: producerInfo.Slots["content"]}

	spec := &mount.Specification{}
	c.Assert(s.iface.MountConnectedPlug(spec, plug, nil, slot, nil), IsNil)
	expectedMnt := []mount.Entry{{
		Name:    "/snap/producer/5/export",
		Dir:     "/snap/consumer/7/import",
		Options: []string{"bind", "ro"},
	}}
	c.Assert(spec.MountEntries(), DeepEquals, expectedMnt)

	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, plug, nil, slot, nil)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	expected := `
# In addition to the bind mount, add any AppArmor rules so that
# snaps may directly access the slot implementation's files
# read-only.
/snap/producer/5/export/** mrkix,
`
	c.Assert(apparmorSpec.SnippetForTag("snap.consumer.app"), Equals, expected)
}

// Check that sharing of writable data is possible
func (s *ContentSuite) TestConnectedPlugSnippetSharingSnapData(c *C) {
	const consumerYaml = `name: consumer 
plugs:
 content:
  target: $SNAP_DATA/import
apps:
 app:
  command: foo
`
	consumerInfo := snaptest.MockInfo(c, consumerYaml, &snap.SideInfo{Revision: snap.R(7)})
	plug := &interfaces.Plug{PlugInfo: consumerInfo.Plugs["content"]}
	const producerYaml = `name: producer
slots:
 content:
  write:
   - $SNAP_DATA/export
`
	producerInfo := snaptest.MockInfo(c, producerYaml, &snap.SideInfo{Revision: snap.R(5)})
	slot := &interfaces.Slot{SlotInfo: producerInfo.Slots["content"]}

	spec := &mount.Specification{}
	c.Assert(s.iface.MountConnectedPlug(spec, plug, nil, slot, nil), IsNil)
	expectedMnt := []mount.Entry{{
		Name:    "/var/snap/producer/5/export",
		Dir:     "/var/snap/consumer/7/import",
		Options: []string{"bind"},
	}}
	c.Assert(spec.MountEntries(), DeepEquals, expectedMnt)

	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, plug, nil, slot, nil)
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
}

// Check that sharing of writable common data is possible
func (s *ContentSuite) TestConnectedPlugSnippetSharingSnapCommon(c *C) {
	const consumerYaml = `name: consumer 
plugs:
 content:
  target: $SNAP_COMMON/import
apps:
 app:
  command: foo
`
	consumerInfo := snaptest.MockInfo(c, consumerYaml, &snap.SideInfo{Revision: snap.R(7)})
	plug := &interfaces.Plug{PlugInfo: consumerInfo.Plugs["content"]}
	const producerYaml = `name: producer
slots:
 content:
  write:
   - $SNAP_COMMON/export
`
	producerInfo := snaptest.MockInfo(c, producerYaml, &snap.SideInfo{Revision: snap.R(5)})
	slot := &interfaces.Slot{SlotInfo: producerInfo.Slots["content"]}

	spec := &mount.Specification{}
	c.Assert(s.iface.MountConnectedPlug(spec, plug, nil, slot, nil), IsNil)
	expectedMnt := []mount.Entry{{
		Name:    "/var/snap/producer/common/export",
		Dir:     "/var/snap/consumer/common/import",
		Options: []string{"bind"},
	}}
	c.Assert(spec.MountEntries(), DeepEquals, expectedMnt)

	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, plug, nil, slot, nil)
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
}
