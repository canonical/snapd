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

type TestSpecificationSuite struct {
	iface *ifacetest.TestInterface
	spec  *ifacetest.TestSpecification
	plug  *interfaces.Plug
	slot  *interfaces.Slot
}

var _ = Suite(&TestSpecificationSuite{
	iface: &ifacetest.TestInterface{
		InterfaceName: "test",
		TestConnectedPlugCallback: func(spec *ifacetest.TestSpecification, plug *interfaces.Plug, slot *interfaces.Slot) error {
			spec.AddSnippet("connected-plug")
			return nil
		},
		TestConnectedSlotCallback: func(spec *ifacetest.TestSpecification, plug *interfaces.Plug, slot *interfaces.Slot) error {
			spec.AddSnippet("connected-slot")
			return nil
		},
		TestPermanentPlugCallback: func(spec *ifacetest.TestSpecification, plug *interfaces.Plug) error {
			spec.AddSnippet("permanent-plug")
			return nil
		},
		TestPermanentSlotCallback: func(spec *ifacetest.TestSpecification, slot *interfaces.Slot) error {
			spec.AddSnippet("permanent-slot")
			return nil
		},
	},
	plug: &interfaces.Plug{
		PlugInfo: &snap.PlugInfo{
			Snap:      &snap.Info{SuggestedName: "snap"},
			Name:      "name",
			Interface: "test",
		},
	},
	slot: &interfaces.Slot{
		SlotInfo: &snap.SlotInfo{
			Snap:      &snap.Info{SuggestedName: "snap"},
			Name:      "name",
			Interface: "test",
		},
	},
})

func (s *TestSpecificationSuite) SetUpTest(c *C) {
	s.spec = &ifacetest.TestSpecification{}
}

// AddSnippet is not broken
func (s *TestSpecificationSuite) TestAddSnippet(c *C) {
	s.spec.AddSnippet("hello")
	s.spec.AddSnippet("world")
	c.Assert(s.spec.Snippets, DeepEquals, []string{"hello", "world"})
}

// The TestSpecification can be used through the interfaces.Specification interface
func (s *TestSpecificationSuite) TestSpecificationIface(c *C) {
	var r interfaces.Specification = s.spec
	c.Assert(r.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(r.AddConnectedSlot(s.iface, s.plug, s.slot), IsNil)
	c.Assert(r.AddPermanentPlug(s.iface, s.plug), IsNil)
	c.Assert(r.AddPermanentSlot(s.iface, s.slot), IsNil)
	c.Assert(s.spec.Snippets, DeepEquals, []string{
		"connected-plug", "connected-slot", "permanent-plug", "permanent-slot"})
}
