// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

func (s *specSuite) TestClashingSameIface(c *C) {
	info1 := snaptest.MockInfo(c, `name: snap1
version: 0
plugs:
    plug1:
        interface: test
`, nil)

	iface := &ifacetest.TestInterface{
		InterfaceName: "test",
	}
	svc1 := &systemd.Service{ExecStart: "one"}
	svc2 := &systemd.Service{ExecStart: "two"}

	iface.SystemdPermanentPlugCallback = func(spec *systemd.Specification, plug *snap.PlugInfo) error {
		if err := spec.AddService("foo", svc1); err != nil {
			return err
		}
		return spec.AddService("foo", svc2)
	}

	spec := systemd.Specification{}
	err := spec.AddPermanentPlug(iface, info1.Plugs["plug1"])
	c.Assert(err, ErrorMatches, `internal error: interface "test" has inconsistent system needs: service for "foo" used to be defined as .*, now re-defined as .*`)
}

func (s *specSuite) TestClashingTwoIfaces(c *C) {
	info1 := snaptest.MockInfo(c, `name: snap1
version: 0
plugs:
    plug1:
        interface: test1
    plug2:
        interface: test2
`, nil)

	iface1 := &ifacetest.TestInterface{
		InterfaceName: "test1",
	}
	iface2 := &ifacetest.TestInterface{
		InterfaceName: "test2",
	}
	svc1 := &systemd.Service{ExecStart: "one"}
	svc2 := &systemd.Service{ExecStart: "two"}

	iface1.SystemdPermanentPlugCallback = func(spec *systemd.Specification, plug *snap.PlugInfo) error {
		return spec.AddService("foo", svc1)
	}

	iface2.SystemdPermanentPlugCallback = func(spec *systemd.Specification, plug *snap.PlugInfo) error {
		return spec.AddService("foo", svc2)
	}

	spec := systemd.Specification{}
	err := spec.AddPermanentPlug(iface1, info1.Plugs["plug1"])
	c.Assert(err, IsNil)
	err = spec.AddPermanentPlug(iface2, info1.Plugs["plug2"])
	c.Assert(err, ErrorMatches, `internal error: interface "test2" and "test1" have conflicting system needs: service for "foo" used to be defined as .* by "test1", now re-defined as .*`)
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

func mockConnectedPlug(c *C, yaml string, si *snap.SideInfo, plugName string) (*interfaces.ConnectedPlug, *snap.PlugInfo) {
	info := snaptest.MockInfo(c, yaml, si)

	set, err := interfaces.NewSnapAppSet(info, nil)
	c.Assert(err, IsNil)

	if plugInfo, ok := info.Plugs[plugName]; ok {
		return interfaces.NewConnectedPlug(plugInfo, set, nil, nil), plugInfo
	}
	panic(fmt.Sprintf("cannot find plug %q in snap %q", plugName, info.InstanceName()))
}

func mockConnectedSlot(c *C, yaml string, si *snap.SideInfo, slotName string) (*interfaces.ConnectedSlot, *snap.SlotInfo) {
	info := snaptest.MockInfo(c, yaml, si)

	set, err := interfaces.NewSnapAppSet(info, nil)
	c.Assert(err, IsNil)

	if slotInfo, ok := info.Slots[slotName]; ok {
		return interfaces.NewConnectedSlot(slotInfo, set, nil, nil), slotInfo
	}
	panic(fmt.Sprintf("cannot find slot %q in snap %q", slotName, info.InstanceName()))
}

func (s *specSuite) TestAddMethods(c *C) {
	const plugYaml = `name: snap1
version: 0
plugs:
 plug1:
  interface: test
`
	plug, plugInfo := mockConnectedPlug(c, plugYaml, nil, "plug1")
	const slotYaml = `name: snap2
version: 0
slots:
 slot2:
  interface: test
`
	slot, slotInfo := mockConnectedSlot(c, slotYaml, nil, "slot2")

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
