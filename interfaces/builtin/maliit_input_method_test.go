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

type MaliitInputMethodInterfaceSuite struct {
	iface interfaces.Interface
	slot  *interfaces.Slot
	plug  *interfaces.Plug
}

var _ = Suite(&MaliitInputMethodInterfaceSuite{
	iface: &builtin.MaliitInputMethodInterface{},
	slot: &interfaces.Slot{
		SlotInfo: &snap.SlotInfo{
			Snap:      &snap.Info{SuggestedName: "maliit-input-method"},
			Name:      "maliit-input-method-player",
			Interface: "maliit-input-method",
		},
	},
	plug: &interfaces.Plug{
		PlugInfo: &snap.PlugInfo{
			Snap:      &snap.Info{SuggestedName: "maliit-input-method"},
			Name:      "maliit-input-method-client",
			Interface: "maliit-input-method",
		},
	},
})

func (s *MaliitInputMethodInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "maliit-input-method")
}

// The label glob when all apps are bound to the maliit-input-method slot
func (s *MaliitInputMethodInterfaceSuite) TestConnectedPlugSnippetUsesSlotLabelAll(c *C) {
	app1 := &snap.AppInfo{Name: "app1"}
	app2 := &snap.AppInfo{Name: "app2"}
	slot := &interfaces.Slot{
		SlotInfo: &snap.SlotInfo{
			Snap: &snap.Info{
				SuggestedName: "maliit-input-method",
				Apps:          map[string]*snap.AppInfo{"app1": app1, "app2": app2},
			},
			Name:      "maliit-input-method",
			Interface: "maliit-input-method",
			Apps:      map[string]*snap.AppInfo{"app1": app1, "app2": app2},
		},
	}
	snippet, err := s.iface.ConnectedPlugSnippet(s.plug, slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(string(snippet), testutil.Contains, `peer=(label="snap.maliit-input-method.*"),`)
}

// The label uses alternation when some, but not all, apps are bound to the maliit-input-method slot
func (s *MaliitInputMethodInterfaceSuite) TestConnectedPlugSnippetUsesSlotLabelSome(c *C) {
	app1 := &snap.AppInfo{Name: "app1"}
	app2 := &snap.AppInfo{Name: "app2"}
	app3 := &snap.AppInfo{Name: "app3"}
	slot := &interfaces.Slot{
		SlotInfo: &snap.SlotInfo{
			Snap: &snap.Info{
				SuggestedName: "maliit-input-method",
				Apps:          map[string]*snap.AppInfo{"app1": app1, "app2": app2, "app3": app3},
			},
			Name:      "maliit-input-method",
			Interface: "maliit-input-method",
			Apps:      map[string]*snap.AppInfo{"app1": app1, "app2": app2},
		},
	}
	snippet, err := s.iface.ConnectedPlugSnippet(s.plug, slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(string(snippet), testutil.Contains, `peer=(label="snap.maliit-input-method.{app1,app2}"),`)
}

func (s *MaliitInputMethodInterfaceSuite) TestConnectedPlugSecComp(c *C) {
	snippet, err := s.iface.ConnectedPlugSnippet(s.plug, s.slot, interfaces.SecuritySecComp)
	c.Assert(err, IsNil)
	c.Assert(snippet, Not(IsNil))

	c.Check(string(snippet), testutil.Contains, "connect\n")
}

// The label uses short form when exactly one app is bound to the maliit-input-method slot
func (s *MaliitInputMethodInterfaceSuite) TestConnectedPlugSnippetUsesSlotLabelOne(c *C) {
	app := &snap.AppInfo{Name: "app"}
	slot := &interfaces.Slot{
		SlotInfo: &snap.SlotInfo{
			Snap: &snap.Info{
				SuggestedName: "maliit-input-method",
				Apps:          map[string]*snap.AppInfo{"app": app},
			},
			Name:      "maliit-input-method",
			Interface: "maliit-input-method",
			Apps:      map[string]*snap.AppInfo{"app": app},
		},
	}
	snippet, err := s.iface.ConnectedPlugSnippet(s.plug, slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(string(snippet), testutil.Contains, `peer=(label="snap.maliit-input-method.app"),`)
}

// The label glob when all apps are bound to the maliit-input-method plug
func (s *MaliitInputMethodInterfaceSuite) TestConnectedSlotSnippetUsesPlugLabelAll(c *C) {
	app1 := &snap.AppInfo{Name: "app1"}
	app2 := &snap.AppInfo{Name: "app2"}
	plug := &interfaces.Plug{
		PlugInfo: &snap.PlugInfo{
			Snap: &snap.Info{
				SuggestedName: "maliit-input-method",
				Apps:          map[string]*snap.AppInfo{"app1": app1, "app2": app2},
			},
			Name:      "maliit-input-method",
			Interface: "maliit-input-method",
			Apps:      map[string]*snap.AppInfo{"app1": app1, "app2": app2},
		},
	}
	snippet, err := s.iface.ConnectedSlotSnippet(plug, s.slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(string(snippet), testutil.Contains, `peer=(label="snap.maliit-input-method.*"),`)
}

// The label uses alternation when some, but not all, apps is bound to the maliit-input-method plug
func (s *MaliitInputMethodInterfaceSuite) TestConnectedSlotSnippetUsesPlugLabelSome(c *C) {
	app1 := &snap.AppInfo{Name: "app1"}
	app2 := &snap.AppInfo{Name: "app2"}
	app3 := &snap.AppInfo{Name: "app3"}
	plug := &interfaces.Plug{
		PlugInfo: &snap.PlugInfo{
			Snap: &snap.Info{
				SuggestedName: "maliit-input-method",
				Apps:          map[string]*snap.AppInfo{"app1": app1, "app2": app2, "app3": app3},
			},
			Name:      "maliit-input-method",
			Interface: "maliit-input-method",
			Apps:      map[string]*snap.AppInfo{"app1": app1, "app2": app2},
		},
	}
	snippet, err := s.iface.ConnectedSlotSnippet(plug, s.slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(string(snippet), testutil.Contains, `peer=(label="snap.maliit-input-method.{app1,app2}"),`)
}

// The label uses short form when exactly one app is bound to the maliit-input-method plug
func (s *MaliitInputMethodInterfaceSuite) TestConnectedSlotSnippetUsesPlugLabelOne(c *C) {
	app := &snap.AppInfo{Name: "app"}
	plug := &interfaces.Plug{
		PlugInfo: &snap.PlugInfo{
			Snap: &snap.Info{
				SuggestedName: "maliit-input-method",
				Apps:          map[string]*snap.AppInfo{"app": app},
			},
			Name:      "maliit-input-method",
			Interface: "maliit-input-method",
			Apps:      map[string]*snap.AppInfo{"app": app},
		},
	}
	snippet, err := s.iface.ConnectedSlotSnippet(plug, s.slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(string(snippet), testutil.Contains, `peer=(label="snap.maliit-input-method.app"),`)
}

func (s *MaliitInputMethodInterfaceSuite) TestPermanentSlotSecComp(c *C) {
	snippet, err := s.iface.PermanentSlotSnippet(s.slot, interfaces.SecuritySecComp)
	c.Assert(err, IsNil)
	c.Assert(snippet, Not(IsNil))

	c.Check(string(snippet), testutil.Contains, "listen\n")
}

func (s *MaliitInputMethodInterfaceSuite) TestUsedSecuritySystems(c *C) {
	systems := [...]interfaces.SecuritySystem{interfaces.SecurityAppArmor,
		interfaces.SecuritySecComp}
	for _, system := range systems {
		snippet, err := s.iface.ConnectedPlugSnippet(s.plug, s.slot, system)
		c.Assert(err, IsNil)
		c.Assert(snippet, Not(IsNil))
		snippet, err = s.iface.PermanentSlotSnippet(s.slot, system)
		c.Assert(err, IsNil)
		c.Assert(snippet, Not(IsNil))
	}
	snippet, err := s.iface.ConnectedSlotSnippet(s.plug, s.slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, Not(IsNil))
}
