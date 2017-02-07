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
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type MprisInterfaceSuite struct {
	iface interfaces.Interface
	slot  *interfaces.Slot
	plug  *interfaces.Plug
}

var _ = Suite(&MprisInterfaceSuite{
	iface: &builtin.MprisInterface{},
	slot: &interfaces.Slot{
		SlotInfo: &snap.SlotInfo{
			Snap:      &snap.Info{SuggestedName: "mpris"},
			Name:      "mpris-player",
			Interface: "mpris",
		},
	},
	plug: &interfaces.Plug{
		PlugInfo: &snap.PlugInfo{
			Snap:      &snap.Info{SuggestedName: "mpris"},
			Name:      "mpris-client",
			Interface: "mpris",
		},
	},
})

func (s *MprisInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "mpris")
}

func (s *MprisInterfaceSuite) TestGetName(c *C) {
	const mockSnapYaml = `name: mpris-client
version: 1.0
slots:
 mpris-slot:
  interface: mpris
  name: foo
`
	info := snaptest.MockInfo(c, mockSnapYaml, nil)
	slot := &interfaces.Slot{SlotInfo: info.Slots["mpris-slot"]}
	iface := &builtin.MprisInterface{}
	name, err := builtin.MprisGetName(iface, slot.Attrs)
	c.Assert(err, IsNil)
	c.Assert(name, Equals, "foo")
}

func (s *MprisInterfaceSuite) TestGetNameMissing(c *C) {
	const mockSnapYaml = `name: mpris-client
version: 1.0
slots:
 mpris-slot:
  interface: mpris
`
	info := snaptest.MockInfo(c, mockSnapYaml, nil)
	slot := &interfaces.Slot{SlotInfo: info.Slots["mpris-slot"]}
	iface := &builtin.MprisInterface{}
	name, err := builtin.MprisGetName(iface, slot.Attrs)
	c.Assert(err, IsNil)
	c.Assert(name, Equals, "@{SNAP_NAME}")
}
func (s *MprisInterfaceSuite) TestGetNameBadDot(c *C) {
	const mockSnapYaml = `name: mpris-client
version: 1.0
slots:
 mpris-slot:
  interface: mpris
  name: foo.bar
`
	info := snaptest.MockInfo(c, mockSnapYaml, nil)
	slot := &interfaces.Slot{SlotInfo: info.Slots["mpris-slot"]}
	iface := &builtin.MprisInterface{}
	name, err := builtin.MprisGetName(iface, slot.Attrs)
	c.Assert(err, Not(IsNil))
	c.Assert(err, ErrorMatches, "invalid name element: \"foo.bar\"")
	c.Assert(name, Equals, "")
}

func (s *MprisInterfaceSuite) TestGetNameBadList(c *C) {
	const mockSnapYaml = `name: mpris-client
version: 1.0
slots:
 mpris-slot:
  interface: mpris
  name:
  - foo
`
	info := snaptest.MockInfo(c, mockSnapYaml, nil)
	slot := &interfaces.Slot{SlotInfo: info.Slots["mpris-slot"]}
	iface := &builtin.MprisInterface{}
	name, err := builtin.MprisGetName(iface, slot.Attrs)
	c.Assert(err, Not(IsNil))
	c.Assert(err, ErrorMatches, `name element \[foo\] is not a string`)
	c.Assert(name, Equals, "")
}

func (s *MprisInterfaceSuite) TestGetNameUnknownAttribute(c *C) {
	const mockSnapYaml = `name: mpris-client
version: 1.0
slots:
 mpris-slot:
  interface: mpris
  unknown: foo
`
	info := snaptest.MockInfo(c, mockSnapYaml, nil)
	slot := &interfaces.Slot{SlotInfo: info.Slots["mpris-slot"]}
	iface := &builtin.MprisInterface{}
	name, err := builtin.MprisGetName(iface, slot.Attrs)
	c.Assert(err, Not(IsNil))
	c.Assert(err, ErrorMatches, "unknown attribute 'unknown'")
	c.Assert(name, Equals, "")
}

