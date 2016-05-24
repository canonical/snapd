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
)

type NetworkObserveInterfaceSuite struct {
	iface interfaces.Interface
	slot  *interfaces.Slot
	plug  *interfaces.Plug
}

var _ = Suite(&NetworkObserveInterfaceSuite{
	iface: builtin.NewNetworkObserveInterface(),
	slot: &interfaces.Slot{
		SlotInfo: &snap.SlotInfo{
			Snap:      &snap.Info{SuggestedName: "ubuntu-core", Type: snap.TypeOS},
			Name:      "network-observe",
			Interface: "network-observe",
		},
	},
	plug: &interfaces.Plug{
		PlugInfo: &snap.PlugInfo{
			Snap:      &snap.Info{SuggestedName: "other"},
			Name:      "network-observe",
			Interface: "network-observe",
		},
	},
})

func (s *NetworkObserveInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "network-observe")
}

func (s *NetworkObserveInterfaceSuite) TestSanitizeSlot(c *C) {
	err := s.iface.SanitizeSlot(s.slot)
	c.Assert(err, IsNil)
	err = s.iface.SanitizeSlot(&interfaces.Slot{SlotInfo: &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "some-snap"},
		Name:      "network-observe",
		Interface: "network-observe",
	}})
	c.Assert(err, ErrorMatches, "network-observe slots are reserved for the operating system snap")
}

func (s *NetworkObserveInterfaceSuite) TestSanitizePlug(c *C) {
	err := s.iface.SanitizePlug(s.plug)
	c.Assert(err, IsNil)
}

func (s *NetworkObserveInterfaceSuite) TestSanitizeIncorrectInterface(c *C) {
	c.Assert(func() { s.iface.SanitizeSlot(&interfaces.Slot{SlotInfo: &snap.SlotInfo{Interface: "other"}}) },
		PanicMatches, `slot is not of interface "network-observe"`)
	c.Assert(func() { s.iface.SanitizePlug(&interfaces.Plug{PlugInfo: &snap.PlugInfo{Interface: "other"}}) },
		PanicMatches, `plug is not of interface "network-observe"`)
}

func (s *NetworkObserveInterfaceSuite) TestUnusedSecuritySystems(c *C) {
	systems := [...]interfaces.SecuritySystem{interfaces.SecurityAppArmor,
		interfaces.SecuritySecComp, interfaces.SecurityDBus,
		interfaces.SecurityUDev}
	for _, system := range systems {
		snippet, err := s.iface.PermanentPlugSnippet(s.plug, system)
		c.Assert(err, IsNil)
		c.Assert(snippet, IsNil)
		snippet, err = s.iface.PermanentSlotSnippet(s.slot, system)
		c.Assert(err, IsNil)
		c.Assert(snippet, IsNil)
		snippet, err = s.iface.ConnectedSlotSnippet(s.plug, s.slot, system)
		c.Assert(err, IsNil)
		c.Assert(snippet, IsNil)
	}
	snippet, err := s.iface.ConnectedPlugSnippet(s.plug, s.slot, interfaces.SecurityDBus)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
	snippet, err = s.iface.ConnectedPlugSnippet(s.plug, s.slot, interfaces.SecurityUDev)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
}

func (s *NetworkObserveInterfaceSuite) TestUsedSecuritySystems(c *C) {
	// connected plugs have a non-nil security snippet for apparmor
	snippet, err := s.iface.ConnectedPlugSnippet(s.plug, s.slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, Not(IsNil))
	// connected plugs have a non-nil security snippet for seccomp
	snippet, err = s.iface.ConnectedPlugSnippet(s.plug, s.slot, interfaces.SecuritySecComp)
	c.Assert(err, IsNil)
	c.Assert(snippet, Not(IsNil))
}

func (s *NetworkObserveInterfaceSuite) TestUnexpectedSecuritySystems(c *C) {
	snippet, err := s.iface.PermanentPlugSnippet(s.plug, "foo")
	c.Assert(err, Equals, interfaces.ErrUnknownSecurity)
	c.Assert(snippet, IsNil)
	snippet, err = s.iface.ConnectedPlugSnippet(s.plug, s.slot, "foo")
	c.Assert(err, Equals, interfaces.ErrUnknownSecurity)
	c.Assert(snippet, IsNil)
	snippet, err = s.iface.PermanentSlotSnippet(s.slot, "foo")
	c.Assert(err, Equals, interfaces.ErrUnknownSecurity)
	c.Assert(snippet, IsNil)
	snippet, err = s.iface.ConnectedSlotSnippet(s.plug, s.slot, "foo")
	c.Assert(err, Equals, interfaces.ErrUnknownSecurity)
	c.Assert(snippet, IsNil)
}

func (s *NetworkObserveInterfaceSuite) TestAutoConnect(c *C) {
	c.Check(s.iface.AutoConnect(), Equals, false)
}
