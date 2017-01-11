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

type PhysicalMemoryObserveInterfaceSuite struct {
	iface interfaces.Interface
	slot  *interfaces.Slot
	plug  *interfaces.Plug
}

var _ = Suite(&PhysicalMemoryObserveInterfaceSuite{
	iface: &builtin.PhysicalMemoryObserveInterface{},
})

func (s *PhysicalMemoryObserveInterfaceSuite) SetUpTest(c *C) {
	// Mock for OS Snap
	osSnapInfo := snaptest.MockInfo(c, `
name: ubuntu-core
type: os
slots:
  test-physical-memory:
    interface: physical-memory-observe
`, nil)
	s.slot = &interfaces.Slot{SlotInfo: osSnapInfo.Slots["test-physical-memory"]}

	// Snap Consumers
	consumingSnapInfo := snaptest.MockInfo(c, `
name: client-snap
plugs:
  plug-for-physical-memory:
    interface: physical-memory-observe
apps:
  app-accessing-physical-memory:
    command: foo
    plugs: [plug-for-physical-memory]
`, nil)
	s.plug = &interfaces.Plug{PlugInfo: consumingSnapInfo.Plugs["plug-for-physical-memory"]}
}

func (s *PhysicalMemoryObserveInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "physical-memory-observe")
}

func (s *PhysicalMemoryObserveInterfaceSuite) TestSanitizeSlot(c *C) {
	err := s.iface.SanitizeSlot(s.slot)
	c.Assert(err, IsNil)
	err = s.iface.SanitizeSlot(&interfaces.Slot{SlotInfo: &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "some-snap"},
		Name:      "physical-memory-observe",
		Interface: "physical-memory-observe",
	}})
	c.Assert(err, ErrorMatches, "physical-memory-observe slots only allowed on core snap")
}

func (s *PhysicalMemoryObserveInterfaceSuite) TestSanitizePlug(c *C) {
	err := s.iface.SanitizePlug(s.plug)
	c.Assert(err, IsNil)
}

func (s *PhysicalMemoryObserveInterfaceSuite) TestSanitizeIncorrectInterface(c *C) {
	c.Assert(func() { s.iface.SanitizeSlot(&interfaces.Slot{SlotInfo: &snap.SlotInfo{Interface: "other"}}) },
		PanicMatches, `slot is not of interface "physical-memory-observe"`)
	c.Assert(func() { s.iface.SanitizePlug(&interfaces.Plug{PlugInfo: &snap.PlugInfo{Interface: "other"}}) },
		PanicMatches, `plug is not of interface "physical-memory-observe"`)
}

func (s *PhysicalMemoryObserveInterfaceSuite) TestUsedSecuritySystems(c *C) {
	expectedSnippet1 := []byte(`
# Description: With kernels with STRICT_DEVMEM=n, read-only access to all physical
# memory. With STRICT_DEVMEM=y, allow reading /dev/mem for read-only
# access to architecture-specific subset of the physical address (eg, PCI,
# space, BIOS code and data regions on x86, etc).
/dev/mem r,
`)
	expectedSnippet2 := []byte(`KERNEL=="mem", TAG+="snap_client-snap_app-accessing-physical-memory"
`)

	// connected plugs have a non-nil security snippet for apparmor
	snippet, err := s.iface.ConnectedPlugSnippet(s.plug, s.slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, DeepEquals, expectedSnippet1, Commentf("\nexpected:\n%s\nfound:\n%s", expectedSnippet1, snippet))

	snippet, err = s.iface.ConnectedPlugSnippet(s.plug, s.slot, interfaces.SecurityUDev)
	c.Assert(err, IsNil)
	c.Assert(snippet, DeepEquals, expectedSnippet2, Commentf("\nexpected:\n%s\nfound:\n%s", expectedSnippet2, snippet))
}
