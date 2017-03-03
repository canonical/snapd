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
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type NetworkManagerInterfaceSuite struct {
	iface interfaces.Interface
	slot  *interfaces.Slot
	plug  *interfaces.Plug
}

const netmgrMockSlotSnapInfoYaml = `name: network-manager
version: 1.0
slots:
 network-manager:
  interface: network-manager
apps:
 nm:
  command: foo
  slots:
   - network-manager
`

var _ = Suite(&NetworkManagerInterfaceSuite{})

func (s *NetworkManagerInterfaceSuite) SetUpTest(c *C) {
	s.iface = &builtin.NetworkManagerInterface{}
	s.plug = &interfaces.Plug{
		PlugInfo: &snap.PlugInfo{
			Snap:      &snap.Info{SuggestedName: "network-manager"},
			Name:      "nmcli",
			Interface: "network-manager",
		},
	}
	slotSnap := snaptest.MockInfo(c, netmgrMockSlotSnapInfoYaml, nil)
	s.slot = &interfaces.Slot{SlotInfo: slotSnap.Slots["network-manager"]}
}

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

func (s *NetworkManagerInterfaceSuite) TestConnectedSlotSnippetAppArmor(c *C) {
	snippet, err := s.iface.ConnectedSlotSnippet(s.plug, s.slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, Not(IsNil))

	c.Check(string(snippet), testutil.Contains, "peer=(label=\"snap.network-manager.*\")")
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
}

func (s *NetworkManagerInterfaceSuite) TestSecCompPermanentSlot(c *C) {
	seccompSpec := &seccomp.Specification{}
	err := seccompSpec.AddPermanentSlot(s.iface, s.slot)
	c.Assert(err, IsNil)
	snippets := seccompSpec.Snippets()
	c.Assert(len(snippets), Equals, 1)
	c.Assert(len(snippets["snap.network-manager.nm"]), Equals, 1)
	c.Check(string(snippets["snap.network-manager.nm"][0]), testutil.Contains, "listen\n")
}
