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
	"github.com/snapcore/snapd/testutil"
)

type BluezInterfaceSuite struct {
	iface interfaces.Interface
	slot  *interfaces.Slot
	plug  *interfaces.Plug
}

var _ = Suite(&BluezInterfaceSuite{
	iface: &builtin.BluezInterface{},
	slot: &interfaces.Slot{
		SlotInfo: &snap.SlotInfo{
			Snap:      &snap.Info{SuggestedName: "bluez"},
			Name:      "bluez",
			Interface: "bluez",
		},
	},
	plug: &interfaces.Plug{
		PlugInfo: &snap.PlugInfo{
			Snap:      &snap.Info{SuggestedName: "bluez"},
			Name:      "bluezctl",
			Interface: "bluez",
		},
	},
})

func (s *BluezInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "bluez")
}

// The label glob when all apps are bound to the bluez slot
func (s *BluezInterfaceSuite) TestConnectedPlugSnippetUsesSlotLabelAll(c *C) {
	app1 := &snap.AppInfo{Name: "app1"}
	app2 := &snap.AppInfo{Name: "app2"}
	slot := &interfaces.Slot{
		SlotInfo: &snap.SlotInfo{
			Snap: &snap.Info{
				SuggestedName: "bluez",
				Apps:          map[string]*snap.AppInfo{"app1": app1, "app2": app2},
			},
			Name:      "bluez",
			Interface: "bluez",
			Apps:      map[string]*snap.AppInfo{"app1": app1, "app2": app2},
		},
	}
	snippet, err := s.iface.ConnectedPlugSnippet(s.plug, nil, slot, nil, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(string(snippet), testutil.Contains, `peer=(label="snap.bluez.*"),`)
}

// The label uses alternation when some, but not all, apps is bound to the bluez slot
func (s *BluezInterfaceSuite) TestConnectedPlugSnippetUsesSlotLabelSome(c *C) {
	app1 := &snap.AppInfo{Name: "app1"}
	app2 := &snap.AppInfo{Name: "app2"}
	app3 := &snap.AppInfo{Name: "app3"}
	slot := &interfaces.Slot{
		SlotInfo: &snap.SlotInfo{
			Snap: &snap.Info{
				SuggestedName: "bluez",
				Apps:          map[string]*snap.AppInfo{"app1": app1, "app2": app2, "app3": app3},
			},
			Name:      "bluez",
			Interface: "bluez",
			Apps:      map[string]*snap.AppInfo{"app1": app1, "app2": app2},
		},
	}
	snippet, err := s.iface.ConnectedPlugSnippet(s.plug, nil, slot, nil, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(string(snippet), testutil.Contains, `peer=(label="snap.bluez.{app1,app2}"),`)
}

// The label uses short form when exactly one app is bound to the bluez slot
func (s *BluezInterfaceSuite) TestConnectedPlugSnippetUsesSlotLabelOne(c *C) {
	app := &snap.AppInfo{Name: "app"}
	slot := &interfaces.Slot{
		SlotInfo: &snap.SlotInfo{
			Snap: &snap.Info{
				SuggestedName: "bluez",
				Apps:          map[string]*snap.AppInfo{"app": app},
			},
			Name:      "bluez",
			Interface: "bluez",
			Apps:      map[string]*snap.AppInfo{"app": app},
		},
	}
	snippet, err := s.iface.ConnectedPlugSnippet(s.plug, nil, slot, nil, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(string(snippet), testutil.Contains, `peer=(label="snap.bluez.app"),`)
}

func (s *BluezInterfaceSuite) TestUsedSecuritySystems(c *C) {
	systems := [...]interfaces.SecuritySystem{interfaces.SecurityAppArmor,
		interfaces.SecuritySecComp}
	for _, system := range systems {
		snippet, err := s.iface.ConnectedPlugSnippet(s.plug, nil, s.slot, nil, system)
		c.Assert(err, IsNil)
		c.Assert(snippet, Not(IsNil))
		snippet, err = s.iface.PermanentSlotSnippet(s.slot, system)
		c.Assert(err, IsNil)
		c.Assert(snippet, Not(IsNil))
	}
	snippet, err := s.iface.ConnectedPlugSnippet(s.plug, nil, s.slot, nil, interfaces.SecurityDBus)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
	snippet, err = s.iface.PermanentSlotSnippet(s.slot, interfaces.SecurityDBus)
	c.Assert(err, IsNil)
	c.Assert(snippet, Not(IsNil))
}
