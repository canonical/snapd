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

package configfiles_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/configfiles"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
)

type specSuite struct {
	spec     *configfiles.Specification
	iface1   *ifacetest.TestInterface
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
}

var _ = Suite(&specSuite{
	iface1: &ifacetest.TestInterface{
		InterfaceName: "test",
		ConfigfilesConnectedPlugCallback: func(spec *configfiles.Specification,
			plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
			spec.AddPathContent("/etc/conf1.d/a.conf",
				&osutil.MemoryFileState{Content: []byte("aaaa"), Mode: 0655})
			return nil
		},
		ConfigfilesConnectedSlotCallback: func(spec *configfiles.Specification,
			plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
			spec.AddPathContent("/etc/conf2.d/b.conf",
				&osutil.MemoryFileState{Content: []byte("bbbb"), Mode: 0655})
			return nil
		},
		ConfigfilesPermanentPlugCallback: func(spec *configfiles.Specification,
			plug *snap.PlugInfo) error {
			spec.AddPathContent("/etc/conf3.d/c.conf",
				&osutil.MemoryFileState{Content: []byte("cccc"), Mode: 0655})
			return nil
		},
		ConfigfilesPermanentSlotCallback: func(spec *configfiles.Specification,
			slot *snap.SlotInfo) error {
			spec.AddPathContent("/etc/conf4.d/d.conf",
				&osutil.MemoryFileState{Content: []byte("dddd"), Mode: 0655})
			return nil
		},
	},
})

func (s *specSuite) SetUpTest(c *C) {
	s.spec = &configfiles.Specification{}
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

// The configfiles.Specification can be used through the interfaces.Specification interface
func (s *specSuite) TestSpecificationIface(c *C) {
	var r interfaces.Specification = s.spec
	c.Assert(r.AddConnectedPlug(s.iface1, s.plug, s.slot), IsNil)
	c.Assert(s.spec.PathContent(), DeepEquals, map[string]osutil.FileState{
		"/etc/conf1.d/a.conf": &osutil.MemoryFileState{Content: []byte("aaaa"), Mode: 0655},
	})
	c.Assert(r.AddConnectedSlot(s.iface1, s.plug, s.slot), IsNil)
	c.Assert(s.spec.PathContent(), DeepEquals, map[string]osutil.FileState{
		"/etc/conf1.d/a.conf": &osutil.MemoryFileState{Content: []byte("aaaa"), Mode: 0655},
		"/etc/conf2.d/b.conf": &osutil.MemoryFileState{Content: []byte("bbbb"), Mode: 0655},
	})
	c.Assert(r.AddPermanentPlug(s.iface1, s.plugInfo), IsNil)
	c.Assert(s.spec.PathContent(), DeepEquals, map[string]osutil.FileState{
		"/etc/conf1.d/a.conf": &osutil.MemoryFileState{Content: []byte("aaaa"), Mode: 0655},
		"/etc/conf2.d/b.conf": &osutil.MemoryFileState{Content: []byte("bbbb"), Mode: 0655},
		"/etc/conf3.d/c.conf": &osutil.MemoryFileState{Content: []byte("cccc"), Mode: 0655},
	})
	c.Assert(s.spec.Plugs(), DeepEquals, []string{"name"})
	c.Assert(r.AddPermanentSlot(s.iface1, s.slotInfo), IsNil)
	c.Assert(s.spec.PathContent(), DeepEquals, map[string]osutil.FileState{
		"/etc/conf1.d/a.conf": &osutil.MemoryFileState{Content: []byte("aaaa"), Mode: 0655},
		"/etc/conf2.d/b.conf": &osutil.MemoryFileState{Content: []byte("bbbb"), Mode: 0655},
		"/etc/conf3.d/c.conf": &osutil.MemoryFileState{Content: []byte("cccc"), Mode: 0655},
		"/etc/conf4.d/d.conf": &osutil.MemoryFileState{Content: []byte("dddd"), Mode: 0655},
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
		"internal error: configfiles plugs can be defined only by the system snap")
	c.Assert(r.AddConnectedSlot(s.iface1, s.plug, s.slot), ErrorMatches,
		"internal error: configfiles plugs can be defined only by the system snap")
	c.Assert(r.AddPermanentPlug(s.iface1, s.plugInfo), ErrorMatches,
		"internal error: configfiles plugs can be defined only by the system snap")
}

func (s *specSuite) TestAddPathContentErrors(c *C) {
	c.Check(s.spec.AddPathContent("/../conf/snap.name.conf",
		&osutil.MemoryFileState{Content: []byte("aaaa"), Mode: 0655}), ErrorMatches,
		`configfiles internal error: unclean path: "/../conf/snap.name.conf"`)
	c.Check(s.spec.AddPathContent("/etc/conf/",
		&osutil.MemoryFileState{Content: []byte("aaaa"), Mode: 0655}), ErrorMatches,
		`configfiles internal error: unclean path: "/etc/conf/"`)
	c.Check(s.spec.AddPathContent("../etc/conf/snap.name.conf",
		&osutil.MemoryFileState{Content: []byte("aaaa"), Mode: 0655}), ErrorMatches,
		`configfiles internal error: relative paths not supported: "../etc/conf/snap.name.conf"`)
	c.Check(s.spec.AddPathContent("/etc/conf/snap.name.conf",
		&osutil.MemoryFileState{Content: []byte("aaaa"), Mode: 0655}), IsNil)
	c.Check(s.spec.AddPathContent("/etc/conf/snap.name.conf",
		&osutil.MemoryFileState{Content: []byte("aaaa"), Mode: 0655}), ErrorMatches,
		`configfiles internal error: already managed path: "/etc/conf/snap.name.conf"`)
}
