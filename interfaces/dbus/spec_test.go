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

package dbus_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/dbus"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/snap"
)

type specSuite struct {
	iface    *ifacetest.TestInterface
	spec     *dbus.Specification
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
}

var _ = Suite(&specSuite{
	iface: &ifacetest.TestInterface{
		InterfaceName: "test",
		DBusConnectedPlugCallback: func(spec *dbus.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
			spec.AddSnippet("connected-plug")
			return nil
		},
		DBusConnectedSlotCallback: func(spec *dbus.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
			spec.AddSnippet("connected-slot")
			return nil
		},
		DBusPermanentPlugCallback: func(spec *dbus.Specification, plug *snap.PlugInfo) error {
			spec.AddSnippet("permanent-plug")
			return nil
		},
		DBusPermanentSlotCallback: func(spec *dbus.Specification, slot *snap.SlotInfo) error {
			spec.AddSnippet("permanent-slot")
			return nil
		},
	},
	plugInfo: &snap.PlugInfo{
		Snap:      &snap.Info{SuggestedName: "snap1"},
		Name:      "name",
		Interface: "test",
		Apps: map[string]*snap.AppInfo{
			"app1": {
				Snap: &snap.Info{
					SuggestedName: "snap1",
				},
				Name: "app1"}},
	},
	slotInfo: &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "snap2"},
		Name:      "name",
		Interface: "test",
		Apps: map[string]*snap.AppInfo{
			"app2": {
				Snap: &snap.Info{
					SuggestedName: "snap2",
				},
				Name: "app2"}},
	},
})

func (s *specSuite) SetUpTest(c *C) {
	s.spec = &dbus.Specification{}
	s.plug = interfaces.NewConnectedPlug(s.plugInfo, nil, nil)
	s.slot = interfaces.NewConnectedSlot(s.slotInfo, nil, nil)
}

// The spec.Specification can be used through the interfaces.Specification interface
func (s *specSuite) TestSpecificationIface(c *C) {
	var r interfaces.Specification = s.spec
	c.Assert(r.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(r.AddConnectedSlot(s.iface, s.plug, s.slot), IsNil)
	c.Assert(r.AddPermanentPlug(s.iface, s.plugInfo), IsNil)
	c.Assert(r.AddPermanentSlot(s.iface, s.slotInfo), IsNil)
	c.Assert(s.spec.Snippets(), DeepEquals, map[string][]string{
		"snap.snap1.app1": {"connected-plug", "permanent-plug"},
		"snap.snap2.app2": {"connected-slot", "permanent-slot"},
	})
	c.Assert(s.spec.SecurityTags(), DeepEquals, []string{"snap.snap1.app1", "snap.snap2.app2"})
	c.Assert(s.spec.SnippetForTag("snap.snap1.app1"), Equals, "connected-plug\npermanent-plug\n")

	c.Assert(s.spec.SnippetForTag("non-existing"), Equals, "")
}

func (s *specSuite) TestAddService(c *C) {
	snapInfo := &snap.Info{
		SuggestedName: "snap1",
	}
	app := &snap.AppInfo{
		Name: "app",
		Snap: snapInfo,
	}
	svc := &snap.AppInfo{
		Name:   "svc",
		Daemon: "simple",
		Snap:   snapInfo,
	}
	err := s.spec.AddService("system", "org.foo", svc)
	c.Assert(err, IsNil)
	err = s.spec.AddService("system", "org.bar", svc)
	c.Assert(err, IsNil)
	c.Check(s.spec.SystemServices(), DeepEquals, map[string]*dbus.Service{
		"org.foo": {
			BusName:     "org.foo",
			SecurityTag: "snap.snap1.svc",
			Content: []byte(`[D-BUS Service]
Name=org.foo
Comment=Bus name for snap application snap1.svc
Exec=/usr/bin/snap run snap1.svc
AssumedAppArmorLabel=snap.snap1.svc
User=root
SystemdService=snap.snap1.svc.service
X-Snap=snap1
`),
		},
		"org.bar": {
			BusName:     "org.bar",
			SecurityTag: "snap.snap1.svc",
			Content: []byte(`[D-BUS Service]
Name=org.bar
Comment=Bus name for snap application snap1.svc
Exec=/usr/bin/snap run snap1.svc
AssumedAppArmorLabel=snap.snap1.svc
User=root
SystemdService=snap.snap1.svc.service
X-Snap=snap1
`),
		},
	})

	err = s.spec.AddService("session", "org.baz", app)
	c.Check(err, IsNil)
	c.Check(s.spec.SessionServices(), DeepEquals, map[string]*dbus.Service{
		"org.baz": {
			BusName:     "org.baz",
			SecurityTag: "snap.snap1.app",
			Content: []byte(`[D-BUS Service]
Name=org.baz
Comment=Bus name for snap application snap1.app
Exec=/usr/bin/snap run snap1.app
AssumedAppArmorLabel=snap.snap1.app
X-Snap=snap1
`),
		},
	})
}
