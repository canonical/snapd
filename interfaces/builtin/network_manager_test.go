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

type NetworkManagerInterfaceSuite struct {
	iface interfaces.Interface
	slot  *interfaces.Slot
	plug  *interfaces.Plug
}

var _ = Suite(&NetworkManagerInterfaceSuite{
	iface: &builtin.NetworkManagerInterface{},
	slot: &interfaces.Slot{
		SlotInfo: &snap.SlotInfo{
			Snap:      &snap.Info{SuggestedName: "network-manager"},
			Name:      "network-manager",
			Interface: "network-manager",
		},
	},
	plug: &interfaces.Plug{
		PlugInfo: &snap.PlugInfo{
			Snap:      &snap.Info{SuggestedName: "network-manager"},
			Name:      "nmcli",
			Interface: "network-manager",
		},
	},
})

func (s *NetworkManagerInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "network-manager")
}

// The label glob when all apps are bound to the network-manager slot
func (s *NetworkManagerInterfaceSuite) TestConnectedPlugSnippetUsesSlotLabelAll(c *C) {
	app1 := &snap.AppInfo{Name: "app1"}
	app2 := &snap.AppInfo{Name: "app2"}
	slot := &interfaces.Slot{
		SlotInfo: &snap.SlotInfo{
			Snap: &snap.Info{
				SuggestedName: "network-manager",
				Apps:          map[string]*snap.AppInfo{"app1": app1, "app2": app2},
			},
			Name:      "network-manager",
			Interface: "network-manager",
			Apps:      map[string]*snap.AppInfo{"app1": app1, "app2": app2},
		},
	}
	release.OnClassic = false
	snippet, err := s.iface.ConnectedPlugSnippet(s.plug, slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(string(snippet), testutil.Contains, `peer=(label="snap.network-manager.*"),`)
}

// The label uses alternation when some, but not all, apps is bound to the network-manager slot
func (s *NetworkManagerInterfaceSuite) TestConnectedPlugSnippetUsesSlotLabelSome(c *C) {
	app1 := &snap.AppInfo{Name: "app1"}
	app2 := &snap.AppInfo{Name: "app2"}
	app3 := &snap.AppInfo{Name: "app3"}
	slot := &interfaces.Slot{
		SlotInfo: &snap.SlotInfo{
			Snap: &snap.Info{
				SuggestedName: "network-manager",
				Apps:          map[string]*snap.AppInfo{"app1": app1, "app2": app2, "app3": app3},
			},
			Name:      "network-manager",
			Interface: "network-manager",
			Apps:      map[string]*snap.AppInfo{"app1": app1, "app2": app2},
		},
	}
	release.OnClassic = false
	snippet, err := s.iface.ConnectedPlugSnippet(s.plug, slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(string(snippet), testutil.Contains, `peer=(label="snap.network-manager.{app1,app2}"),`)
}

// The label uses short form when exactly one app is bound to the network-manager slot
func (s *NetworkManagerInterfaceSuite) TestConnectedPlugSnippetUsesSlotLabelOne(c *C) {
	app := &snap.AppInfo{Name: "app"}
	slot := &interfaces.Slot{
		SlotInfo: &snap.SlotInfo{
			Snap: &snap.Info{
				SuggestedName: "network-manager",
				Apps:          map[string]*snap.AppInfo{"app": app},
			},
			Name:      "network-manager",
			Interface: "network-manager",
			Apps:      map[string]*snap.AppInfo{"app": app},
		},
	}
	release.OnClassic = false
	snippet, err := s.iface.ConnectedPlugSnippet(s.plug, slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(string(snippet), testutil.Contains, `peer=(label="snap.network-manager.app"),`)
}

func (s *NetworkManagerInterfaceSuite) TestConnectedPlugSnippedUsesUnconfinedLabelOnClassic(c *C) {
	slot := &interfaces.Slot{}
	release.OnClassic = true
	snippet, err := s.iface.ConnectedPlugSnippet(s.plug, slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(string(snippet), testutil.Contains, "peer=(label=unconfined),")
}

func (s *NetworkManagerInterfaceSuite) TestUsedSecuritySystems(c *C) {
	snippet, err := s.iface.ConnectedPlugSnippet(s.plug, s.slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, Not(IsNil))
	snippet, err = s.iface.PermanentSlotSnippet(s.slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, Not(IsNil))
	snippet, err = s.iface.ConnectedPlugSnippet(s.plug, s.slot, interfaces.SecurityDBus)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
	snippet, err = s.iface.PermanentSlotSnippet(s.slot, interfaces.SecurityDBus)
	c.Assert(err, IsNil)
	c.Assert(snippet, Not(IsNil))
	snippet, err = s.iface.PermanentSlotSnippet(s.slot, interfaces.SecuritySecComp)
	c.Assert(err, IsNil)
	c.Assert(snippet, Not(IsNil))
}
