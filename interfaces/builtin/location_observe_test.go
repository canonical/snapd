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
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type LocationObserveInterfaceSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

var _ = Suite(&LocationObserveInterfaceSuite{
	iface: builtin.MustInterface("location-observe"),
})

func (s *LocationObserveInterfaceSuite) SetUpTest(c *C) {
	var mockPlugSnapInfoYaml = `name: other
version: 1.0
apps:
 app:
  command: foo
  plugs: [location-observe]
`
	var mockSlotSnapInfoYaml = `name: location
version: 1.0
apps:
 app2:
  command: foo
  slots: [location-observe]
`
	s.slot, s.slotInfo = MockConnectedSlot(c, mockSlotSnapInfoYaml, nil, "location-observe")
	s.plug, s.plugInfo = MockConnectedPlug(c, mockPlugSnapInfoYaml, nil, "location-observe")
}

func (s *LocationObserveInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "location-observe")
}

// The label glob when all apps are bound to the location slot
func (s *LocationObserveInterfaceSuite) TestConnectedPlugSnippetUsesSlotLabelAll(c *C) {
	appSet := appSetWithApps(c, "location", "app1", "app2")
	si := appSet.Info()
	slot := interfaces.NewConnectedSlot(&snap.SlotInfo{
		Snap:      si,
		Name:      "location",
		Interface: "location",
		Apps:      map[string]*snap.AppInfo{"app1": si.Apps["app1"], "app2": si.Apps["app2"]},
	}, appSet, nil, nil)

	apparmorSpec := apparmor.NewSpecification(s.plug.AppSet())
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app"), testutil.Contains, `peer=(label="snap.location.*"),`)
}

// The label uses alternation when some, but not all, apps is bound to the location slot
func (s *LocationObserveInterfaceSuite) TestConnectedPlugSnippetUsesSlotLabelSome(c *C) {
	appSet := appSetWithApps(c, "location", "app1", "app2", "app3")
	si := appSet.Info()
	slot := interfaces.NewConnectedSlot(&snap.SlotInfo{
		Snap:      si,
		Name:      "location",
		Interface: "location",
		Apps:      map[string]*snap.AppInfo{"app1": si.Apps["app1"], "app2": si.Apps["app2"]},
	}, appSet, nil, nil)

	apparmorSpec := apparmor.NewSpecification(s.plug.AppSet())
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app"), testutil.Contains, `peer=(label="snap.location{.app1,.app2}"),`)
}

// The label uses short form when exactly one app is bound to the location slot
func (s *LocationObserveInterfaceSuite) TestConnectedPlugSnippetUsesSlotLabelOne(c *C) {
	appSet := appSetWithApps(c, "location", "app")
	si := appSet.Info()
	slot := interfaces.NewConnectedSlot(&snap.SlotInfo{
		Snap:      si,
		Name:      "location",
		Interface: "location",
		Apps:      map[string]*snap.AppInfo{"app": si.Apps["app"]},
	}, appSet, nil, nil)

	apparmorSpec := apparmor.NewSpecification(s.plug.AppSet())
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app"), testutil.Contains, `peer=(label="snap.location.app"),`)
}

// The label glob when all apps are bound to the location plug
func (s *LocationObserveInterfaceSuite) TestConnectedSlotSnippetUsesPlugLabelAll(c *C) {
	appSet := appSetWithApps(c, "location", "app1", "app2")
	si := appSet.Info()

	plug := interfaces.NewConnectedPlug(&snap.PlugInfo{
		Snap:      si,
		Name:      "location",
		Interface: "location",
		Apps:      map[string]*snap.AppInfo{"app1": si.Apps["app1"], "app2": si.Apps["app2"]},
	}, appSet, nil, nil)

	slot := interfaces.NewConnectedSlot(&snap.SlotInfo{
		Snap:      si,
		Name:      "location",
		Interface: "location",
		Apps:      map[string]*snap.AppInfo{"app1": si.Apps["app1"], "app2": si.Apps["app2"]},
	}, appSet, nil, nil)

	apparmorSpec := apparmor.NewSpecification(appSet)
	err := apparmorSpec.AddConnectedSlot(s.iface, plug, slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.location.app1", "snap.location.app2"})
	c.Assert(apparmorSpec.SnippetForTag("snap.location.app1"), testutil.Contains, `peer=(label="snap.location.*"),`)
	c.Assert(apparmorSpec.SnippetForTag("snap.location.app2"), testutil.Contains, `peer=(label="snap.location.*"),`)
}

// The label uses alternation when some, but not all, apps is bound to the location plug
func (s *LocationObserveInterfaceSuite) TestConnectedSlotSnippetUsesPlugLabelSome(c *C) {
	appSet := appSetWithApps(c, "location", "app1", "app2", "app3")
	si := appSet.Info()
	plug := interfaces.NewConnectedPlug(&snap.PlugInfo{
		Snap:      si,
		Name:      "location",
		Interface: "location",
		Apps:      map[string]*snap.AppInfo{"app1": si.Apps["app1"], "app2": si.Apps["app2"]},
	}, appSet, nil, nil)

	apparmorSpec := apparmor.NewSpecification(s.slot.AppSet())
	err := apparmorSpec.AddConnectedSlot(s.iface, plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.location.app2"})
	c.Assert(apparmorSpec.SnippetForTag("snap.location.app2"), testutil.Contains, `peer=(label="snap.location{.app1,.app2}"),`)
}

// The label uses short form when exactly one app is bound to the location plug
func (s *LocationObserveInterfaceSuite) TestConnectedSlotSnippetUsesPlugLabelOne(c *C) {
	appSet := appSetWithApps(c, "location", "app")
	si := appSet.Info()
	plug := interfaces.NewConnectedPlug(&snap.PlugInfo{
		Snap:      si,
		Name:      "location",
		Interface: "location",
		Apps:      map[string]*snap.AppInfo{"app": si.Apps["app"]},
	}, appSet, nil, nil)

	apparmorSpec := apparmor.NewSpecification(s.slot.AppSet())
	err := apparmorSpec.AddConnectedSlot(s.iface, plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.location.app2"})
	c.Assert(apparmorSpec.SnippetForTag("snap.location.app2"), testutil.Contains, `peer=(label="snap.location.app"),`)
}

func (s *LocationObserveInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
