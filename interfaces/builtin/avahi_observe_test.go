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
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type AvahiObserveInterfaceSuite struct {
	iface interfaces.Interface
	slot  *interfaces.Slot
	plug  *interfaces.Plug
}

var _ = Suite(&AvahiObserveInterfaceSuite{
	iface: builtin.MustInterface("avahi-observe"),
})

const avahiMockPlugSnapInfoYaml = `name: other
version: 1.0
apps:
 app:
  command: foo
  plugs: [avahi-observe]
`
const avahiMockSlotSnapInfoYaml = `name: avahi
version: 1.0
apps:
 app1:
  command: foo
  slots: [avahi-observe]
`

func (s *AvahiObserveInterfaceSuite) SetUpTest(c *C) {
	slotSnap := snaptest.MockInfo(c, avahiMockSlotSnapInfoYaml, nil)
	s.slot = &interfaces.Slot{SlotInfo: slotSnap.Slots["avahi-observe"]}
	plugSnap := snaptest.MockInfo(c, avahiMockPlugSnapInfoYaml, nil)
	s.plug = &interfaces.Plug{PlugInfo: plugSnap.Plugs["avahi-observe"]}
}

func (s *AvahiObserveInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "avahi-observe")
}

// The label glob when all apps are bound to the avahi slot
func (s *AvahiObserveInterfaceSuite) TestConnectedPlugSnippetUsesSlotLabelAll(c *C) {
	app1 := &snap.AppInfo{Name: "app1"}
	app2 := &snap.AppInfo{Name: "app2"}
	slot := &interfaces.Slot{
		SlotInfo: &snap.SlotInfo{
			Snap: &snap.Info{
				SuggestedName: "avahi-observe",
				Apps:          map[string]*snap.AppInfo{"app1": app1, "app2": app2},
			},
			Name:      "avahi-observe",
			Interface: "avahi-observe",
			Apps:      map[string]*snap.AppInfo{"app1": app1, "app2": app2},
		},
	}
	restore := release.MockOnClassic(false)
	defer restore()

	plugSnap := snaptest.MockInfo(c, avahiMockPlugSnapInfoYaml, nil)
	plug := &interfaces.Plug{PlugInfo: plugSnap.Plugs["avahi-observe"]}

	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, plug, nil, slot, nil)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app"), testutil.Contains, `peer=(label="snap.avahi-observe.*"),`)
}

// The label glob when all apps are bound to the avahi slot
func (s *AvahiObserveInterfaceSuite) TestConnectedPlugSnippetUsesSlotLabelSome(c *C) {
	app1 := &snap.AppInfo{Name: "app1"}
	app2 := &snap.AppInfo{Name: "app2"}
	app3 := &snap.AppInfo{Name: "app3"}
	slot := &interfaces.Slot{
		SlotInfo: &snap.SlotInfo{
			Snap: &snap.Info{
				SuggestedName: "avahi-observe",
				Apps:          map[string]*snap.AppInfo{"app1": app1, "app2": app2, "app3": app3},
			},
			Name:      "avahi-observe",
			Interface: "avahi-observe",
			Apps:      map[string]*snap.AppInfo{"app1": app1, "app2": app2},
		},
	}
	restore := release.MockOnClassic(false)
	defer restore()

	plugSnap := snaptest.MockInfo(c, avahiMockPlugSnapInfoYaml, nil)
	plug := &interfaces.Plug{PlugInfo: plugSnap.Plugs["avahi-observe"]}

	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, plug, nil, slot, nil)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app"), testutil.Contains, `peer=(label="snap.avahi-observe.{app1,app2}"),`)
}

// The label glob when all apps are bound to the avahi slot
func (s *AvahiObserveInterfaceSuite) TestConnectedPlugSnippetUsesSlotLabelOne(c *C) {
	app := &snap.AppInfo{Name: "app"}
	slot := &interfaces.Slot{
		SlotInfo: &snap.SlotInfo{
			Snap: &snap.Info{
				SuggestedName: "avahi-observe",
				Apps:          map[string]*snap.AppInfo{"app": app},
			},
			Name:      "avahi-observe",
			Interface: "avahi-observe",
			Apps:      map[string]*snap.AppInfo{"app": app},
		},
	}
	restore := release.MockOnClassic(false)
	defer restore()

	plugSnap := snaptest.MockInfo(c, avahiMockPlugSnapInfoYaml, nil)
	plug := &interfaces.Plug{PlugInfo: plugSnap.Plugs["avahi-observe"]}

	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, plug, nil, slot, nil)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app"), testutil.Contains, `peer=(label="snap.avahi-observe.app"),`)
}

func (s *AvahiObserveInterfaceSuite) TestConnectedPlugSnippedUsesUnconfinedLabelOnClassic(c *C) {
	slot := &interfaces.Slot{}
	restore := release.MockOnClassic(true)
	defer restore()
	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, nil, slot, nil)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app"), testutil.Contains, "peer=(label=unconfined),")
}

func (s *AvahiObserveInterfaceSuite) TestConnectedSlotSnippetAppArmor(c *C) {
	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedSlot(s.iface, s.plug, nil, s.slot, nil)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.avahi.app1"})
	c.Assert(apparmorSpec.SnippetForTag("snap.avahi.app1"), testutil.Contains, `interface=org.freedesktop.Avahi`)
}

func (s *AvahiObserveInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(s.slot.Sanitize(s.iface), IsNil)
	// avahi-observe slot can now be used on snap other than core.
	slot := &interfaces.Slot{SlotInfo: &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "some-snap"},
		Name:      "avahi-observe",
		Interface: "avahi-observe",
	}}
	c.Assert(slot.Sanitize(s.iface), IsNil)
}

func (s *AvahiObserveInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(s.plug.Sanitize(s.iface), IsNil)
}

func (s *AvahiObserveInterfaceSuite) TestUsedSecuritySystems(c *C) {
	// connected plugs have a non-nil security snippet for apparmor
	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, nil, s.slot, nil)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app"})
	c.Check(apparmorSpec.SnippetForTag("snap.other.app"), testutil.Contains, "name=org.freedesktop.Avahi")
}

func (s *AvahiObserveInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
