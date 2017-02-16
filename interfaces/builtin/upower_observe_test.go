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
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type UPowerObserveInterfaceSuite struct {
	iface interfaces.Interface
	slot  *interfaces.Slot
	plug  *interfaces.Plug
}

var _ = Suite(&UPowerObserveInterfaceSuite{
	iface: &builtin.UpowerObserveInterface{},
	slot: &interfaces.Slot{
		SlotInfo: &snap.SlotInfo{
			Snap:      &snap.Info{SuggestedName: "core", Type: snap.TypeOS},
			Name:      "upower-observe",
			Interface: "upower-observe",
		},
	},
	plug: &interfaces.Plug{
		PlugInfo: &snap.PlugInfo{
			Snap:      &snap.Info{SuggestedName: "other"},
			Name:      "upower-observe",
			Interface: "upower-observe",
		},
	},
})

func (s *UPowerObserveInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "upower-observe")
}

func (s *UPowerObserveInterfaceSuite) TestSanitizeSlot(c *C) {
	err := s.iface.SanitizeSlot(s.slot)
	c.Assert(err, IsNil)
	err = s.iface.SanitizeSlot(&interfaces.Slot{SlotInfo: &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "some-snap"},
		Name:      "upower-observe",
		Interface: "upower-observe",
	}})
	c.Assert(err, ErrorMatches, "upower-observe slots are reserved for the operating system or application snaps")
}

func (s *UPowerObserveInterfaceSuite) TestSanitizePlug(c *C) {
	err := s.iface.SanitizePlug(s.plug)
	c.Assert(err, IsNil)
}

func (s *UPowerObserveInterfaceSuite) TestSanitizeIncorrectInterface(c *C) {
	c.Assert(func() { s.iface.SanitizeSlot(&interfaces.Slot{SlotInfo: &snap.SlotInfo{Interface: "other"}}) },
		PanicMatches, `slot is not of interface "upower-observe"`)
	c.Assert(func() { s.iface.SanitizePlug(&interfaces.Plug{PlugInfo: &snap.PlugInfo{Interface: "other"}}) },
		PanicMatches, `plug is not of interface "upower-observe"`)
}

func (s *UPowerObserveInterfaceSuite) TestUsedSecuritySystems(c *C) {
	// connected plugs have a non-nil security snippet for apparmor
	snippet, err := s.iface.ConnectedPlugSnippet(s.plug, nil, s.slot, nil, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, Not(IsNil))
	// connected plugs have a non-nil security snippet for seccomp
	snippet, err = s.iface.ConnectedPlugSnippet(s.plug, nil, s.slot, nil, interfaces.SecuritySecComp)
	c.Assert(err, IsNil)
	c.Assert(snippet, Not(IsNil))
}

