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

type MprisInterfaceSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

var _ = Suite(&MprisInterfaceSuite{
	iface: builtin.MustInterface("mpris"),
})

func (s *MprisInterfaceSuite) SetUpTest(c *C) {
	var mockPlugSnapInfoYaml = `name: other
version: 1.0
apps:
 app:
  command: foo
  plugs: [mpris]
`
	var mockSlotSnapInfoYaml = `name: mpris
version: 1.0
apps:
 app:
  command: foo
  slots: [mpris]
`

	snapInfo := snaptest.MockInfo(c, mockPlugSnapInfoYaml, nil)
	s.plugInfo = snapInfo.Plugs["mpris"]
	s.plug = interfaces.NewConnectedPlug(s.plugInfo, nil, nil)
	snapInfo = snaptest.MockInfo(c, mockSlotSnapInfoYaml, nil)
	s.slotInfo = snapInfo.Slots["mpris"]
	s.slot = interfaces.NewConnectedSlot(s.slotInfo, nil, nil)
}

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
	slot := info.Slots["mpris-slot"]
	name, err := builtin.MprisGetName(s.iface, slot.Attrs)
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
	slot := info.Slots["mpris-slot"]
	name, err := builtin.MprisGetName(s.iface, slot.Attrs)
	c.Assert(err, IsNil)
	c.Assert(name, Equals, "@{SNAP_INSTANCE_NAME}")
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
	slot := info.Slots["mpris-slot"]
	name, err := builtin.MprisGetName(s.iface, slot.Attrs)
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
	slot := info.Slots["mpris-slot"]
	name, err := builtin.MprisGetName(s.iface, slot.Attrs)
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
	slot := info.Slots["mpris-slot"]
	name, err := builtin.MprisGetName(s.iface, slot.Attrs)
	c.Assert(err, Not(IsNil))
	c.Assert(err, ErrorMatches, "unknown attribute 'unknown'")
	c.Assert(name, Equals, "")
}

// The label glob when all apps are bound to the mpris slot
func (s *MprisInterfaceSuite) TestConnectedPlugSnippetUsesSlotLabelAll(c *C) {
	app1 := &snap.AppInfo{Name: "app1"}
	app2 := &snap.AppInfo{Name: "app2"}
	slot := interfaces.NewConnectedSlot(&snap.SlotInfo{
		Snap: &snap.Info{
			SuggestedName: "mpris",
			Apps:          map[string]*snap.AppInfo{"app1": app1, "app2": app2},
		},
		Name:      "mpris",
		Interface: "mpris",
		Apps:      map[string]*snap.AppInfo{"app1": app1, "app2": app2},
	}, nil, nil)

	apparmorSpec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap()))
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app"), testutil.Contains, `peer=(label="snap.mpris.*"),`)
}

// The label uses alternation when some, but not all, apps are bound to the mpris slot
func (s *MprisInterfaceSuite) TestConnectedPlugSnippetUsesSlotLabelSome(c *C) {
	app1 := &snap.AppInfo{Name: "app1"}
	app2 := &snap.AppInfo{Name: "app2"}
	app3 := &snap.AppInfo{Name: "app3"}
	slot := interfaces.NewConnectedSlot(&snap.SlotInfo{
		Snap: &snap.Info{
			SuggestedName: "mpris",
			Apps:          map[string]*snap.AppInfo{"app1": app1, "app2": app2, "app3": app3},
		},
		Name:      "mpris",
		Interface: "mpris",
		Apps:      map[string]*snap.AppInfo{"app1": app1, "app2": app2},
	}, nil, nil)

	apparmorSpec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap()))
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app"), testutil.Contains, `peer=(label="snap.mpris.{app1,app2}"),`)
}

// The label uses short form when exactly one app is bound to the mpris slot
func (s *MprisInterfaceSuite) TestConnectedPlugSnippetUsesSlotLabelOne(c *C) {
	app := &snap.AppInfo{Name: "app"}
	slot := interfaces.NewConnectedSlot(&snap.SlotInfo{
		Snap: &snap.Info{
			SuggestedName: "mpris",
			Apps:          map[string]*snap.AppInfo{"app": app},
		},
		Name:      "mpris",
		Interface: "mpris",
		Apps:      map[string]*snap.AppInfo{"app": app},
	}, nil, nil)

	apparmorSpec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap()))
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app"), testutil.Contains, `peer=(label="snap.mpris.app"),`)
}

