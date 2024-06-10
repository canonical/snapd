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
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type Unity8ContactsInterfaceSuite struct {
	iface        interfaces.Interface
	slotInfo     *snap.SlotInfo
	slot         *interfaces.ConnectedSlot
	coreSlotInfo *snap.SlotInfo
	coreSlot     *interfaces.ConnectedSlot
	plugInfo     *snap.PlugInfo
	plug         *interfaces.ConnectedPlug
}

var _ = Suite(&Unity8ContactsInterfaceSuite{
	iface: builtin.MustInterface("unity8-contacts"),
})

func (s *Unity8ContactsInterfaceSuite) SetUpTest(c *C) {
	const mockPlugSnapInfo = `name: other
version: 1.0
apps:
 app:
  command: foo
  plugs: [unity8-contacts]
`

	const mockCoreSlotInfoYaml = `name: contacts
version: 1.0
apps:
 app:
  command: foo
  slots: [unity8-contacts]
`
	s.slotInfo = &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "core", SnapType: snap.TypeOS},
		Name:      "unity8-contacts",
		Interface: "unity8-contacts",
	}
	s.slot = interfaces.NewConnectedSlot(s.slotInfo, nil, nil)

	plugSnap := snaptest.MockInfo(c, mockPlugSnapInfo, nil)
	s.plugInfo = plugSnap.Plugs["unity8-contacts"]
	s.plug = interfaces.NewConnectedPlug(s.plugInfo, nil, nil)

	slotSnap := snaptest.MockInfo(c, mockCoreSlotInfoYaml, nil)
	s.coreSlotInfo = slotSnap.Slots["unity8-contacts"]
	s.coreSlot = interfaces.NewConnectedSlot(s.coreSlotInfo, nil, nil)
}

func (s *Unity8ContactsInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "unity8-contacts")
}

func (s *Unity8ContactsInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *Unity8ContactsInterfaceSuite) TestUsedSecuritySystems(c *C) {
	// connected plugs have a non-nil security snippet for apparmor
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	apparmorSpec := apparmor.NewSpecification(appSet)
	err = apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), HasLen, 1)
}

