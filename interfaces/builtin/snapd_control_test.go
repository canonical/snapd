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
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type SnapdControlInterfaceSuite struct {
	iface interfaces.Interface
	slot  *interfaces.Slot
	plug  *interfaces.Plug
}

var _ = Suite(&SnapdControlInterfaceSuite{
	iface: builtin.MustInterface("snapd-control"),
})

func (s *SnapdControlInterfaceSuite) SetUpTest(c *C) {
	consumingSnapInfo := snaptest.MockInfo(c, `
name: other
apps:
 app:
    command: foo
    plugs: [snapd-control]
`, nil)
	s.slot = &interfaces.Slot{
		SlotInfo: &snap.SlotInfo{
			Snap:      &snap.Info{SuggestedName: "core", Type: snap.TypeOS},
			Name:      "snapd-control",
			Interface: "snapd-control",
		},
	}
	s.plug = &interfaces.Plug{PlugInfo: consumingSnapInfo.Plugs["snapd-control"]}
}

func (s *SnapdControlInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "snapd-control")
}

func (s *SnapdControlInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(s.slot.Sanitize(s.iface), IsNil)
	slot := &interfaces.Slot{SlotInfo: &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "some-snap"},
		Name:      "snapd-control",
		Interface: "snapd-control",
	}}
	c.Assert(slot.Sanitize(s.iface), ErrorMatches,
		"snapd-control slots are reserved for the core snap")
}

func (s *SnapdControlInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(s.plug.Sanitize(s.iface), IsNil)
}

func (s *SnapdControlInterfaceSuite) TestSanitizePlugWithAttrHappy(c *C) {
	const mockSnapYaml = `name: snapd-manager
version: 1.0
plugs:
 snapd-control:
  refresh-schedule: managed
`
	info := snaptest.MockInfo(c, mockSnapYaml, nil)
	plug := &interfaces.Plug{PlugInfo: info.Plugs["snapd-control"]}
	c.Assert(plug.Sanitize(s.iface), IsNil)
}

func (s *SnapdControlInterfaceSuite) TestSanitizePlugWithAttrNotHappy(c *C) {
	const mockSnapYaml = `name: snapd-manager
version: 1.0
plugs:
 snapd-control:
  refresh-schedule: unsupported-value
`
	info := snaptest.MockInfo(c, mockSnapYaml, nil)
	plug := &interfaces.Plug{PlugInfo: info.Plugs["snapd-control"]}
	c.Assert(plug.Sanitize(s.iface), ErrorMatches, `unsupported refresh-schedule value: "unsupported-value"`)
}

func (s *SnapdControlInterfaceSuite) TestUsedSecuritySystems(c *C) {
	// connected plugs have a non-nil security snippet for apparmor
	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, nil, s.slot, nil)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app"), testutil.Contains, `/run/snapd.socket rw,`)
}

func (s *SnapdControlInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