// The label glob when all apps are bound to the mpris plug
func (s *MprisInterfaceSuite) TestConnectedSlotSnippetUsesPlugLabelAll(c *C) {
	app1 := &snap.AppInfo{Name: "app1"}
	app2 := &snap.AppInfo{Name: "app2"}
	plug := interfaces.NewConnectedPlug(&snap.PlugInfo{
		Snap: &snap.Info{
			SuggestedName: "mpris",
			Apps:          map[string]*snap.AppInfo{"app1": app1, "app2": app2},
		},
		Name:      "mpris",
		Interface: "mpris",
		Apps:      map[string]*snap.AppInfo{"app1": app1, "app2": app2},
	}, nil, nil)

	apparmorSpec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.slot.Snap()))
	err := apparmorSpec.AddConnectedSlot(s.iface, plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.mpris.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.mpris.app"), testutil.Contains, `peer=(label="snap.mpris.*"),`)
}

// The label uses alternation when some, but not all, apps is bound to the mpris plug
func (s *MprisInterfaceSuite) TestConnectedSlotSnippetUsesPlugLabelSome(c *C) {
	app1 := &snap.AppInfo{Name: "app1"}
	app2 := &snap.AppInfo{Name: "app2"}
	app3 := &snap.AppInfo{Name: "app3"}
	plug := interfaces.NewConnectedPlug(&snap.PlugInfo{
		Snap: &snap.Info{
			SuggestedName: "mpris",
			Apps:          map[string]*snap.AppInfo{"app1": app1, "app2": app2, "app3": app3},
		},
		Name:      "mpris",
		Interface: "mpris",
		Apps:      map[string]*snap.AppInfo{"app1": app1, "app2": app2},
	}, nil, nil)

	apparmorSpec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.slot.Snap()))
	err := apparmorSpec.AddConnectedSlot(s.iface, plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.mpris.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.mpris.app"), testutil.Contains, `peer=(label="snap.mpris.{app1,app2}"),`)
}

// The label uses short form when exactly one app is bound to the mpris plug
func (s *MprisInterfaceSuite) TestConnectedSlotSnippetUsesPlugLabelOne(c *C) {
	app := &snap.AppInfo{Name: "app"}
	plug := interfaces.NewConnectedPlug(&snap.PlugInfo{
		Snap: &snap.Info{
			SuggestedName: "mpris",
			Apps:          map[string]*snap.AppInfo{"app": app},
		},
		Name:      "mpris",
		Interface: "mpris",
		Apps:      map[string]*snap.AppInfo{"app": app},
	}, nil, nil)

	apparmorSpec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.slot.Snap()))
	err := apparmorSpec.AddConnectedSlot(s.iface, plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.mpris.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.mpris.app"), testutil.Contains, `peer=(label="snap.mpris.app"),`)
}

func (s *MprisInterfaceSuite) TestPermanentSlotAppArmor(c *C) {
	apparmorSpec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.slotInfo.Snap))
	err := apparmorSpec.AddPermanentSlot(s.iface, s.slotInfo)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.mpris.app"})

	// verify bind rule
	c.Assert(apparmorSpec.SnippetForTag("snap.mpris.app"), testutil.Contains, "dbus (bind)\n    bus=session\n    name=\"org.mpris.MediaPlayer2.@{SNAP_INSTANCE_NAME}{,.*}\",\n")
}

func (s *MprisInterfaceSuite) TestPermanentSlotAppArmorWithName(c *C) {
	const mockSnapYaml = `name: mpris-client
version: 1.0
slots:
 mpris-slot:
  interface: mpris
  name: foo
apps:
 app:
  command: foo
`
	info := snaptest.MockInfo(c, mockSnapYaml, nil)
	slot := info.Slots["mpris-slot"]

	apparmorSpec := apparmor.NewSpecification(interfaces.NewSnapAppSet(slot.Snap))
	err := apparmorSpec.AddPermanentSlot(s.iface, slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.mpris-client.app"})

	// verify bind rule
	c.Assert(apparmorSpec.SnippetForTag("snap.mpris-client.app"), testutil.Contains, "dbus (bind)\n    bus=session\n    name=\"org.mpris.MediaPlayer2.foo{,.*}\",\n")
}

func (s *MprisInterfaceSuite) TestPermanentSlotAppArmorNative(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	apparmorSpec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.slotInfo.Snap))
	err := apparmorSpec.AddPermanentSlot(s.iface, s.slotInfo)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.mpris.app"})

	// verify classic rule not present
	c.Assert(apparmorSpec.SnippetForTag("snap.mpris.app"), Not(testutil.Contains), "# Allow unconfined clients to interact with the player on classic\n")
}

func (s *MprisInterfaceSuite) TestPermanentSlotAppArmorClassic(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	apparmorSpec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.slotInfo.Snap))
	err := apparmorSpec.AddPermanentSlot(s.iface, s.slotInfo)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.mpris.app"})

	// verify classic rule present
	c.Assert(apparmorSpec.SnippetForTag("snap.mpris.app"), testutil.Contains, "# Allow unconfined clients to interact with the player on classic\n")
}

func (s *MprisInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
