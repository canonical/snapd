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
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type UDisks2InterfaceSuite struct {
	iface interfaces.Interface
	slot  *interfaces.Slot
	plug  *interfaces.Plug
}

const udisks2mockPlugSnapInfoYaml = `name: udisks2
version: 1.0
apps:
 app:
  command: foo
  plugs: [udisks2]
`

const udisks2mockSlotSnapInfoYaml = `name: udisks2
version: 1.0
apps:
 app1:
  command: foo
  slots: [udisks2]
`

var _ = Suite(&UDisks2InterfaceSuite{})

func (s *UDisks2InterfaceSuite) SetUpTest(c *C) {
	s.iface = &builtin.UDisks2Interface{}
	slotSnap := snaptest.MockInfo(c, udisks2mockSlotSnapInfoYaml, nil)
	plugSnap := snaptest.MockInfo(c, udisks2mockPlugSnapInfoYaml, nil)
	s.slot = &interfaces.Slot{SlotInfo: slotSnap.Slots["udisks2"]}
	s.plug = &interfaces.Plug{PlugInfo: plugSnap.Plugs["udisks2"]}
}

func (s *UDisks2InterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "udisks2")
}

func (s *UDisks2InterfaceSuite) TestSanitizeSlot(c *C) {
	err := s.iface.SanitizeSlot(s.slot)
	c.Assert(err, IsNil)
}

// The label glob when all apps are bound to the udisks2 slot
func (s *UDisks2InterfaceSuite) TestConnectedPlugSnippetUsesSlotLabelAll(c *C) {
	app1 := &snap.AppInfo{Name: "app1"}
	app2 := &snap.AppInfo{Name: "app2"}
	slot := &interfaces.Slot{
		SlotInfo: &snap.SlotInfo{
			Snap: &snap.Info{
				SuggestedName: "udisks2prod",
				Apps:          map[string]*snap.AppInfo{"app1": app1, "app2": app2},
			},
			Name:      "udisks2",
			Interface: "udisks2",
			Apps:      map[string]*snap.AppInfo{"app1": app1, "app2": app2},
		},
	}

	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.udisks2.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.udisks2.app"), testutil.Contains, `peer=(label="snap.udisks2prod.*"),`)
}

// The label uses alternation when some, but not all, apps is bound to the udisks2 slot
func (s *UDisks2InterfaceSuite) TestConnectedPlugSnippetUsesSlotLabelSome(c *C) {
	app1 := &snap.AppInfo{Name: "app1"}
	app2 := &snap.AppInfo{Name: "app2"}
	app3 := &snap.AppInfo{Name: "app3"}
	slot := &interfaces.Slot{
		SlotInfo: &snap.SlotInfo{
			Snap: &snap.Info{
				SuggestedName: "udisks2prod",
				Apps:          map[string]*snap.AppInfo{"app1": app1, "app2": app2, "app3": app3},
			},
			Name:      "udisks2",
			Interface: "udisks2",
			Apps:      map[string]*snap.AppInfo{"app1": app1, "app2": app2},
		},
	}

	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.udisks2.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.udisks2.app"), testutil.Contains, `peer=(label="snap.udisks2prod.{app1,app2}"),`)
}

// The label uses short form when exactly one app is bound to the udisks2 slot
func (s *UDisks2InterfaceSuite) TestConnectedPlugSnippetUsesSlotLabelOne(c *C) {
	app := &snap.AppInfo{Name: "app"}
	slot := &interfaces.Slot{
		SlotInfo: &snap.SlotInfo{
			Snap: &snap.Info{
				SuggestedName: "udisks2",
				Apps:          map[string]*snap.AppInfo{"app": app},
			},
			Name:      "udisks2",
			Interface: "udisks2",
			Apps:      map[string]*snap.AppInfo{"app": app},
		},
	}

	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.udisks2.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.udisks2.app"), testutil.Contains, `peer=(label="snap.udisks2.app"),`)
}

