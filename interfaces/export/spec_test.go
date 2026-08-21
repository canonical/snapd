// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) Canonical Ltd
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

package export_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/export"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/snap"
)

type specSuite struct {
	spec     *export.Specification
	iface1   *ifacetest.TestInterface
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot

	connectedPlugCalled bool
	connectedSlotCalled bool
	permanentPlugCalled bool
	permanentSlotCalled bool
}

var _ = Suite(&specSuite{
	iface1: &ifacetest.TestInterface{
		InterfaceName: "test",
	},
})

func (s *specSuite) SetUpTest(c *C) {
	s.spec = &export.Specification{}
	s.connectedPlugCalled = false
	s.connectedSlotCalled = false
	s.permanentPlugCalled = false
	s.permanentSlotCalled = false

	s.iface1.ExportConnectedPlugCallback = func(spec *export.Specification,
		plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
		s.connectedPlugCalled = true
		return nil
	}
	s.iface1.ExportConnectedSlotCallback = func(spec *export.Specification,
		plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
		s.connectedSlotCalled = true
		return nil
	}
	s.iface1.ExportPermanentPlugCallback = func(spec *export.Specification,
		plug *snap.PlugInfo) error {
		s.permanentPlugCalled = true
		return nil
	}
	s.iface1.ExportPermanentSlotCallback = func(spec *export.Specification,
		slot *snap.SlotInfo) error {
		s.permanentSlotCalled = true
		return nil
	}

	const plugYaml = `name: snapd
version: 1
apps:
  app:
    plugs: [name]
`
	s.plug, s.plugInfo = ifacetest.MockConnectedPlug(c, plugYaml, nil, "name")

	const slotYaml = `name: snap
version: 1
slots:
  name:
    interface: test
`
	s.slot, s.slotInfo = ifacetest.MockConnectedSlot(c, slotYaml, nil, "name")
}

// The export.Specification can be used through the interfaces.Specification interface
func (s *specSuite) TestSpecificationIface(c *C) {
	var r interfaces.Specification = s.spec

	c.Assert(r.AddConnectedPlug(s.iface1, s.plug, s.slot), IsNil)
	c.Check(s.connectedPlugCalled, Equals, true)

	c.Assert(r.AddConnectedSlot(s.iface1, s.plug, s.slot), IsNil)
	c.Check(s.connectedSlotCalled, Equals, true)

	c.Check(s.spec.Plugs(), HasLen, 0)
	c.Assert(r.AddPermanentPlug(s.iface1, s.plugInfo), IsNil)
	c.Check(s.permanentPlugCalled, Equals, true)
	c.Check(s.spec.Plugs(), DeepEquals, []string{"name"})

	c.Assert(r.AddPermanentSlot(s.iface1, s.slotInfo), IsNil)
	c.Check(s.permanentSlotCalled, Equals, true)
}

func (s *specSuite) TestPlugNotFromSystem(c *C) {
	const plugYaml = `name: notsystem
version: 1
apps:
  app:
    plugs: [name]
`
	s.plug, s.plugInfo = ifacetest.MockConnectedPlug(c, plugYaml, nil, "name")

	var r interfaces.Specification = s.spec
	c.Assert(r.AddConnectedPlug(s.iface1, s.plug, s.slot), ErrorMatches,
		"internal error: export plugs can be defined only by the system snap")
	c.Assert(r.AddConnectedSlot(s.iface1, s.plug, s.slot), ErrorMatches,
		"internal error: export plugs can be defined only by the system snap")
	c.Assert(r.AddPermanentPlug(s.iface1, s.plugInfo), ErrorMatches,
		"internal error: export plugs can be defined only by the system snap")

	// The connected-plug/slot and permanent-plug callbacks are never
	// reached, since the system-snap check happens before dispatch.
	c.Check(s.connectedPlugCalled, Equals, false)
	c.Check(s.connectedSlotCalled, Equals, false)
	c.Check(s.permanentPlugCalled, Equals, false)

	// AddPermanentSlot has no system-snap restriction (it mirrors the
	// slot-side behaviour of configfiles/symlinks).
	c.Assert(r.AddPermanentSlot(s.iface1, s.slotInfo), IsNil)
	c.Check(s.permanentSlotCalled, Equals, true)
}

func (s *specSuite) TestNoCallbacksIsNoop(c *C) {
	// A TestInterface with no callbacks set still satisfies the
	// ConnectedPlugCallback marker (the method is unconditionally defined
	// on the type), so it is tracked in Plugs(), but invoking it must be a
	// safe no-op since all the callback fields are nil.
	plain := &ifacetest.TestInterface{InterfaceName: "plain"}
	var r interfaces.Specification = s.spec
	c.Assert(r.AddConnectedPlug(plain, s.plug, s.slot), IsNil)
	c.Assert(r.AddConnectedSlot(plain, s.plug, s.slot), IsNil)
	c.Assert(r.AddPermanentPlug(plain, s.plugInfo), IsNil)
	c.Assert(r.AddPermanentSlot(plain, s.slotInfo), IsNil)
	c.Check(s.spec.Plugs(), DeepEquals, []string{"name"})
}
