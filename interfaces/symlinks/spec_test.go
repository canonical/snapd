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

package symlinks_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/interfaces/symlinks"
	"github.com/snapcore/snapd/snap"
)

type specSuite struct {
	spec     *symlinks.Specification
	iface1   *ifacetest.TestInterface
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
}

var _ = Suite(&specSuite{
	iface1: &ifacetest.TestInterface{
		InterfaceName: "test",
		SymlinksConnectedPlugCallback: func(spec *symlinks.Specification,
			plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
			return spec.AddSymlink("/snap/mysnap/1/lib/bar.so", "/usr/lib/foo/bar.so")
		},
		SymlinksConnectedSlotCallback: func(spec *symlinks.Specification,
			plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
			return spec.AddSymlink("/snap/mysnap/1/lib2/bar.so", "/usr/lib/foo/bar2.so")
		},
		SymlinksPermanentPlugCallback: func(spec *symlinks.Specification,
			plug *snap.PlugInfo) error {
			return spec.AddSymlink("/snap/mysnap/1/lib3/bar.so", "/usr/lib/foo2/bar.so")
		},
		SymlinksPermanentSlotCallback: func(spec *symlinks.Specification,
			slot *snap.SlotInfo) error {
			return spec.AddSymlink("/snap/mysnap/1/lib4/bar.so", "/usr/lib/foo2/bar2.so")
		},
	},
})

func (s *specSuite) SetUpTest(c *C) {
	s.spec = &symlinks.Specification{}
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

// The symlinks.Specification can be used through the interfaces.Specification interface
func (s *specSuite) TestSpecificationIface(c *C) {
	var r interfaces.Specification = s.spec
	c.Assert(r.AddConnectedPlug(s.iface1, s.plug, s.slot), IsNil)
	c.Assert(s.spec.Symlinks(), DeepEquals, map[string]symlinks.SymlinkToTarget{
		"/usr/lib/foo": {"bar.so": "/snap/mysnap/1/lib/bar.so"},
	})
	c.Assert(r.AddConnectedSlot(s.iface1, s.plug, s.slot), IsNil)
	c.Assert(s.spec.Symlinks(), DeepEquals, map[string]symlinks.SymlinkToTarget{
		"/usr/lib/foo": {
			"bar.so":  "/snap/mysnap/1/lib/bar.so",
			"bar2.so": "/snap/mysnap/1/lib2/bar.so"},
	})
	c.Assert(r.AddPermanentPlug(s.iface1, s.plugInfo), IsNil)
	c.Assert(s.spec.Symlinks(), DeepEquals, map[string]symlinks.SymlinkToTarget{
		"/usr/lib/foo": {
			"bar.so":  "/snap/mysnap/1/lib/bar.so",
			"bar2.so": "/snap/mysnap/1/lib2/bar.so"},
		"/usr/lib/foo2": {
			"bar.so": "/snap/mysnap/1/lib3/bar.so"},
	})
	c.Assert(s.spec.Plugs(), DeepEquals, []string{"name"})
	c.Assert(r.AddPermanentSlot(s.iface1, s.slotInfo), IsNil)
	c.Assert(s.spec.Symlinks(), DeepEquals, map[string]symlinks.SymlinkToTarget{
		"/usr/lib/foo": {
			"bar.so":  "/snap/mysnap/1/lib/bar.so",
			"bar2.so": "/snap/mysnap/1/lib2/bar.so"},
		"/usr/lib/foo2": {
			"bar.so":  "/snap/mysnap/1/lib3/bar.so",
			"bar2.so": "/snap/mysnap/1/lib4/bar.so"},
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
		"internal error: symlinks plugs can be defined only by the system snap")
	c.Assert(r.AddConnectedSlot(s.iface1, s.plug, s.slot), ErrorMatches,
		"internal error: symlinks plugs can be defined only by the system snap")
	c.Assert(r.AddPermanentPlug(s.iface1, s.plugInfo), ErrorMatches,
		"internal error: symlinks plugs can be defined only by the system snap")
}

func (s *specSuite) TestAddSymlinkErrors(c *C) {
	c.Check(s.spec.AddSymlink("/usr/lib/foo4/bar.so", "../../lib4/bar.so"), ErrorMatches,
		`symlinks internal error: relative paths not supported: "../../lib4/bar.so"`)
	c.Check(s.spec.AddSymlink("../../foo4/bar.so", "/snap/mysnap/lib4/bar.so"), ErrorMatches,
		`symlinks internal error: relative paths not supported: "../../foo4/bar.so"`)
	c.Check(s.spec.AddSymlink("/usr/lib/foo4/bar.so", "/snap/./mysnap/1/lib3/bar.so"), ErrorMatches,
		`symlinks internal error: unclean path: "/snap/./mysnap/1/lib3/bar.so"`)
	c.Check(s.spec.AddSymlink("//usr/lib/foo4/bar.so", "/snap/mysnap/1/lib3/bar.so"), ErrorMatches,
		`symlinks internal error: unclean path: "//usr/lib/foo4/bar.so"`)

	c.Assert(s.spec.AddConnectedPlug(s.iface1, s.plug, s.slot), IsNil)
	c.Assert(s.spec.Symlinks(), DeepEquals, map[string]symlinks.SymlinkToTarget{
		"/usr/lib/foo": {"bar.so": "/snap/mysnap/1/lib/bar.so"},
	})
	c.Assert(s.spec.AddConnectedPlug(s.iface1, s.plug, s.slot), ErrorMatches,
		`symlinks internal error: already managed symlink: "/usr/lib/foo/bar.so"`)
	c.Assert(s.spec.Symlinks(), DeepEquals, map[string]symlinks.SymlinkToTarget{
		"/usr/lib/foo": {"bar.so": "/snap/mysnap/1/lib/bar.so"},
	})
}