// The label glob when all apps are bound to the ofono slot
func (s *UPowerObserveInterfaceSuite) TestConnectedPlugSnippetUsesSlotLabelAll(c *C) {
	app1 := &snap.AppInfo{Name: "app1"}
	app2 := &snap.AppInfo{Name: "app2"}
	slot := &interfaces.Slot{
		SlotInfo: &snap.SlotInfo{
			Snap: &snap.Info{
				SuggestedName: "upower",
				Apps:          map[string]*snap.AppInfo{"app1": app1, "app2": app2},
			},
			Name:      "upower",
			Interface: "upower-observe",
			Apps:      map[string]*snap.AppInfo{"app1": app1, "app2": app2},
		},
	}
	release.OnClassic = false
	snippet, err := s.iface.ConnectedPlugSnippet(s.plug, nil, slot, nil, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(string(snippet), testutil.Contains, `peer=(label="snap.upower.*"),`)
}

// The label uses alternation when some, but not all, apps is bound to the ofono slot
func (s *UPowerObserveInterfaceSuite) TestConnectedPlugSnippetUsesSlotLabelSome(c *C) {
	app1 := &snap.AppInfo{Name: "app1"}
	app2 := &snap.AppInfo{Name: "app2"}
	app3 := &snap.AppInfo{Name: "app3"}
	slot := &interfaces.Slot{
		SlotInfo: &snap.SlotInfo{
			Snap: &snap.Info{
				SuggestedName: "upower",
				Apps:          map[string]*snap.AppInfo{"app1": app1, "app2": app2, "app3": app3},
			},
			Name:      "upower",
			Interface: "upower",
			Apps:      map[string]*snap.AppInfo{"app1": app1, "app2": app2},
		},
	}
	release.OnClassic = false
	snippet, err := s.iface.ConnectedPlugSnippet(s.plug, nil, slot, nil, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(string(snippet), testutil.Contains, `peer=(label="snap.upower.{app1,app2}"),`)
}

// The label uses short form when exactly one app is bound to the upower-observe slot
func (s *UPowerObserveInterfaceSuite) TestConnectedPlugSnippetUsesSlotLabelOne(c *C) {
	app := &snap.AppInfo{Name: "app"}
	slot := &interfaces.Slot{
		SlotInfo: &snap.SlotInfo{
			Snap: &snap.Info{
				SuggestedName: "upower",
				Apps:          map[string]*snap.AppInfo{"app": app},
			},
			Name:      "upower",
			Interface: "upower",
			Apps:      map[string]*snap.AppInfo{"app": app},
		},
	}
	release.OnClassic = false
	snippet, err := s.iface.ConnectedPlugSnippet(s.plug, nil, slot, nil, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(string(snippet), testutil.Contains, `peer=(label="snap.upower.app"),`)
}

func (s *UPowerObserveInterfaceSuite) TestConnectedPlugSnippetUsesUnconfinedLabelOnClassic(c *C) {
	release.OnClassic = true
	snippet, err := s.iface.ConnectedPlugSnippet(s.plug, nil, s.slot, nil, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	// verify apparmor connected
	c.Assert(string(snippet), testutil.Contains, "#include <abstractions/dbus-strict>")
	// verify classic connected
	c.Assert(string(snippet), testutil.Contains, "peer=(label=unconfined),")
}

func (s *UPowerObserveInterfaceSuite) TestConnectedPlugSnippetAppArmor(c *C) {
	release.OnClassic = false
	snippet, err := s.iface.ConnectedPlugSnippet(s.plug, nil, s.slot, nil, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	// verify apparmor connected
	c.Assert(string(snippet), testutil.Contains, "#include <abstractions/dbus-strict>")
	// verify classic didn't connect
	c.Assert(string(snippet), Not(testutil.Contains), "peer=(label=unconfined),")
}

func (s *UPowerObserveInterfaceSuite) TestConnectedPlugSnippetSecComp(c *C) {
	snippet, err := s.iface.ConnectedPlugSnippet(s.plug, nil, s.slot, nil, interfaces.SecuritySecComp)
	c.Assert(err, IsNil)
	c.Assert(snippet, Not(IsNil))
	c.Check(string(snippet), testutil.Contains, "send\n")
}

func (s *UPowerObserveInterfaceSuite) TestPermanentSlotSnippetAppArmor(c *C) {
	snippet, err := s.iface.PermanentSlotSnippet(s.slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, Not(IsNil))
	c.Check(string(snippet), testutil.Contains, "org.freedesktop.UPower")
}

func (s *UPowerObserveInterfaceSuite) TestPermanentSlotSnippetSecComp(c *C) {
	snippet, err := s.iface.PermanentSlotSnippet(s.slot, interfaces.SecuritySecComp)
	c.Assert(err, IsNil)
	c.Assert(snippet, Not(IsNil))
	c.Check(string(snippet), testutil.Contains, "bind\n")
}

func (s *UPowerObserveInterfaceSuite) TestConnectedSlotSnippetUsesPlugLabelOne(c *C) {
	app := &snap.AppInfo{Name: "app"}
	plug := &interfaces.Plug{
		PlugInfo: &snap.PlugInfo{
			Snap: &snap.Info{
				SuggestedName: "upower",
				Apps:          map[string]*snap.AppInfo{"app": app},
			},
			Name:      "upower",
			Interface: "upower-observe",
			Apps:      map[string]*snap.AppInfo{"app": app},
		},
	}
	snippet, err := s.iface.ConnectedSlotSnippet(plug, nil, s.slot, nil, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(string(snippet), testutil.Contains, `peer=(label="snap.upower.app"),`)
}
