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
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type HardwareRandomObserveInterfaceSuite struct {
	iface interfaces.Interface
	slot  *interfaces.Slot
	plug  *interfaces.Plug
}

var _ = Suite(&HardwareRandomObserveInterfaceSuite{
	iface: &builtin.HardwareRandomObserveInterface{},
})

func (s *HardwareRandomObserveInterfaceSuite) SetUpTest(c *C) {
	// Mock for OS Snap
	osSnapInfo := snaptest.MockInfo(c, `
name: core
type: os
slots:
  hardware-random-observe:
`, nil)
	s.slot = &interfaces.Slot{SlotInfo: osSnapInfo.Slots["hardware-random-observe"]}

	// Snap Consumers
	consumingSnapInfo := snaptest.MockInfo(c, `
name: snap
apps:
  app:
    command: foo
    plugs: [hardware-random-observe]
`, nil)
	s.plug = &interfaces.Plug{PlugInfo: consumingSnapInfo.Plugs["hardware-random-observe"]}
}

func (s *HardwareRandomObserveInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "hardware-random-observe")
}

func (s *HardwareRandomObserveInterfaceSuite) TestSanitizeSlot(c *C) {
	err := s.iface.SanitizeSlot(s.slot)
	c.Assert(err, IsNil)
	err = s.iface.SanitizeSlot(&interfaces.Slot{SlotInfo: &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "some-snap"},
		Name:      "hardware-random-observe",
		Interface: "hardware-random-observe",
	}})
	c.Assert(err, ErrorMatches, "hardware-random-observe slots are reserved for the operating system snap")
}

func (s *HardwareRandomObserveInterfaceSuite) TestSanitizePlug(c *C) {
	err := s.iface.SanitizePlug(s.plug)
	c.Assert(err, IsNil)
}

func (s *HardwareRandomObserveInterfaceSuite) TestSanitizeIncorrectInterface(c *C) {
	c.Assert(func() { s.iface.SanitizeSlot(&interfaces.Slot{SlotInfo: &snap.SlotInfo{Interface: "other"}}) },
		PanicMatches, `slot is not of interface "hardware-random-observe"`)
	c.Assert(func() { s.iface.SanitizePlug(&interfaces.Plug{PlugInfo: &snap.PlugInfo{Interface: "other"}}) },
		PanicMatches, `plug is not of interface "hardware-random-observe"`)
}

func (s *HardwareRandomObserveInterfaceSuite) TestAppArmorSpec(c *C) {
	spec := &apparmor.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, nil, s.slot, nil), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.snap.app"})
	c.Assert(spec.SnippetForTag("snap.snap.app"), testutil.Contains, "hw_random/rng_{available,current} r,")
}

func (s *HardwareRandomObserveInterfaceSuite) TestUDevSpec(c *C) {
	spec := &udev.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, nil, s.slot, nil), IsNil)
	expected := []string{`KERNEL=="hwrng", TAG+="snap_snap_app"`}
	c.Assert(spec.Snippets(), DeepEquals, expected)
}

func (s *HardwareRandomObserveInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
