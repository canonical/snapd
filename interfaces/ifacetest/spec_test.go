// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2017 Canonical Ltd
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

package ifacetest_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/snap"
)

type SpecificationSuite struct {
	iface    *ifacetest.TestInterface
	spec     *ifacetest.Specification
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
}

var _ = Suite(&SpecificationSuite{
	iface: &ifacetest.TestInterface{
		InterfaceName: "test",
		TestConnectedPlugCallback: func(spec *ifacetest.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
			spec.AddSnippet("connected-plug")
			return nil
		},
		TestConnectedSlotCallback: func(spec *ifacetest.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
			spec.AddSnippet("connected-slot")
			return nil
		},
		TestPermanentPlugCallback: func(spec *ifacetest.Specification, plug *snap.PlugInfo) error {
			spec.AddSnippet("permanent-plug")
			return nil
		},
		TestPermanentSlotCallback: func(spec *ifacetest.Specification, slot *snap.SlotInfo) error {
			spec.AddSnippet("permanent-slot")
			return nil
		},
	},
	plugInfo: &snap.PlugInfo{
		Snap:      &snap.Info{SuggestedName: "snap"},
		Name:      "name",
		Interface: "test",
	},
	slotInfo: &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "snap"},
		Name:      "name",
		Interface: "test",
	},
})

func (s *SpecificationSuite) SetUpTest(c *C) {
	s.spec = &ifacetest.Specification{}
	s.plug = interfaces.NewConnectedPlug(s.plugInfo, nil, nil)
	s.slot = interfaces.NewConnectedSlot(s.slotInfo, nil, nil)
}

// AddSnippet is not broken
func (s *SpecificationSuite) TestAddSnippet(c *C) {
	s.spec.AddSnippet("hello")
	s.spec.AddSnippet("world")
	c.Assert(s.spec.Snippets, DeepEquals, []string{"hello", "world"})
}

// The Specification can be used through the interfaces.Specification interface
func (s *SpecificationSuite) SpecificationIface(c *C) {
	var r interfaces.Specification = s.spec
	c.Assert(r.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(r.AddConnectedSlot(s.iface, s.plug, s.slot), IsNil)
	c.Assert(r.AddPermanentPlug(s.iface, s.plugInfo), IsNil)
	c.Assert(r.AddPermanentSlot(s.iface, s.slotInfo), IsNil)
	c.Assert(s.spec.Snippets, DeepEquals, []string{
		"connected-plug", "connected-slot", "permanent-plug", "permanent-slot",
	})
}
