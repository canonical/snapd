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

package systemd_test

import (
	"fmt"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/interfaces/systemd"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
)

type specSuite struct{}

var _ = Suite(&specSuite{})

func (s *specSuite) TestAddService(c *C) {
	spec := systemd.Specification{}
	c.Assert(spec.Services(), IsNil)
	svc1 := &systemd.Service{ExecStart: "one"}
	err := spec.AddService("svc1", svc1)
	c.Assert(err, IsNil)
	svc2 := &systemd.Service{ExecStart: "two"}
	err = spec.AddService("svc2", svc2)
	c.Assert(err, IsNil)
	c.Assert(spec.Services(), DeepEquals, map[string]*systemd.Service{
		"svc1": svc1,
		"svc2": svc2,
	})
}

func (s *specSuite) TestClashing(c *C) {
	svc1 := &systemd.Service{ExecStart: "one"}
	svc2 := &systemd.Service{ExecStart: "two"}
	spec := systemd.Specification{}
	err := spec.AddService("foo", svc1)
	c.Assert(err, IsNil)
	err = spec.AddService("foo", svc2)
	c.Assert(err, ErrorMatches, `internal error: interface has conflicting system needs: service for "foo" used to be defined as .*, now re-defined as .*`)
}

func (s *specSuite) TestDifferentObjectsNotClashing(c *C) {
	svc1 := &systemd.Service{ExecStart: "one and the same"}
	svc2 := &systemd.Service{ExecStart: "one and the same"}
	spec := systemd.Specification{}
	err := spec.AddService("foo", svc1)
	c.Assert(err, IsNil)
	err = spec.AddService("foo", svc2)
	c.Assert(err, IsNil)
}

func (s *specSuite) TestAddMethods(c *C) {
	info1 := snaptest.MockInfo(c, `name: snap1
version: 0
plugs:
    plug1:
        interface: test
`, nil)
	info2 := snaptest.MockInfo(c, `name: snap2
version: 0
slots:
    slot2:
        interface: test
`, nil)
	plugInfo := info1.Plugs["plug1"]
	plug := interfaces.NewConnectedPlug(plugInfo, nil, nil)
	slotInfo := info2.Slots["slot2"]
	slot := interfaces.NewConnectedSlot(slotInfo, nil, nil)

	spec := systemd.Specification{}

	iface := &ifacetest.TestInterface{
		InterfaceName: "test",
		SystemdConnectedPlugCallback: func(spec *systemd.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
			spec.AddService(fmt.Sprintf("%s-%s", plug.Name(), slot.Name()), &systemd.Service{ExecStart: "connected-plug"})
			return nil
		},
		SystemdConnectedSlotCallback: func(spec *systemd.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
			spec.AddService(fmt.Sprintf("%s-%s", slot.Name(), plug.Name()), &systemd.Service{ExecStart: "connected-slot"})
			return nil
		},
		SystemdPermanentPlugCallback: func(spec *systemd.Specification, plug *snap.PlugInfo) error {
			spec.AddService(plug.Name, &systemd.Service{ExecStart: "permanent-plug"})
			return nil
		},
		SystemdPermanentSlotCallback: func(spec *systemd.Specification, slot *snap.SlotInfo) error {
			spec.AddService(slot.Name, &systemd.Service{ExecStart: "permanent-slot"})
			return nil
		},
	}

	err := spec.AddPermanentSlot(iface, slotInfo)
	c.Assert(err, IsNil)

	err = spec.AddPermanentPlug(iface, plugInfo)
	c.Assert(err, IsNil)

	err = spec.AddConnectedSlot(iface, plug, slot)
	c.Assert(err, IsNil)

	err = spec.AddConnectedPlug(iface, plug, slot)
	c.Assert(err, IsNil)

	c.Check(spec.Services(), DeepEquals, map[string]*systemd.Service{
		"plug1-slot2": {ExecStart: "connected-plug"},
		"slot2-plug1": {ExecStart: "connected-slot"},
		"plug1":       {ExecStart: "permanent-plug"},
		"slot2":       {ExecStart: "permanent-slot"},
	})
}
