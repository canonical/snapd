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
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
)

type UhidInterfaceSuite struct {
	iface interfaces.Interface

	testSlot1 *interfaces.Slot

	testUdevBadValue1     *interfaces.Slot
	testUdevBadValue2     *interfaces.Slot
	testUdevBadValue3     *interfaces.Slot
	testUdevBadValue4     *interfaces.Slot
	testUdevBadInterface1 *interfaces.Slot

	plug *interfaces.Plug
}

var _ = Suite(&UhidInterfaceSuite{
	iface: &builtin.UhidInterface{},
})

func (s *UhidInterfaceSuite) SetUpTest(c *C) {
	// Mocking
	osSnapInfo := snaptest.MockInfo(c, `
name: ubuntu-core
type: os
slots:
  test-slot-1:
    interface: uhid
    path: /dev/uhid
  test-udev-bad-value-1:
    interface: uhid
    path: /dev/i2c-1
  test-udev-bad-value-2:
    interface: uhid
    path: ""
  test-udev-bad-value-3:
    interface: uhid
    path: /dev/uhid-1
  test-udev-bad-value-4:
    interface: uhid
    path: /uhid
  test-udev-bad-interface-1:
    interface: other-interface
    path: /dev/uhid
`, nil)
	s.testSlot1 = &interfaces.Slot{SlotInfo: osSnapInfo.Slots["test-slot-1"]}
	s.testUdevBadValue1 = &interfaces.Slot{SlotInfo: osSnapInfo.Slots["test-udev-bad-value-1"]}
	s.testUdevBadValue2 = &interfaces.Slot{SlotInfo: osSnapInfo.Slots["test-udev-bad-value-2"]}
	s.testUdevBadValue3 = &interfaces.Slot{SlotInfo: osSnapInfo.Slots["test-udev-bad-value-3"]}
	s.testUdevBadValue4 = &interfaces.Slot{SlotInfo: osSnapInfo.Slots["test-udev-bad-value-4"]}
	s.testUdevBadInterface1 = &interfaces.Slot{SlotInfo: osSnapInfo.Slots["test-udev-bad-interface-1"]}

	// Snap Consumers
	consumingSnapInfo := snaptest.MockInfo(c, `
name: client-snap
plugs:
  plug-for-slot-1:
    interface: uhid
apps:
  app-accessing-slot-1:
    command: foo
    plugs: [uhid]
`, nil)
	s.plug = &interfaces.Plug{PlugInfo: consumingSnapInfo.Plugs["plug-for-slot-1"]}
}

func (s *UhidInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "uhid")
}

func (s *UhidInterfaceSuite) TestSanitizeSlot(c *C) {
	err := s.iface.SanitizeSlot(s.testSlot1)
	c.Assert(err, IsNil)
}

func (s *UhidInterfaceSuite) TestSanitizeBadOsSnapSlots(c *C) {
	err := s.iface.SanitizeSlot(s.testUdevBadValue1)
	c.Assert(err, ErrorMatches, "uhid path attribute must be a valid device node")

	err = s.iface.SanitizeSlot(s.testUdevBadValue2)
	c.Assert(err, ErrorMatches, "uhid slot must have a path attribute")

	err = s.iface.SanitizeSlot(s.testUdevBadValue3)
	c.Assert(err, ErrorMatches, "uhid path attribute must be a valid device node")

	err = s.iface.SanitizeSlot(s.testUdevBadValue4)
	c.Assert(err, ErrorMatches, "uhid path attribute must be a valid device node")

	c.Assert(func() { s.iface.SanitizeSlot(s.testUdevBadInterface1) }, PanicMatches, `slot is not of interface "uhid"`)
}

func (s *UhidInterfaceSuite) TestSanitizePlug(c *C) {
	err := s.iface.SanitizePlug(s.plug)
	c.Assert(err, IsNil)
}

func (s *UhidInterfaceSuite) TestSanitizeBadPlug(c *C) {
	c.Assert(func() { s.iface.SanitizePlug(&interfaces.Plug{PlugInfo: &snap.PlugInfo{Interface: "other"}}) },
		PanicMatches, `plug is not of interface "uhid"`)
}

func (s *UhidInterfaceSuite) TestConnectedPlugAppArmorSnippets(c *C) {
	// connected plugs have a non-nil security snippet for apparmor
	snippet, err := s.iface.ConnectedPlugSnippet(s.plug, s.testSlot1, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, Not(IsNil))
}

func (s *UhidInterfaceSuite) TestConnectedPlugUdevSnippets(c *C) {

	expectedSnippet1 := []byte(`KERNEL=="uhid", TAG+="snap_client-snap_app-accessing-slot-1"
`)

	snippet, err := s.iface.ConnectedPlugSnippet(s.plug, s.testSlot1, interfaces.SecurityUDev)
	c.Assert(err, IsNil)
	c.Assert(snippet, DeepEquals, expectedSnippet1, Commentf("\nexpected:\n%s\nfound:\n%s", expectedSnippet1, snippet))
}

func (s *UhidInterfaceSuite) TestAutoConnect(c *C) {
	c.Check(s.iface.AutoConnect(nil, nil), Equals, true)
}
