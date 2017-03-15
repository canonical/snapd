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

package builtin_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type MaliitInterfaceSuite struct {
	iface interfaces.Interface
	slot  *interfaces.Slot
	plug  *interfaces.Plug
}

var _ = Suite(&MaliitInterfaceSuite{
	iface: &builtin.MaliitInterface{},
	slot: &interfaces.Slot{
		SlotInfo: &snap.SlotInfo{
			Snap:      &snap.Info{SuggestedName: "maliit"},
			Name:      "maliit-server",
			Interface: "maliit",
		},
	},
	plug: &interfaces.Plug{
		PlugInfo: &snap.PlugInfo{
			Snap:      &snap.Info{SuggestedName: "maliit"},
			Name:      "maliit-client",
			Interface: "maliit",
		},
	},
})

func (s *MaliitInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "maliit")
}

// The label glob when all apps are bound to the maliit slot
func (s *MaliitInterfaceSuite) TestConnectedPlugSnippetUsesSlotLabelAll(c *C) {
	app1 := &snap.AppInfo{Name: "app1"}
	app2 := &snap.AppInfo{Name: "app2"}
	slot := &interfaces.Slot{
		SlotInfo: &snap.SlotInfo{
			Snap: &snap.Info{
				SuggestedName: "maliit",
				Apps:          map[string]*snap.AppInfo{"app1": app1, "app2": app2},
			},
			Name:      "maliit",
			Interface: "maliit",
			Apps:      map[string]*snap.AppInfo{"app1": app1, "app2": app2},
		},
	}
	snippet, err := s.iface.ConnectedPlugSnippet(s.plug, slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(string(snippet), testutil.Contains, `peer=(label="snap.maliit.*"),`)
}

// The label uses alternation when some, but not all, apps are bound to the maliit slot
func (s *MaliitInterfaceSuite) TestConnectedPlugSnippetUsesSlotLabelSome(c *C) {
	app1 := &snap.AppInfo{Name: "app1"}
	app2 := &snap.AppInfo{Name: "app2"}
	app3 := &snap.AppInfo{Name: "app3"}
	slot := &interfaces.Slot{
		SlotInfo: &snap.SlotInfo{
			Snap: &snap.Info{
				SuggestedName: "maliit",
				Apps:          map[string]*snap.AppInfo{"app1": app1, "app2": app2, "app3": app3},
			},
			Name:      "maliit",
			Interface: "maliit",
			Apps:      map[string]*snap.AppInfo{"app1": app1, "app2": app2},
		},
	}
	snippet, err := s.iface.ConnectedPlugSnippet(s.plug, slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(string(snippet), testutil.Contains, `peer=(label="snap.maliit.{app1,app2}"),`)
}

func (s *MaliitInterfaceSuite) TestConnectedPlugSecComp(c *C) {
	snippet, err := s.iface.ConnectedPlugSnippet(s.plug, s.slot, interfaces.SecuritySecComp)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
}

// The label uses short form when exactly one app is bound to the maliit slot
func (s *MaliitInterfaceSuite) TestConnectedPlugSnippetUsesSlotLabelOne(c *C) {
	app := &snap.AppInfo{Name: "app"}
	slot := &interfaces.Slot{
		SlotInfo: &snap.SlotInfo{
			Snap: &snap.Info{
				SuggestedName: "maliit",
				Apps:          map[string]*snap.AppInfo{"app": app},
			},
			Name:      "maliit",
			Interface: "maliit",
			Apps:      map[string]*snap.AppInfo{"app": app},
		},
	}
	snippet, err := s.iface.ConnectedPlugSnippet(s.plug, slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(string(snippet), testutil.Contains, `peer=(label="snap.maliit.app"),`)
}

// The label glob when all apps are bound to the maliit plug
func (s *MaliitInterfaceSuite) TestConnectedSlotSnippetUsesPlugLabelAll(c *C) {
	app1 := &snap.AppInfo{Name: "app1"}
	app2 := &snap.AppInfo{Name: "app2"}
	plug := &interfaces.Plug{
		PlugInfo: &snap.PlugInfo{
			Snap: &snap.Info{
				SuggestedName: "maliit",
				Apps:          map[string]*snap.AppInfo{"app1": app1, "app2": app2},
			},
			Name:      "maliit",
			Interface: "maliit",
			Apps:      map[string]*snap.AppInfo{"app1": app1, "app2": app2},
		},
	}
	snippet, err := s.iface.ConnectedSlotSnippet(plug, s.slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(string(snippet), testutil.Contains, `peer=(label="snap.maliit.*"),`)
}

// The label uses alternation when some, but not all, apps is bound to the maliit plug
func (s *MaliitInterfaceSuite) TestConnectedSlotSnippetUsesPlugLabelSome(c *C) {
	app1 := &snap.AppInfo{Name: "app1"}
	app2 := &snap.AppInfo{Name: "app2"}
	app3 := &snap.AppInfo{Name: "app3"}
	plug := &interfaces.Plug{
		PlugInfo: &snap.PlugInfo{
			Snap: &snap.Info{
				SuggestedName: "maliit",
				Apps:          map[string]*snap.AppInfo{"app1": app1, "app2": app2, "app3": app3},
			},
			Name:      "maliit",
			Interface: "maliit",
			Apps:      map[string]*snap.AppInfo{"app1": app1, "app2": app2},
		},
	}
	snippet, err := s.iface.ConnectedSlotSnippet(plug, s.slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(string(snippet), testutil.Contains, `peer=(label="snap.maliit.{app1,app2}"),`)
}

// The label uses short form when exactly one app is bound to the maliit plug
func (s *MaliitInterfaceSuite) TestConnectedSlotSnippetUsesPlugLabelOne(c *C) {
	app := &snap.AppInfo{Name: "app"}
	plug := &interfaces.Plug{
		PlugInfo: &snap.PlugInfo{
			Snap: &snap.Info{
				SuggestedName: "maliit",
				Apps:          map[string]*snap.AppInfo{"app": app},
			},
			Name:      "maliit",
			Interface: "maliit",
			Apps:      map[string]*snap.AppInfo{"app": app},
		},
	}
	snippet, err := s.iface.ConnectedSlotSnippet(plug, s.slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(string(snippet), testutil.Contains, `peer=(label="snap.maliit.app"),`)
}

func (s *MaliitInterfaceSuite) TestPermanentSlotSecComp(c *C) {
	snippet, err := s.iface.PermanentSlotSnippet(s.slot, interfaces.SecuritySecComp)
	c.Assert(err, IsNil)
	c.Assert(snippet, Not(IsNil))

	c.Check(string(snippet), testutil.Contains, "listen\n")
}

func (s *MaliitInterfaceSuite) TestUsedSecuritySystems(c *C) {
	// connected plugs have a non-nil security snippet for apparmor
	snippet, err := s.iface.ConnectedPlugSnippet(s.plug, s.slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, Not(IsNil))
	// connected plugs have a non-nil security snippet for seccomp
	snippet, err = s.iface.ConnectedPlugSnippet(s.plug, s.slot, interfaces.SecuritySecComp)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
}

func (s *MaliitInterfaceSuite) TestConnectedPlugSnippetAppArmor(c *C) {
	release.OnClassic = false
	snippet, err := s.iface.ConnectedPlugSnippet(s.plug, s.slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	// verify apparmor connected
	c.Assert(string(snippet), testutil.Contains, "#include <abstractions/dbus-session-strict>")
	// verify classic didn't connect
	c.Assert(string(snippet), Not(testutil.Contains), "peer=(label=unconfined),")
}

func (s *MaliitInterfaceSuite) TestConnectedPlugSnippetSecComp(c *C) {
	snippet, err := s.iface.ConnectedPlugSnippet(s.plug, s.slot, interfaces.SecuritySecComp)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
}

func (s *MaliitInterfaceSuite) TestPermanentSlotSnippetAppArmor(c *C) {
	snippet, err := s.iface.PermanentSlotSnippet(s.slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, Not(IsNil))
	c.Check(string(snippet), testutil.Contains, "org.maliit.Server.Address")
}

func (s *MaliitInterfaceSuite) TestPermanentSlotSnippetSecComp(c *C) {
	snippet, err := s.iface.PermanentSlotSnippet(s.slot, interfaces.SecuritySecComp)
	c.Assert(err, IsNil)
	c.Assert(snippet, Not(IsNil))
	c.Check(string(snippet), testutil.Contains, "listen\n")
}

func (s *MaliitInterfaceSuite) TestConnectedSlotSnippetAppArmor(c *C) {
	snippet, err := s.iface.ConnectedSlotSnippet(s.plug, s.slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, Not(IsNil))

	c.Check(string(snippet), testutil.Contains, "peer=(label=\"snap.maliit.*\")")
}
