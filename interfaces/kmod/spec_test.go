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

package kmod_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/interfaces/kmod"
	"github.com/snapcore/snapd/snap"
)

type specSuite struct {
	iface1, iface2 *ifacetest.TestInterface
	spec           *kmod.Specification
	plug           *interfaces.Plug
	slot           *interfaces.Slot
}

var _ = Suite(&specSuite{
	iface1: &ifacetest.TestInterface{
		InterfaceName: "test",
		KModConnectedPlugCallback: func(spec *kmod.Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error {
			return spec.AddModule("module1")
		},
		KModConnectedSlotCallback: func(spec *kmod.Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error {
			return spec.AddModule("module2")
		},
		KModPermanentPlugCallback: func(spec *kmod.Specification, plug *interfaces.Plug) error {
			return spec.AddModule("module3")
		},
		KModPermanentSlotCallback: func(spec *kmod.Specification, slot *interfaces.Slot) error {
			return spec.AddModule("module4")
		},
	},
	iface2: &ifacetest.TestInterface{
		InterfaceName: "test-two",
		KModConnectedPlugCallback: func(spec *kmod.Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error {
			return spec.AddModule("module1")
		},
		KModConnectedSlotCallback: func(spec *kmod.Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error {
			return spec.AddModule("module2")
		},
		KModPermanentPlugCallback: func(spec *kmod.Specification, plug *interfaces.Plug) error {
			return spec.AddModule("module5")
		},
		KModPermanentSlotCallback: func(spec *kmod.Specification, slot *interfaces.Slot) error {
			return spec.AddModule("module6")
		},
	},
	plug: &interfaces.Plug{
		PlugInfo: &snap.PlugInfo{
			PlugSlotData: snap.PlugSlotData{
				Snap:      &snap.Info{SuggestedName: "snap"},
				Name:      "name",
				Interface: "test",
			},
		},
	},
	slot: &interfaces.Slot{
		SlotInfo: &snap.SlotInfo{
			PlugSlotData: snap.PlugSlotData{
				Snap:      &snap.Info{SuggestedName: "snap"},
				Name:      "name",
				Interface: "test",
			},
		},
	},
})

func (s *specSuite) SetUpTest(c *C) {
	s.spec = &kmod.Specification{}
}

// AddModule is not broken
func (s *specSuite) TestSmoke(c *C) {
	m1 := "module1"
	m2 := "module2"
	c.Assert(s.spec.AddModule(m1), IsNil)
	c.Assert(s.spec.AddModule(m2), IsNil)
	c.Assert(s.spec.Modules(), DeepEquals, map[string]bool{
		"module1": true, "module2": true})
}

// AddModule ignores duplicated modules
func (s *specSuite) TestDeduplication(c *C) {
	mod := "module1"
	c.Assert(s.spec.AddModule(mod), IsNil)
	c.Assert(s.spec.AddModule(mod), IsNil)
	c.Assert(s.spec.Modules(), DeepEquals, map[string]bool{"module1": true})

	var r interfaces.Specification = s.spec
	c.Assert(r.AddConnectedPlug(s.iface1, s.plug, nil, s.slot, nil), IsNil)
	c.Assert(r.AddConnectedSlot(s.iface1, s.plug, nil, s.slot, nil), IsNil)
	c.Assert(r.AddPermanentPlug(s.iface1, s.plug), IsNil)
	c.Assert(r.AddPermanentSlot(s.iface1, s.slot), IsNil)

	c.Assert(r.AddConnectedPlug(s.iface2, s.plug, nil, s.slot, nil), IsNil)
	c.Assert(r.AddConnectedSlot(s.iface2, s.plug, nil, s.slot, nil), IsNil)
	c.Assert(r.AddPermanentPlug(s.iface2, s.plug), IsNil)
	c.Assert(r.AddPermanentSlot(s.iface2, s.slot), IsNil)
	c.Assert(s.spec.Modules(), DeepEquals, map[string]bool{
		"module1": true, "module2": true, "module3": true, "module4": true, "module5": true, "module6": true})
}

// The kmod.Specification can be used through the interfaces.Specification interface
func (s *specSuite) TestSpecificationIface(c *C) {
	var r interfaces.Specification = s.spec
	c.Assert(r.AddConnectedPlug(s.iface1, s.plug, nil, s.slot, nil), IsNil)
	c.Assert(r.AddConnectedSlot(s.iface1, s.plug, nil, s.slot, nil), IsNil)
	c.Assert(r.AddPermanentPlug(s.iface1, s.plug), IsNil)
	c.Assert(r.AddPermanentSlot(s.iface1, s.slot), IsNil)
	c.Assert(s.spec.Modules(), DeepEquals, map[string]bool{
		"module1": true, "module2": true, "module3": true, "module4": true})
}
