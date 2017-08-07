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

package builtin_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type WaylandInterfaceSuite struct {
	iface interfaces.Interface
	slot  *interfaces.Slot
	plug  *interfaces.Plug
}

const waylandMockPlugSnapInfoYaml = `name: other
version: 1.0
apps:
 app2:
  command: foo
  plugs: [wayland]
`

var _ = Suite(&WaylandInterfaceSuite{
	iface: builtin.MustInterface("wayland"),
})

func (s *WaylandInterfaceSuite) SetUpTest(c *C) {
	s.slot = &interfaces.Slot{
		SlotInfo: &snap.SlotInfo{
			Snap:      &snap.Info{SuggestedName: "core", Type: snap.TypeOS},
			Name:      "wayland",
			Interface: "wayland",
		},
	}
	plugSnap := snaptest.MockInfo(c, waylandMockPlugSnapInfoYaml, nil)
	s.plug = &interfaces.Plug{PlugInfo: plugSnap.Plugs["wayland"]}
}

func (s *WaylandInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "wayland")
}

func (s *WaylandInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(s.slot.Sanitize(s.iface), IsNil)
	slot := &interfaces.Slot{SlotInfo: &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "some-snap"},
		Name:      "wayland",
		Interface: "wayland",
	}}
	c.Assert(slot.Sanitize(s.iface), ErrorMatches,
		"wayland slots are reserved for the core snap")
}

func (s *WaylandInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(s.plug.Sanitize(s.iface), IsNil)
}

func (s *WaylandInterfaceSuite) TestUsedSecuritySystems(c *C) {
	// connected plugs have a non-nil security snippet for apparmor
	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, nil, s.slot, nil)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app2"})
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app2"), testutil.Contains, `owner /run/user/*/wayland-[0-9]* rw,`)
}

func (s *WaylandInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
