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
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type MaliitInterfaceSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

var _ = Suite(&MaliitInterfaceSuite{
	iface: builtin.MustInterface("maliit"),
})

func (s *MaliitInterfaceSuite) SetUpTest(c *C) {
	const mockPlugSnapInfoYaml = `name: other
version: 1.0
apps:
 app:
  command: foo
  plugs: [maliit]
`
	const mockCoreSlotSnapInfoYaml = `name: maliit
version: 1.0
apps:
 maliit:
  command: foo
  slots: [maliit]
`
	s.slot, s.slotInfo = MockConnectedSlot(c, mockCoreSlotSnapInfoYaml, nil, "maliit")
	s.plug, s.plugInfo = MockConnectedPlug(c, mockPlugSnapInfoYaml, nil, "maliit")
}

func (s *MaliitInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "maliit")
}

// The label glob when all apps are bound to the maliit slot
func (s *MaliitInterfaceSuite) TestConnectedPlugSnippetUsesSlotLabelAll(c *C) {
	appSet := appSetWithApps(c, "maliit", "app1", "app2")
	si := appSet.Info()
	slot := interfaces.NewConnectedSlot(&snap.SlotInfo{
		Snap:      si,
		Name:      "maliit",
		Interface: "maliit",
		Apps:      map[string]*snap.AppInfo{"app1": si.Apps["app1"], "app2": si.Apps["app2"]},
	}, appSet, nil, nil)

	apparmorSpec := apparmor.NewSpecification(s.plug.AppSet())
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app"), testutil.Contains, `peer=(label="snap.maliit.*"),`)
}

// The label uses alternation when some, but not all, apps are bound to the maliit slot
func (s *MaliitInterfaceSuite) TestConnectedPlugSnippetUsesSlotLabelSome(c *C) {
	appSet := appSetWithApps(c, "maliit", "app1", "app2", "app3")
	si := appSet.Info()
	slot := interfaces.NewConnectedSlot(&snap.SlotInfo{
		Snap:      si,
		Name:      "maliit",
		Interface: "maliit",
		Apps:      map[string]*snap.AppInfo{"app1": si.Apps["app1"], "app2": si.Apps["app2"]},
	}, appSet, nil, nil)

	apparmorSpec := apparmor.NewSpecification(s.plug.AppSet())
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app"), testutil.Contains, `peer=(label="snap.maliit{.app1,.app2}"),`)
}

func (s *MaliitInterfaceSuite) TestConnectedPlugSecComp(c *C) {
	seccompSpec := seccomp.NewSpecification(s.plug.AppSet())
	err := seccompSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(seccompSpec.SecurityTags(), HasLen, 0)
}

// The label uses short form when exactly one app is bound to the maliit slot
func (s *MaliitInterfaceSuite) TestConnectedPlugSnippetUsesSlotLabelOne(c *C) {
	appSet := appSetWithApps(c, "maliit", "app")
	si := appSet.Info()
	slot := interfaces.NewConnectedSlot(&snap.SlotInfo{
		Snap:      si,
		Name:      "maliit",
		Interface: "maliit",
		Apps:      map[string]*snap.AppInfo{"app": si.Apps["app"]},
	}, appSet, nil, nil)

	apparmorSpec := apparmor.NewSpecification(s.plug.AppSet())
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app"), testutil.Contains, `peer=(label="snap.maliit.app"),`)
}