// The label glob when all apps are bound to the udisks2 plug
func (s *UDisks2InterfaceSuite) TestConnectedSlotSnippetUsesPlugLabelAll(c *C) {
	app1 := &snap.AppInfo{Name: "app1"}
	app2 := &snap.AppInfo{Name: "app2"}
	plug := &interfaces.Plug{
		PlugInfo: &snap.PlugInfo{
			Snap: &snap.Info{
				SuggestedName: "udisks2client",
				Apps:          map[string]*snap.AppInfo{"app1": app1, "app2": app2},
			},
			Name:      "udisks2",
			Interface: "udisks2",
			Apps:      map[string]*snap.AppInfo{"app1": app1, "app2": app2},
		},
	}

	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedSlot(s.iface, plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.udisks2.app1"})
	c.Assert(apparmorSpec.SnippetForTag("snap.udisks2.app1"), testutil.Contains, `peer=(label="snap.udisks2client.*"),`)
}

// The label uses alternation when some, but not all, apps is bound to the udisks2 plug
func (s *UDisks2InterfaceSuite) TestConnectedSlotSnippetUsesPlugLabelSome(c *C) {
	app1 := &snap.AppInfo{Name: "app1"}
	app2 := &snap.AppInfo{Name: "app2"}
	app3 := &snap.AppInfo{Name: "app3"}
	plug := &interfaces.Plug{
		PlugInfo: &snap.PlugInfo{
			Snap: &snap.Info{
				SuggestedName: "udisks2",
				Apps:          map[string]*snap.AppInfo{"app1": app1, "app2": app2, "app3": app3},
			},
			Name:      "udisks2",
			Interface: "udisks2",
			Apps:      map[string]*snap.AppInfo{"app1": app1, "app2": app2},
		},
	}

	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedSlot(s.iface, plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.udisks2.app1"})
	c.Assert(apparmorSpec.SnippetForTag("snap.udisks2.app1"), testutil.Contains, `peer=(label="snap.udisks2.{app1,app2}"),`)
}

// The label uses short form when exactly one app is bound to the udisks2 plug
func (s *UDisks2InterfaceSuite) TestConnectedSlotSnippetUsesPlugLabelOne(c *C) {
	app := &snap.AppInfo{Name: "app"}
	plug := &interfaces.Plug{
		PlugInfo: &snap.PlugInfo{
			Snap: &snap.Info{
				SuggestedName: "udisks2",
				Apps:          map[string]*snap.AppInfo{"app": app},
			},
			Name:      "udisks2",
			Interface: "udisks2",
			Apps:      map[string]*snap.AppInfo{"app": app},
		},
	}

	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedSlot(s.iface, plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.udisks2.app1"})
	c.Assert(apparmorSpec.SnippetForTag("snap.udisks2.app1"), testutil.Contains, `peer=(label="snap.udisks2.app"),`)
}

func (s *UDisks2InterfaceSuite) TestUsedSecuritySystems(c *C) {
	systems := [...]interfaces.SecuritySystem{interfaces.SecurityDBus}
	for _, system := range systems {
		snippet, err := s.iface.ConnectedPlugSnippet(s.plug, s.slot, system)
		c.Assert(err, IsNil)
		c.Assert(snippet, Not(IsNil))
		snippet, err = s.iface.PermanentSlotSnippet(s.slot, system)
		c.Assert(err, IsNil)
		c.Assert(snippet, Not(IsNil))
	}

	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	err = apparmorSpec.AddPermanentSlot(s.iface, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), HasLen, 2)

	snippet, err := s.iface.PermanentSlotSnippet(s.slot, interfaces.SecurityUDev)
	c.Assert(err, IsNil)
	c.Assert(snippet, Not(IsNil))

	seccompSpec := &seccomp.Specification{}
	err = seccompSpec.AddPermanentSlot(s.iface, s.slot)
	c.Assert(err, IsNil)
	c.Assert(seccompSpec.SecurityTags(), DeepEquals, []string{"snap.udisks2.app1"})
	c.Check(seccompSpec.SnippetForTag("snap.udisks2.app1"), testutil.Contains, "mount\n")
}