// The label glob when all apps are bound to the contacts slot
func (s *Unity8ContactsInterfaceSuite) TestConnectedPlugSnippetUsesSlotLabelAll(c *C) {
	app1 := &snap.AppInfo{Name: "app1"}
	app2 := &snap.AppInfo{Name: "app2"}
	slot := interfaces.NewConnectedSlot(&snap.SlotInfo{
		Snap: &snap.Info{
			SuggestedName: "unity8",
			Apps:          map[string]*snap.AppInfo{"app1": app1, "app2": app2},
		},
		Name:      "unity8-contacts",
		Interface: "unity8-contacts",
		Apps:      map[string]*snap.AppInfo{"app1": app1, "app2": app2},
	}, nil, nil)

	release.OnClassic = false

	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	apparmorSpec := apparmor.NewSpecification(appSet)
	err = apparmorSpec.AddConnectedPlug(s.iface, s.plug, slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app"), testutil.Contains, `peer=(label="snap.unity8.*"),`)
}

// The label uses alternation when some, but not all, apps is bound to the contacts slot
func (s *Unity8ContactsInterfaceSuite) TestConnectedPlugSnippetUsesSlotLabelSome(c *C) {
	app1 := &snap.AppInfo{Name: "app1"}
	app2 := &snap.AppInfo{Name: "app2"}
	app3 := &snap.AppInfo{Name: "app3"}
	slot := interfaces.NewConnectedSlot(&snap.SlotInfo{
		Snap: &snap.Info{
			SuggestedName: "unity8",
			Apps:          map[string]*snap.AppInfo{"app1": app1, "app2": app2, "app3": app3},
		},
		Name:      "unity8-contacts",
		Interface: "unity8-contacts",
		Apps:      map[string]*snap.AppInfo{"app1": app1, "app2": app2},
	}, nil, nil)

	release.OnClassic = false

	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	apparmorSpec := apparmor.NewSpecification(appSet)
	err = apparmorSpec.AddConnectedPlug(s.iface, s.plug, slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app"), testutil.Contains, `peer=(label="snap.unity8.{app1,app2}"),`)
}

// The label uses short form when exactly one app is bound to the calendar slot
func (s *Unity8ContactsInterfaceSuite) TestConnectedPlugSnippetUsesSlotLabelOne(c *C) {
	app := &snap.AppInfo{Name: "app"}
	slot := interfaces.NewConnectedSlot(&snap.SlotInfo{
		Snap: &snap.Info{
			SuggestedName: "unity8",
			Apps:          map[string]*snap.AppInfo{"app": app},
		},
		Name:      "unity8-contacts",
		Interface: "unity8-contacts",
		Apps:      map[string]*snap.AppInfo{"app": app},
	}, nil, nil)

	release.OnClassic = false

	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	apparmorSpec := apparmor.NewSpecification(appSet)
	err = apparmorSpec.AddConnectedPlug(s.iface, s.plug, slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app"), testutil.Contains, `peer=(label="snap.unity8.app"),`)
}

func (s *Unity8ContactsInterfaceSuite) TestConnectedPlugSnippetUsesUnconfinedLabelOnClassic(c *C) {
	release.OnClassic = true

	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	apparmorSpec := apparmor.NewSpecification(appSet)
	err = apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app"})
	snippet := apparmorSpec.SnippetForTag("snap.other.app")

	// verify apparmor connected
	c.Assert(snippet, testutil.Contains, "#include <abstractions/dbus-session-strict>")
	// verify classic connected
	c.Assert(snippet, testutil.Contains, "peer=(label=unconfined),")
}

func (s *Unity8ContactsInterfaceSuite) TestConnectedPlugSnippetAppArmor(c *C) {
	release.OnClassic = false

	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	apparmorSpec := apparmor.NewSpecification(appSet)
	err = apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app"})
	snippet := apparmorSpec.SnippetForTag("snap.other.app")
	// verify apparmor connected
	c.Assert(snippet, testutil.Contains, "#include <abstractions/dbus-session-strict>")
	// verify classic didn't connect
	c.Assert(snippet, Not(testutil.Contains), "peer=(label=unconfined),")
}

func (s *Unity8ContactsInterfaceSuite) TestConnectedSlotSnippetAppArmor(c *C) {
	appSet, err := interfaces.NewSnapAppSet(s.coreSlot.Snap(), nil)
	c.Assert(err, IsNil)
	apparmorSpec := apparmor.NewSpecification(appSet)
	err = apparmorSpec.AddConnectedSlot(s.iface, s.plug, s.coreSlot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.contacts.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.contacts.app"), testutil.Contains, "peer=(label=\"snap.other.app\")")
}

func (s *Unity8ContactsInterfaceSuite) TestPermanentSlotSnippetAppArmor(c *C) {
	appSet, err := interfaces.NewSnapAppSet(s.coreSlotInfo.Snap, nil)
	c.Assert(err, IsNil)
	apparmorSpec := apparmor.NewSpecification(appSet)
	err = apparmorSpec.AddPermanentSlot(s.iface, s.coreSlotInfo)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.contacts.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.contacts.app"), testutil.Contains, "name=\"org.gnome.evolution.dataserver.Sources5\"")
}

func (s *Unity8ContactsInterfaceSuite) TestPermanentSlotSnippetSecComp(c *C) {
	appSet, err := interfaces.NewSnapAppSet(s.coreSlotInfo.Snap, nil)
	c.Assert(err, IsNil)
	seccompSpec := seccomp.NewSpecification(appSet)
	err = seccompSpec.AddPermanentSlot(s.iface, s.coreSlotInfo)
	c.Assert(err, IsNil)
	c.Assert(seccompSpec.SecurityTags(), DeepEquals, []string{"snap.contacts.app"})
	c.Check(seccompSpec.SnippetForTag("snap.contacts.app"), testutil.Contains, "listen\n")
}

func (s *Unity8ContactsInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
