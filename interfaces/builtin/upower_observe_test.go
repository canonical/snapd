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
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type UPowerObserveInterfaceSuite struct {
	iface           interfaces.Interface
	coreSlotInfo    *snap.SlotInfo
	coreSlot        *interfaces.ConnectedSlot
	classicSlotInfo *snap.SlotInfo
	classicSlot     *interfaces.ConnectedSlot
	plugInfo        *snap.PlugInfo
	plug            *interfaces.ConnectedPlug
}

var _ = Suite(&UPowerObserveInterfaceSuite{
	iface: builtin.MustInterface("upower-observe"),
})

func (s *UPowerObserveInterfaceSuite) SetUpTest(c *C) {
	const mockPlugSnapInfoYaml = `name: other
version: 1.0
apps:
 app:
  command: foo
  plugs: [upower-observe]
`
	const upowerMockClassicSlotSnapInfoYaml = `name: core
version: 0
type: os
slots:
 upower-observe:
  interface: upower-observe
`
	const upowerMockSlotSnapInfoYaml = `name: upowerd
version: 1.0
slots:
 upower-observe:
  interface: upower-observe
apps:
 app:
  command: foo
  slots: [upower-observe]
`
	// upower snap with upower-server slot on an core/all-snap install.
	snapInfo := snaptest.MockInfo(c, upowerMockSlotSnapInfoYaml, nil)
	s.coreSlotInfo = snapInfo.Slots["upower-observe"]
	s.coreSlot = interfaces.NewConnectedSlot(s.coreSlotInfo, nil, nil)
	// upower-observe slot on a core snap in a classic install.
	snapInfo = snaptest.MockInfo(c, upowerMockClassicSlotSnapInfoYaml, nil)
	s.classicSlotInfo = snapInfo.Slots["upower-observe"]
	s.classicSlot = interfaces.NewConnectedSlot(s.classicSlotInfo, nil, nil)
	// snap with the upower-observe plug
	snapInfo = snaptest.MockInfo(c, mockPlugSnapInfoYaml, nil)
	s.plugInfo = snapInfo.Plugs["upower-observe"]
	s.plug = interfaces.NewConnectedPlug(s.plugInfo, nil, nil)
}

func (s *UPowerObserveInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "upower-observe")
}

func (s *UPowerObserveInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.coreSlotInfo), IsNil)
}

