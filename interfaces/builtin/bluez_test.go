// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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
	"github.com/snapcore/snapd/interfaces/dbus"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type BluezInterfaceSuite struct {
	iface interfaces.Interface
	slot  *interfaces.Slot
	plug  *interfaces.Plug
}

var _ = Suite(&BluezInterfaceSuite{
	iface: builtin.MustInterface("bluez"),
})

const bluezMockPlugSnapInfoYaml = `name: other
version: 1.0
apps:
 app2:
  command: foo
  plugs: [bluez]
`
const bluezMockSlotSnapInfoYaml = `name: bluez
version: 1.0
apps:
 app1:
  command: foo
  slots: [bluez]
`

func (s *BluezInterfaceSuite) SetUpTest(c *C) {
	slotSnap := snaptest.MockInfo(c, bluezMockSlotSnapInfoYaml, nil)
	s.slot = &interfaces.Slot{SlotInfo: slotSnap.Slots["bluez"]}
	plugSnap := snaptest.MockInfo(c, bluezMockPlugSnapInfoYaml, nil)
	s.plug = &interfaces.Plug{PlugInfo: plugSnap.Plugs["bluez"]}
}

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

	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, nil, slot, nil)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app2"})
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app2"), testutil.Contains, `peer=(label="snap.bluez.*"),`)
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

	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, nil, slot, nil)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app2"})
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app2"), testutil.Contains, `peer=(label="snap.bluez.{app1,app2}"),`)
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

	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, nil, slot, nil)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app2"})
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app2"), testutil.Contains, `peer=(label="snap.bluez.app"),`)
}

func (s *BluezInterfaceSuite) TestConnectedSlotSnippetAppArmor(c *C) {
	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedSlot(s.iface, s.plug, nil, s.slot, nil)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.bluez.app1"})
	c.Assert(apparmorSpec.SnippetForTag("snap.bluez.app1"), testutil.Contains, `peer=(label="snap.other.app2")`)
}

func (s *BluezInterfaceSuite) TestUsedSecuritySystems(c *C) {
	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, nil, s.slot, nil)
	c.Assert(err, IsNil)
	err = apparmorSpec.AddPermanentSlot(s.iface, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), HasLen, 2)

	dbusSpec := &dbus.Specification{}
	err = dbusSpec.AddPermanentSlot(s.iface, s.slot)
	c.Assert(err, IsNil)
	c.Assert(dbusSpec.SecurityTags(), DeepEquals, []string{"snap.bluez.app1"})
	c.Assert(dbusSpec.SnippetForTag("snap.bluez.app1"), testutil.Contains, `<allow own="org.bluez"/>`)

	seccompSpec := &seccomp.Specification{}
	err = seccompSpec.AddPermanentSlot(s.iface, s.slot)
	c.Assert(err, IsNil)
	c.Assert(seccompSpec.SecurityTags(), DeepEquals, []string{"snap.bluez.app1"})
	c.Check(seccompSpec.SnippetForTag("snap.bluez.app1"), testutil.Contains, "listen\n")

	expectedSnippet := `
# This file contains udev rules for bluez service.
#
# Do not edit this file, it will be overwritten on updates

KERNEL=="rfkill", TAG+="snap_other_app2"
`
	udevSpec := &udev.Specification{}
	c.Assert(udevSpec.AddConnectedPlug(s.iface, s.plug, nil, s.slot, nil), IsNil)
	c.Assert(udevSpec.Snippets(), HasLen, 1)
	snippet := udevSpec.Snippets()[0]
	c.Check(snippet, Equals, expectedSnippet)
}

func (s *BluezInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
