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

func (s *utilsSuite) TestLabelExpr(c *C) {
	yaml := `name: test-snap
version: 1
apps:
 app1:
  command: bin/test1
  plugs: [home]
 app2:
  command: bin/test2
  plugs: [home]
hooks:
 install:
  plugs: [network,network-manager]
 post-refresh:
  plugs: [network,network-manager]
`
	info := snaptest.MockInfo(c, yaml, nil)

	// all apps and all hooks
	label := builtin.LabelExpr(info.Apps, info.Hooks, info)
	c.Check(label, Equals, `"snap.test-snap.*"`)

	// all apps, no hooks
	label = builtin.LabelExpr(info.Apps, nil, info)
	c.Check(label, Equals, `"snap.test-snap.{app1,app2}"`)

	// one app, no hooks
	label = builtin.LabelExpr(map[string]*snap.AppInfo{"app1": info.Apps["app1"]}, nil, info)
	c.Check(label, Equals, `"snap.test-snap.app1"`)

	// one app, all hooks
	label = builtin.LabelExpr(map[string]*snap.AppInfo{"app1": info.Apps["app1"]}, info.Hooks, info)
	c.Check(label, Equals, `"snap.test-snap.{app1,hook.install,hook.post-refresh}"`)

	// only hooks
	label = builtin.LabelExpr(nil, info.Hooks, info)
	c.Check(label, Equals, `"snap.test-snap.{hook.install,hook.post-refresh}"`)
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
