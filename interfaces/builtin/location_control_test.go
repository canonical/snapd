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
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type LocationControlInterfaceSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

var _ = Suite(&LocationControlInterfaceSuite{
	iface: builtin.MustInterface("location-control"),
})

func (s *LocationControlInterfaceSuite) SetUpTest(c *C) {
	var plugSnapInfoYaml = `name: location-consumer
version: 1.0
plugs:
 location-client:
  interface: location-control
apps:
 app:
  command: foo
  plugs: [location-client]
`
	var slotSnapInfoYaml = `name: location
version: 1.0
slots:
 location:
  interface: location-control
apps:
 app2:
  command: foo
  slots: [location]
`
	snapInfo := snaptest.MockInfo(c, plugSnapInfoYaml, nil)
	s.plugInfo = snapInfo.Plugs["location-client"]
	s.plug = interfaces.NewConnectedPlug(s.plugInfo, nil)
	snapInfo = snaptest.MockInfo(c, slotSnapInfoYaml, nil)
	s.slotInfo = snapInfo.Slots["location"]
	s.slot = interfaces.NewConnectedSlot(s.slotInfo, nil)
}

func (s *LocationControlInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "location-control")
}

// The label glob when all apps are bound to the location slot
func (s *LocationControlInterfaceSuite) TestConnectedPlugSnippetUsesSlotLabelAll(c *C) {
	app1 := &snap.AppInfo{Name: "app1"}
	app2 := &snap.AppInfo{Name: "app2"}
	slot := interfaces.NewConnectedSlot(&snap.SlotInfo{
		Snap: &snap.Info{
			SuggestedName: "location",
			Apps:          map[string]*snap.AppInfo{"app1": app1, "app2": app2},
		},
		Name:      "location",
		Interface: "location",
		Apps:      map[string]*snap.AppInfo{"app1": app1, "app2": app2},
	}, nil)

	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.location-consumer.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.location-consumer.app"), testutil.Contains, `peer=(label="snap.location.*"),`)
}

// The label uses alternation when some, but not all, apps is bound to the location slot
func (s *LocationControlInterfaceSuite) TestConnectedPlugSnippetUsesSlotLabelSome(c *C) {
	app1 := &snap.AppInfo{Name: "app1"}
	app2 := &snap.AppInfo{Name: "app2"}
	app3 := &snap.AppInfo{Name: "app3"}
	slot := interfaces.NewConnectedSlot(&snap.SlotInfo{
		Snap: &snap.Info{
			SuggestedName: "location",
			Apps:          map[string]*snap.AppInfo{"app1": app1, "app2": app2, "app3": app3},
		},
		Name:      "location",
		Interface: "location",
		Apps:      map[string]*snap.AppInfo{"app1": app1, "app2": app2},
	}, nil)

	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.location-consumer.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.location-consumer.app"), testutil.Contains, `peer=(label="snap.location.{app1,app2}"),`)
}

// The label uses short form when exactly one app is bound to the location slot
func (s *LocationControlInterfaceSuite) TestConnectedPlugSnippetUsesSlotLabelOne(c *C) {
	app := &snap.AppInfo{Name: "app"}
	slot := interfaces.NewConnectedSlot(&snap.SlotInfo{
		Snap: &snap.Info{
			SuggestedName: "location",
			Apps:          map[string]*snap.AppInfo{"app": app},
		},
		Name:      "location",
		Interface: "location",
		Apps:      map[string]*snap.AppInfo{"app": app},
	}, nil)

	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.location-consumer.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.location-consumer.app"), testutil.Contains, `peer=(label="snap.location.app"),`)
}

// The label glob when all apps are bound to the location plug
func (s *LocationControlInterfaceSuite) TestConnectedSlotSnippetUsesPlugLabelAll(c *C) {
	app1 := &snap.AppInfo{Name: "app1"}
	app2 := &snap.AppInfo{Name: "app2"}
	plug := interfaces.NewConnectedPlug(&snap.PlugInfo{
		Snap: &snap.Info{
			SuggestedName: "location",
			Apps:          map[string]*snap.AppInfo{"app1": app1, "app2": app2},
		},
		Name:      "location",
		Interface: "location",
		Apps:      map[string]*snap.AppInfo{"app1": app1, "app2": app2},
	}, nil)

	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedSlot(s.iface, plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.location.app2"})
	c.Assert(apparmorSpec.SnippetForTag("snap.location.app2"), testutil.Contains, `peer=(label="snap.location.*"),`)
}

// The label uses alternation when some, but not all, apps is bound to the location plug
func (s *LocationControlInterfaceSuite) TestConnectedSlotSnippetUsesPlugLabelSome(c *C) {
	app1 := &snap.AppInfo{Name: "app1"}
	app2 := &snap.AppInfo{Name: "app2"}
	app3 := &snap.AppInfo{Name: "app3"}
	plug := interfaces.NewConnectedPlug(&snap.PlugInfo{
		Snap: &snap.Info{
			SuggestedName: "location",
			Apps:          map[string]*snap.AppInfo{"app1": app1, "app2": app2, "app3": app3},
		},
		Name:      "location",
		Interface: "location",
		Apps:      map[string]*snap.AppInfo{"app1": app1, "app2": app2},
	}, nil)

	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedSlot(s.iface, plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.location.app2"})
	c.Assert(apparmorSpec.SnippetForTag("snap.location.app2"), testutil.Contains, `peer=(label="snap.location.{app1,app2}"),`)
}

// The label uses short form when exactly one app is bound to the location plug
func (s *LocationControlInterfaceSuite) TestConnectedSlotSnippetUsesPlugLabelOne(c *C) {
	app := &snap.AppInfo{Name: "app"}
	plug := interfaces.NewConnectedPlug(&snap.PlugInfo{
		Snap: &snap.Info{
			SuggestedName: "location",
			Apps:          map[string]*snap.AppInfo{"app": app},
		},
		Name:      "location",
		Interface: "location",
		Apps:      map[string]*snap.AppInfo{"app": app},
	}, nil)

	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedSlot(s.iface, plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.location.app2"})
	c.Assert(apparmorSpec.SnippetForTag("snap.location.app2"), testutil.Contains, `peer=(label="snap.location.app"),`)
}

func (s *LocationControlInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
