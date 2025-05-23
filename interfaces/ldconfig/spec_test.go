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

package ldconfig_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/interfaces/ldconfig"
	"github.com/snapcore/snapd/snap"
)

type specSuite struct {
	spec     *ldconfig.Specification
	iface1   *ifacetest.TestInterface
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
}

var _ = Suite(&specSuite{
	iface1: &ifacetest.TestInterface{
		InterfaceName: "test",
		LdconfigConnectedPlugCallback: func(spec *ldconfig.Specification,
			plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
			spec.AddLibDirs([]string{"/dir1/lib1"})
			return nil
		},
		LdconfigConnectedSlotCallback: func(spec *ldconfig.Specification,
			plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
			spec.AddLibDirs([]string{"/dir1/lib2"})
			return nil
		},
		LdconfigPermanentPlugCallback: func(spec *ldconfig.Specification,
			plug *snap.PlugInfo) error {
			spec.AddLibDirs([]string{"/dir2/lib3"})
			return nil
		},
		LdconfigPermanentSlotCallback: func(spec *ldconfig.Specification,
			slot *snap.SlotInfo) error {
			spec.AddLibDirs([]string{"/dir2/lib4"})
			return nil
		},
	},
})

func (s *specSuite) SetUpTest(c *C) {
	s.spec = &ldconfig.Specification{}
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

// The ldconfig.Specification can be used through the interfaces.Specification interface
func (s *specSuite) TestSpecificationIface(c *C) {
	var r interfaces.Specification = s.spec
	c.Assert(r.AddConnectedPlug(s.iface1, s.plug, s.slot), IsNil)
	c.Assert(s.spec.LibDirs(), DeepEquals, map[ldconfig.SnapSlot][]string{
		{"snap", "name"}: {"/dir1/lib1"},
	})
	c.Assert(r.AddConnectedSlot(s.iface1, s.plug, s.slot), IsNil)
	c.Assert(s.spec.LibDirs(), DeepEquals, map[ldconfig.SnapSlot][]string{
		{"snap", "name"}: {"/dir1/lib2"},
	})
	c.Assert(r.AddPermanentPlug(s.iface1, s.plugInfo), IsNil)
	c.Assert(s.spec.LibDirs(), DeepEquals, map[ldconfig.SnapSlot][]string{
		{"snap", "name"}: {"/dir2/lib3"},
	})
	c.Assert(s.spec.Plugs(), DeepEquals, []string{"name"})
	c.Assert(r.AddPermanentSlot(s.iface1, s.slotInfo), IsNil)
	c.Assert(s.spec.LibDirs(), DeepEquals, map[ldconfig.SnapSlot][]string{
		{"snap", "name"}: {"/dir2/lib4"},
	})
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
		"internal error: ldconfig plugs can be defined only by the system snap")
	c.Assert(r.AddConnectedSlot(s.iface1, s.plug, s.slot), ErrorMatches,
		"internal error: ldconfig plugs can be defined only by the system snap")
	c.Assert(r.AddPermanentPlug(s.iface1, s.plugInfo), ErrorMatches,
		"internal error: ldconfig plugs can be defined only by the system snap")
}