func (s *UPowerObserveInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

// The label glob when all apps are bound to the ofono slot
func (s *UPowerObserveInterfaceSuite) TestConnectedPlugSnippetUsesSlotLabelAll(c *C) {
	app1 := &snap.AppInfo{Name: "app1"}
	app2 := &snap.AppInfo{Name: "app2"}
	slot := interfaces.NewConnectedSlot(&snap.SlotInfo{
		Snap: &snap.Info{
			SuggestedName: "upower",
			Apps:          map[string]*snap.AppInfo{"app1": app1, "app2": app2},
		},
		Name:      "upower",
		Interface: "upower-observe",
		Apps:      map[string]*snap.AppInfo{"app1": app1, "app2": app2},
	}, nil, nil)

	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	apparmorSpec := apparmor.NewSpecification(appSet)
	err = apparmorSpec.AddConnectedPlug(s.iface, s.plug, slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app"), testutil.Contains, `peer=(label="snap.upower.*"),`)
}

// The label uses alternation when some, but not all, apps is bound to the ofono slot
func (s *UPowerObserveInterfaceSuite) TestConnectedPlugSnippetUsesSlotLabelSome(c *C) {
	app1 := &snap.AppInfo{Name: "app1"}
	app2 := &snap.AppInfo{Name: "app2"}
	app3 := &snap.AppInfo{Name: "app3"}
	slot := interfaces.NewConnectedSlot(&snap.SlotInfo{
		Snap: &snap.Info{
			SuggestedName: "upower",
			Apps:          map[string]*snap.AppInfo{"app1": app1, "app2": app2, "app3": app3},
		},
		Name:      "upower",
		Interface: "upower",
		Apps:      map[string]*snap.AppInfo{"app1": app1, "app2": app2},
	}, nil, nil)

	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	apparmorSpec := apparmor.NewSpecification(appSet)
	err = apparmorSpec.AddConnectedPlug(s.iface, s.plug, slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app"), testutil.Contains, `peer=(label="snap.upower.{app1,app2}"),`)
}

// The label uses short form when exactly one app is bound to the upower-observe slot
func (s *UPowerObserveInterfaceSuite) TestConnectedPlugSnippetUsesSlotLabelOne(c *C) {
	app := &snap.AppInfo{Name: "app"}
	slot := interfaces.NewConnectedSlot(&snap.SlotInfo{
		Snap: &snap.Info{
			SuggestedName: "upower",
			Apps:          map[string]*snap.AppInfo{"app": app},
		},
		Name:      "upower",
		Interface: "upower",
		Apps:      map[string]*snap.AppInfo{"app": app},
	}, nil, nil)

	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	apparmorSpec := apparmor.NewSpecification(appSet)
	err = apparmorSpec.AddConnectedPlug(s.iface, s.plug, slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app"), testutil.Contains, `peer=(label="snap.upower.app"),`)
}

func (s *UPowerObserveInterfaceSuite) TestConnectedPlugSnippetUsesUnconfinedLabelOnClassic(c *C) {
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	apparmorSpec := apparmor.NewSpecification(appSet)
	err = apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.classicSlot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app"})
	snippet := apparmorSpec.SnippetForTag("snap.other.app")
	// verify apparmor connected
	c.Assert(string(snippet), testutil.Contains, "#include <abstractions/dbus-strict>")
	// verify classic connected
	c.Assert(string(snippet), testutil.Contains, "peer=(label=unconfined),")
}

func (s *UPowerObserveInterfaceSuite) TestConnectedPlugSnippetAppArmor(c *C) {
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	apparmorSpec := apparmor.NewSpecification(appSet)
	err = apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.coreSlot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app"})
	snippet := apparmorSpec.SnippetForTag("snap.other.app")
	// verify apparmor connected
	c.Assert(string(snippet), testutil.Contains, "#include <abstractions/dbus-strict>")
	// verify classic didn't connect
	c.Assert(string(snippet), Not(testutil.Contains), "peer=(label=unconfined),")
}

func (s *UPowerObserveInterfaceSuite) TestPermanentSlotSnippetAppArmor(c *C) {
	appSet, err := interfaces.NewSnapAppSet(s.coreSlotInfo.Snap, nil)
	c.Assert(err, IsNil)
	apparmorSpec := apparmor.NewSpecification(appSet)
	err = apparmorSpec.AddPermanentSlot(s.iface, s.coreSlotInfo)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.upowerd.app"})
	c.Check(apparmorSpec.SnippetForTag("snap.upowerd.app"), testutil.Contains, "org.freedesktop.UPower")
}

func (s *UPowerObserveInterfaceSuite) TestPermanentSlotSnippetSecComp(c *C) {
	appSet, err := interfaces.NewSnapAppSet(s.coreSlotInfo.Snap, nil)
	c.Assert(err, IsNil)
	seccompSpec := seccomp.NewSpecification(appSet)
	err = seccompSpec.AddPermanentSlot(s.iface, s.coreSlotInfo)
	c.Assert(err, IsNil)
	c.Assert(seccompSpec.SecurityTags(), DeepEquals, []string{"snap.upowerd.app"})
	c.Check(seccompSpec.SnippetForTag("snap.upowerd.app"), testutil.Contains, "bind\n")
}

func (s *UPowerObserveInterfaceSuite) TestConnectedSlotSnippetUsesPlugLabelOne(c *C) {
	app := &snap.AppInfo{Name: "app"}
	plug := interfaces.NewConnectedPlug(&snap.PlugInfo{
		Snap: &snap.Info{
			SuggestedName: "upower",
			Apps:          map[string]*snap.AppInfo{"app": app},
		},
		Name:      "upower",
		Interface: "upower-observe",
		Apps:      map[string]*snap.AppInfo{"app": app},
	}, nil, nil)

	appSet, err := interfaces.NewSnapAppSet(s.coreSlot.Snap(), nil)
	c.Assert(err, IsNil)
	apparmorSpec := apparmor.NewSpecification(appSet)
	err = apparmorSpec.AddConnectedSlot(s.iface, plug, s.coreSlot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.upowerd.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.upowerd.app"), testutil.Contains, `peer=(label="snap.upower.app"),`)
}

func (s *UPowerObserveInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Check(si.ImplicitOnCore, Equals, osutil.IsExecutable("/usr/libexec/upowerd"))
	c.Check(si.ImplicitOnClassic, Equals, true)
	c.Check(si.Summary, Equals, "allows operating as or reading from the UPower service")
	c.Check(si.BaseDeclarationSlots, testutil.Contains, "upower-observe")
}

func (s *UPowerObserveInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