// The label glob when all apps are bound to the mpris slot
func (s *MprisInterfaceSuite) TestConnectedPlugSnippetUsesSlotLabelAll(c *C) {
	app1 := &snap.AppInfo{Name: "app1"}
	app2 := &snap.AppInfo{Name: "app2"}
	slot := &interfaces.Slot{
		SlotInfo: &snap.SlotInfo{
			Snap: &snap.Info{
				SuggestedName: "mpris",
				Apps:          map[string]*snap.AppInfo{"app1": app1, "app2": app2},
			},
			Name:      "mpris",
			Interface: "mpris",
			Apps:      map[string]*snap.AppInfo{"app1": app1, "app2": app2},
		},
	}
	snippet, err := s.iface.ConnectedPlugSnippet(s.plug, slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(string(snippet), testutil.Contains, `peer=(label="snap.mpris.*"),`)
}

// The label uses alternation when some, but not all, apps are bound to the mpris slot
func (s *MprisInterfaceSuite) TestConnectedPlugSnippetUsesSlotLabelSome(c *C) {
	app1 := &snap.AppInfo{Name: "app1"}
	app2 := &snap.AppInfo{Name: "app2"}
	app3 := &snap.AppInfo{Name: "app3"}
	slot := &interfaces.Slot{
		SlotInfo: &snap.SlotInfo{
			Snap: &snap.Info{
				SuggestedName: "mpris",
				Apps:          map[string]*snap.AppInfo{"app1": app1, "app2": app2, "app3": app3},
			},
			Name:      "mpris",
			Interface: "mpris",
			Apps:      map[string]*snap.AppInfo{"app1": app1, "app2": app2},
		},
	}
	snippet, err := s.iface.ConnectedPlugSnippet(s.plug, slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(string(snippet), testutil.Contains, `peer=(label="snap.mpris.{app1,app2}"),`)
}

func (s *MprisInterfaceSuite) TestConnectedPlugSecComp(c *C) {
	snippet, err := s.iface.ConnectedPlugSnippet(s.plug, s.slot, interfaces.SecuritySecComp)
	c.Assert(err, IsNil)
	c.Assert(snippet, Not(IsNil))

	c.Check(string(snippet), testutil.Contains, "getsockname\n")
}

// The label uses short form when exactly one app is bound to the mpris slot
func (s *MprisInterfaceSuite) TestConnectedPlugSnippetUsesSlotLabelOne(c *C) {
	app := &snap.AppInfo{Name: "app"}
	slot := &interfaces.Slot{
		SlotInfo: &snap.SlotInfo{
			Snap: &snap.Info{
				SuggestedName: "mpris",
				Apps:          map[string]*snap.AppInfo{"app": app},
			},
			Name:      "mpris",
			Interface: "mpris",
			Apps:      map[string]*snap.AppInfo{"app": app},
		},
	}
	snippet, err := s.iface.ConnectedPlugSnippet(s.plug, slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(string(snippet), testutil.Contains, `peer=(label="snap.mpris.app"),`)
}

// The label glob when all apps are bound to the mpris plug
func (s *MprisInterfaceSuite) TestConnectedSlotSnippetUsesPlugLabelAll(c *C) {
	app1 := &snap.AppInfo{Name: "app1"}
	app2 := &snap.AppInfo{Name: "app2"}
	plug := &interfaces.Plug{
		PlugInfo: &snap.PlugInfo{
			Snap: &snap.Info{
				SuggestedName: "mpris",
				Apps:          map[string]*snap.AppInfo{"app1": app1, "app2": app2},
			},
			Name:      "mpris",
			Interface: "mpris",
			Apps:      map[string]*snap.AppInfo{"app1": app1, "app2": app2},
		},
	}
	snippet, err := s.iface.ConnectedSlotSnippet(plug, nil, s.slot, nil, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(string(snippet), testutil.Contains, `peer=(label="snap.mpris.*"),`)
}

// The label uses alternation when some, but not all, apps is bound to the mpris plug
func (s *MprisInterfaceSuite) TestConnectedSlotSnippetUsesPlugLabelSome(c *C) {
	app1 := &snap.AppInfo{Name: "app1"}
	app2 := &snap.AppInfo{Name: "app2"}
	app3 := &snap.AppInfo{Name: "app3"}
	plug := &interfaces.Plug{
		PlugInfo: &snap.PlugInfo{
			Snap: &snap.Info{
				SuggestedName: "mpris",
				Apps:          map[string]*snap.AppInfo{"app1": app1, "app2": app2, "app3": app3},
			},
			Name:      "mpris",
			Interface: "mpris",
			Apps:      map[string]*snap.AppInfo{"app1": app1, "app2": app2},
		},
	}
	snippet, err := s.iface.ConnectedSlotSnippet(plug, nil, s.slot, nil, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(string(snippet), testutil.Contains, `peer=(label="snap.mpris.{app1,app2}"),`)
}

// The label uses short form when exactly one app is bound to the mpris plug
func (s *MprisInterfaceSuite) TestConnectedSlotSnippetUsesPlugLabelOne(c *C) {
	app := &snap.AppInfo{Name: "app"}
	plug := &interfaces.Plug{
		PlugInfo: &snap.PlugInfo{
			Snap: &snap.Info{
				SuggestedName: "mpris",
				Apps:          map[string]*snap.AppInfo{"app": app},
			},
			Name:      "mpris",
			Interface: "mpris",
			Apps:      map[string]*snap.AppInfo{"app": app},
		},
	}
	snippet, err := s.iface.ConnectedSlotSnippet(plug, nil, s.slot, nil, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(string(snippet), testutil.Contains, `peer=(label="snap.mpris.app"),`)
}

func (s *MprisInterfaceSuite) TestPermanentSlotAppArmor(c *C) {
	snippet, err := s.iface.PermanentSlotSnippet(s.slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, Not(IsNil))

	// verify bind rule
	c.Check(string(snippet), testutil.Contains, "dbus (bind)\n    bus=session\n    name=\"org.mpris.MediaPlayer2.@{SNAP_NAME}{,.*}\",\n")
}

func (s *MprisInterfaceSuite) TestPermanentSlotAppArmorWithName(c *C) {
	const mockSnapYaml = `name: mpris-client
version: 1.0
slots:
 mpris-slot:
  interface: mpris
  name: foo
`
	info := snaptest.MockInfo(c, mockSnapYaml, nil)
	slot := &interfaces.Slot{SlotInfo: info.Slots["mpris-slot"]}
	iface := &builtin.MprisInterface{}
	snippet, err := iface.PermanentSlotSnippet(slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, Not(IsNil))

	// verify bind rule
	c.Check(string(snippet), testutil.Contains, "dbus (bind)\n    bus=session\n    name=\"org.mpris.MediaPlayer2.foo{,.*}\",\n")
}

func (s *MprisInterfaceSuite) TestPermanentSlotAppArmorNative(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()
	iface := &builtin.MprisInterface{}
	snippet, err := iface.PermanentSlotSnippet(s.slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, Not(IsNil))

	// verify classic rule not present
	c.Check(string(snippet), Not(testutil.Contains), "# Allow unconfined clients to interact with the player on classic\n")
}

func (s *MprisInterfaceSuite) TestPermanentSlotAppArmorClassic(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()
	iface := &builtin.MprisInterface{}
	snippet, err := iface.PermanentSlotSnippet(s.slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, Not(IsNil))

	// verify classic rule present
	c.Check(string(snippet), testutil.Contains, "# Allow unconfined clients to interact with the player on classic\n")
}

func (s *MprisInterfaceSuite) TestPermanentSlotSecComp(c *C) {
	snippet, err := s.iface.PermanentSlotSnippet(s.slot, interfaces.SecuritySecComp)
	c.Assert(err, IsNil)
	c.Assert(snippet, Not(IsNil))

	c.Check(string(snippet), testutil.Contains, "getsockname\n")
}

func (s *MprisInterfaceSuite) TestUsedSecuritySystems(c *C) {
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
	snippet, err := s.iface.ConnectedSlotSnippet(s.plug, nil, s.slot, nil, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, Not(IsNil))
}