// The label glob when all apps are bound to the maliit plug
func (s *MaliitInterfaceSuite) TestConnectedSlotSnippetUsesPlugLabelAll(c *C) {
	appSet := appSetWithApps(c, "maliit", "app1", "app2")
	si := appSet.Info()

	plug := interfaces.NewConnectedPlug(&snap.PlugInfo{
		Snap:      si,
		Name:      "maliit",
		Interface: "maliit",
		Apps:      map[string]*snap.AppInfo{"app1": si.Apps["app1"], "app2": si.Apps["app2"]},
	}, appSet, nil, nil)

	slot := interfaces.NewConnectedSlot(&snap.SlotInfo{
		Snap:      si,
		Name:      "maliit",
		Interface: "maliit",
		Apps:      map[string]*snap.AppInfo{"app1": si.Apps["app1"], "app2": si.Apps["app2"]},
	}, appSet, nil, nil)

	apparmorSpec := apparmor.NewSpecification(appSet)
	err := apparmorSpec.AddConnectedSlot(s.iface, plug, slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.maliit.app1", "snap.maliit.app2"})
	c.Assert(apparmorSpec.SnippetForTag("snap.maliit.app1"), testutil.Contains, `peer=(label="snap.maliit.*"),`)
	c.Assert(apparmorSpec.SnippetForTag("snap.maliit.app2"), testutil.Contains, `peer=(label="snap.maliit.*"),`)
}

// The label uses alternation when some, but not all, apps is bound to the maliit plug
func (s *MaliitInterfaceSuite) TestConnectedSlotSnippetUsesPlugLabelSome(c *C) {
	appSet := appSetWithApps(c, "maliit", "app1", "app2", "app3")
	si := appSet.Info()
	plug := interfaces.NewConnectedPlug(&snap.PlugInfo{
		Snap:      si,
		Name:      "maliit",
		Interface: "maliit",
		Apps:      map[string]*snap.AppInfo{"app1": si.Apps["app1"], "app2": si.Apps["app2"]},
	}, appSet, nil, nil)

	apparmorSpec := apparmor.NewSpecification(s.slot.AppSet())
	err := apparmorSpec.AddConnectedSlot(s.iface, plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.maliit.maliit"})
	c.Assert(apparmorSpec.SnippetForTag("snap.maliit.maliit"), testutil.Contains, `peer=(label="snap.maliit{.app1,.app2}"),`)
}

// The label uses short form when exactly one app is bound to the maliit plug
func (s *MaliitInterfaceSuite) TestConnectedSlotSnippetUsesPlugLabelOne(c *C) {
	appSet := appSetWithApps(c, "maliit", "app")
	si := appSet.Info()
	plug := interfaces.NewConnectedPlug(&snap.PlugInfo{
		Snap:      si,
		Name:      "maliit",
		Interface: "maliit",
		Apps:      map[string]*snap.AppInfo{"app": si.Apps["app"]},
	}, appSet, nil, nil)

	apparmorSpec := apparmor.NewSpecification(s.slot.AppSet())
	err := apparmorSpec.AddConnectedSlot(s.iface, plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.maliit.maliit"})
	c.Assert(apparmorSpec.SnippetForTag("snap.maliit.maliit"), testutil.Contains, `peer=(label="snap.maliit.app"),`)
}

func (s *MaliitInterfaceSuite) TestPermanentSlotSecComp(c *C) {
	seccompSpec := seccomp.NewSpecification(s.slot.AppSet())
	err := seccompSpec.AddPermanentSlot(s.iface, s.slotInfo)
	c.Assert(err, IsNil)
	c.Assert(seccompSpec.SecurityTags(), DeepEquals, []string{"snap.maliit.maliit"})
	c.Assert(seccompSpec.SnippetForTag("snap.maliit.maliit"), testutil.Contains, "listen\n")
}

func (s *MaliitInterfaceSuite) TestConnectedPlugSnippetAppArmor(c *C) {
	release.OnClassic = false

	apparmorSpec := apparmor.NewSpecification(s.plug.AppSet())
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app"})
	snippet := apparmorSpec.SnippetForTag("snap.other.app")
	// verify apparmor connected
	c.Assert(snippet, testutil.Contains, "#include <abstractions/dbus-session-strict>")
	// verify classic didn't connect
	c.Assert(snippet, Not(testutil.Contains), "peer=(label=unconfined),")
}

func (s *MaliitInterfaceSuite) TestPermanentSlotSnippetAppArmor(c *C) {
	apparmorSpec := apparmor.NewSpecification(s.slot.AppSet())
	err := apparmorSpec.AddPermanentSlot(s.iface, s.slotInfo)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.maliit.maliit"})
	c.Assert(apparmorSpec.SnippetForTag("snap.maliit.maliit"), testutil.Contains, "org.maliit.Server.Address")
}

func (s *MaliitInterfaceSuite) TestPermanentSlotSnippetSecComp(c *C) {
	seccompSpec := seccomp.NewSpecification(s.slot.AppSet())
	err := seccompSpec.AddPermanentSlot(s.iface, s.slotInfo)
	c.Assert(err, IsNil)
	c.Assert(seccompSpec.SecurityTags(), DeepEquals, []string{"snap.maliit.maliit"})
	c.Check(seccompSpec.SnippetForTag("snap.maliit.maliit"), testutil.Contains, "listen\n")
}

func (s *MaliitInterfaceSuite) TestConnectedSlotSnippetAppArmor(c *C) {
	apparmorSpec := apparmor.NewSpecification(s.slot.AppSet())
	err := apparmorSpec.AddConnectedSlot(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.maliit.maliit"})
	c.Assert(apparmorSpec.SnippetForTag("snap.maliit.maliit"), testutil.Contains, "peer=(label=\"snap.other.app\"")
}

func (s *MaliitInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
