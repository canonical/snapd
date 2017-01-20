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
	"fmt"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/snap"
)

type TestInterfaceSuite struct {
	iface interfaces.Interface
	plug  *interfaces.Plug
	slot  *interfaces.Slot
}

var _ = Suite(&TestInterfaceSuite{
	iface: &ifacetest.TestInterface{InterfaceName: "test"},
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

// TestInterface has a working Name() function
func (s *TestInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "test")
}

// TestInterface doesn't do any sanitization by default
func (s *TestInterfaceSuite) TestSanitizePlugOK(c *C) {
	err := s.iface.SanitizePlug(s.plug)
	c.Assert(err, IsNil)
}

// TestInterface has provisions to customize sanitization
func (s *TestInterfaceSuite) TestSanitizePlugError(c *C) {
	iface := &ifacetest.TestInterface{
		InterfaceName: "test",
		SanitizePlugCallback: func(plug *interfaces.Plug) error {
			return fmt.Errorf("sanitize plug failed")
		},
	}
	err := iface.SanitizePlug(s.plug)
	c.Assert(err, ErrorMatches, "sanitize plug failed")
}

// TestInterface sanitization still checks for interface identity
func (s *TestInterfaceSuite) TestSanitizePlugWrongInterface(c *C) {
	plug := &interfaces.Plug{
		PlugInfo: &snap.PlugInfo{
			Snap:      &snap.Info{SuggestedName: "snap"},
			Name:      "name",
			Interface: "other-interface",
		},
	}
	c.Assert(func() { s.iface.SanitizePlug(plug) }, Panics, "plug is not of interface \"test\"")
}

// TestInterface doesn't do any sanitization by default
func (s *TestInterfaceSuite) TestSanitizeSlotOK(c *C) {
	err := s.iface.SanitizeSlot(s.slot)
	c.Assert(err, IsNil)
}

// TestInterface has provisions to customize sanitization
func (s *TestInterfaceSuite) TestSanitizeSlotError(c *C) {
	iface := &ifacetest.TestInterface{
		InterfaceName: "test",
		SanitizeSlotCallback: func(slot *interfaces.Slot) error {
			return fmt.Errorf("sanitize slot failed")
		},
	}
	err := iface.SanitizeSlot(s.slot)
	c.Assert(err, ErrorMatches, "sanitize slot failed")
}

// TestInterface sanitization still checks for interface identity
func (s *TestInterfaceSuite) TestSanitizeSlotWrongInterface(c *C) {
	slot := &interfaces.Slot{
		SlotInfo: &snap.SlotInfo{
			Snap:      &snap.Info{SuggestedName: "snap"},
			Name:      "name",
			Interface: "interface",
		},
	}
	c.Assert(func() { s.iface.SanitizeSlot(slot) }, Panics, "slot is not of interface \"test\"")
}

// TestInterface hands out empty plug security snippets
func (s *TestInterfaceSuite) TestPlugSnippet(c *C) {
	snippet, err := s.iface.ConnectedPlugSnippet(s.plug, s.slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
	snippet, err = s.iface.ConnectedPlugSnippet(s.plug, s.slot, interfaces.SecuritySecComp)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
	snippet, err = s.iface.ConnectedPlugSnippet(s.plug, s.slot, interfaces.SecurityDBus)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
	snippet, err = s.iface.ConnectedPlugSnippet(s.plug, s.slot, "foo")
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
}

// TestInterface hands out empty slot security snippets
func (s *TestInterfaceSuite) TestSlotSnippet(c *C) {
	snippet, err := s.iface.ConnectedSlotSnippet(s.plug, s.slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
	snippet, err = s.iface.ConnectedSlotSnippet(s.plug, s.slot, interfaces.SecuritySecComp)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
	snippet, err = s.iface.ConnectedSlotSnippet(s.plug, s.slot, interfaces.SecurityDBus)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
	snippet, err = s.iface.ConnectedSlotSnippet(s.plug, s.slot, "foo")
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
}

func (s *TestInterfaceSuite) TestAutoConnect(c *C) {
	c.Check(s.iface.AutoConnect(nil, nil), Equals, true)

	iface := &ifacetest.TestInterface{AutoConnectCallback: func(*interfaces.Plug, *interfaces.Slot) bool { return false }}

	c.Check(iface.AutoConnect(nil, nil), Equals, false)
}
