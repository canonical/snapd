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

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/snap/snaptest"
)

type ContentSuite struct {
	iface interfaces.Interface
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
  read:
   - shared/read
`
	info := snaptest.MockInfo(c, mockSnapYaml, nil)
	slot := &interfaces.Slot{SlotInfo: info.Slots["content-slot"]}
	err := s.iface.SanitizeSlot(slot)
	c.Assert(err, IsNil)
}

func (s *ContentSuite) TestSanitizeSlotNoPaths(c *C) {
	const mockSnapYaml = `name: content-slot-snap
version: 1.0
slots:
 content-slot:
  interface: content
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
  read: []
  write: []
`
	info := snaptest.MockInfo(c, mockSnapYaml, nil)
	slot := &interfaces.Slot{SlotInfo: info.Slots["content-slot"]}
	err := s.iface.SanitizeSlot(slot)
	c.Assert(err, ErrorMatches, "read or write path must be set")
}

func (s *ContentSuite) TestSanitizeSlotHasRealtivePath(c *C) {
	const mockSnapYaml = `name: content-slot-snap
version: 1.0
slots:
 content-slot:
  interface: content
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
  target: import
`
	info := snaptest.MockInfo(c, mockSnapYaml, nil)
	plug := &interfaces.Plug{PlugInfo: info.Plugs["content-plug"]}
	err := s.iface.SanitizePlug(plug)
	c.Assert(err, IsNil)
}

func (s *ContentSuite) TestSanitizePlugSimpleNoTarget(c *C) {
	const mockSnapYaml = `name: content-slot-snap
version: 1.0
plugs:
 content-plug:
  interface: content
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
  target: ../foo
`
	info := snaptest.MockInfo(c, mockSnapYaml, nil)
	plug := &interfaces.Plug{PlugInfo: info.Plugs["content-plug"]}
	err := s.iface.SanitizePlug(plug)
	c.Assert(err, ErrorMatches, "content interface target path is not clean:.*")
}

func (s *ContentSuite) TestConnectedPlugSnippetSimple(c *C) {
	const mockSnapYaml = `name: content-slot-snap
version: 1.0
slots:
 content-slot:
  interface: content
  read:
   - shared/read
  write:
   - shared/write
plugs:
 content-plug:
  interface: content
  target: import
`
	info := snaptest.MockInfo(c, mockSnapYaml, nil)
	slot := &interfaces.Slot{SlotInfo: info.Slots["content-slot"]}
	plug := &interfaces.Plug{PlugInfo: info.Plugs["content-plug"]}
	content, err := s.iface.ConnectedPlugSnippet(plug, slot, interfaces.SecurityMount)
	c.Assert(err, IsNil)

	expected := fmt.Sprintf(`%[1]s/content-slot-snap/unset/shared/read %[1]s/content-slot-snap/unset/import none bind,ro 0 0
%[1]s/content-slot-snap/unset/shared/write %[1]s/content-slot-snap/unset/import none bind 0 0
`, dirs.SnapMountDir)
	c.Assert(string(content), DeepEquals, expected)
}
