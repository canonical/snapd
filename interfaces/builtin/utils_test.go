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

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
)

type utilsSuite struct {
	iface        interfaces.Interface
	slotOS       *snap.SlotInfo
	slotApp      *snap.SlotInfo
	slotSnapd    *snap.SlotInfo
	slotGadget   *snap.SlotInfo
	conSlotOS    *interfaces.ConnectedSlot
	conSlotSnapd *interfaces.ConnectedSlot
	conSlotApp   *interfaces.ConnectedSlot
}

var _ = Suite(&utilsSuite{
	iface:        &ifacetest.TestInterface{InterfaceName: "iface"},
	slotOS:       &snap.SlotInfo{Snap: &snap.Info{SnapType: snap.TypeOS}},
	slotApp:      &snap.SlotInfo{Snap: &snap.Info{SnapType: snap.TypeApp}},
	slotSnapd:    &snap.SlotInfo{Snap: &snap.Info{SnapType: snap.TypeSnapd, SuggestedName: "snapd"}},
	slotGadget:   &snap.SlotInfo{Snap: &snap.Info{SnapType: snap.TypeGadget}},
	conSlotOS:    interfaces.NewConnectedSlot(&snap.SlotInfo{Snap: &snap.Info{SnapType: snap.TypeOS}}, nil, nil),
	conSlotSnapd: interfaces.NewConnectedSlot(&snap.SlotInfo{Snap: &snap.Info{SnapType: snap.TypeSnapd}}, nil, nil),
	conSlotApp:   interfaces.NewConnectedSlot(&snap.SlotInfo{Snap: &snap.Info{SnapType: snap.TypeApp}}, nil, nil),
})

func (s *utilsSuite) TestSanitizeSlotReservedForOS(c *C) {
	errmsg := "iface slots are reserved for the core snap"
	c.Assert(builtin.SanitizeSlotReservedForOS(s.iface, s.slotOS), IsNil)
	c.Assert(builtin.SanitizeSlotReservedForOS(s.iface, s.slotSnapd), IsNil)
	c.Assert(builtin.SanitizeSlotReservedForOS(s.iface, s.slotApp), ErrorMatches, errmsg)
	c.Assert(builtin.SanitizeSlotReservedForOS(s.iface, s.slotGadget), ErrorMatches, errmsg)
}

func (s *utilsSuite) TestSanitizeSlotReservedForOSOrGadget(c *C) {
	errmsg := "iface slots are reserved for the core and gadget snaps"
	c.Assert(builtin.SanitizeSlotReservedForOSOrGadget(s.iface, s.slotOS), IsNil)
	c.Assert(builtin.SanitizeSlotReservedForOSOrGadget(s.iface, s.slotSnapd), IsNil)
	c.Assert(builtin.SanitizeSlotReservedForOSOrGadget(s.iface, s.slotApp), ErrorMatches, errmsg)
	c.Assert(builtin.SanitizeSlotReservedForOSOrGadget(s.iface, s.slotGadget), IsNil)
}

func (s *utilsSuite) TestSanitizeSlotReservedForOSOrApp(c *C) {
	errmsg := "iface slots are reserved for the core and app snaps"
	c.Assert(builtin.SanitizeSlotReservedForOSOrApp(s.iface, s.slotOS), IsNil)
	c.Assert(builtin.SanitizeSlotReservedForOSOrApp(s.iface, s.slotApp), IsNil)
	c.Assert(builtin.SanitizeSlotReservedForOSOrApp(s.iface, s.slotGadget), ErrorMatches, errmsg)
}

func (s *utilsSuite) TestIsSlotSystemSlot(c *C) {
	c.Assert(builtin.ImplicitSystemPermanentSlot(s.slotApp), Equals, false)
	c.Assert(builtin.ImplicitSystemPermanentSlot(s.slotOS), Equals, true)
	c.Assert(builtin.ImplicitSystemPermanentSlot(s.slotSnapd), Equals, true)
}

func (s *utilsSuite) TestImplicitSystemConnectedSlot(c *C) {
	c.Assert(builtin.ImplicitSystemConnectedSlot(s.conSlotApp), Equals, false)
	c.Assert(builtin.ImplicitSystemConnectedSlot(s.conSlotOS), Equals, true)
	c.Assert(builtin.ImplicitSystemConnectedSlot(s.conSlotSnapd), Equals, true)
}

func MockPlug(c *C, yaml string, si *snap.SideInfo, plugName string) *snap.PlugInfo {
	return builtin.MockPlug(c, yaml, si, plugName)
}

func MockSlot(c *C, yaml string, si *snap.SideInfo, slotName string) *snap.SlotInfo {
	return builtin.MockSlot(c, yaml, si, slotName)
}

func MockConnectedPlug(c *C, yaml string, si *snap.SideInfo, plugName string) (*interfaces.ConnectedPlug, *snap.PlugInfo) {
	info := snaptest.MockInfo(c, yaml, si)
	if plugInfo, ok := info.Plugs[plugName]; ok {
		return interfaces.NewConnectedPlug(plugInfo, nil, nil), plugInfo
	}
	panic(fmt.Sprintf("cannot find plug %q in snap %q", plugName, info.InstanceName()))
}

func MockConnectedSlot(c *C, yaml string, si *snap.SideInfo, slotName string) (*interfaces.ConnectedSlot, *snap.SlotInfo) {
	info := snaptest.MockInfo(c, yaml, si)
	if slotInfo, ok := info.Slots[slotName]; ok {
		return interfaces.NewConnectedSlot(slotInfo, nil, nil), slotInfo
	}
	panic(fmt.Sprintf("cannot find slot %q in snap %q", slotName, info.InstanceName()))
}

func MockHotplugSlot(c *C, yaml string, si *snap.SideInfo, hotplugKey snap.HotplugKey, ifaceName, slotName string, staticAttrs map[string]interface{}) *snap.SlotInfo {
	info := snaptest.MockInfo(c, yaml, si)
	if _, ok := info.Slots[slotName]; ok {
		panic(fmt.Sprintf("slot %q already present in the snap yaml", slotName))
	}
	return &snap.SlotInfo{
		Snap:       info,
		Name:       slotName,
		Attrs:      staticAttrs,
		HotplugKey: hotplugKey,
	}
}
