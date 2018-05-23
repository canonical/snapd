// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2018 Canonical Ltd
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

type HomeInterfaceSuite struct {
	iface    interfaces.Interface
	slot     *interfaces.ConnectedSlot
	slotInfo *snap.SlotInfo
	plug     *interfaces.ConnectedPlug
	plugInfo *snap.PlugInfo
}

var _ = Suite(&HomeInterfaceSuite{
	iface: builtin.MustInterface("home"),
})

func (s *HomeInterfaceSuite) SetUpTest(c *C) {
	const mockPlugSnapInfo = `name: other
version: 1.0
apps:
 app:
  command: foo
  plugs: [home]
`
	s.slotInfo = &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "core", Type: snap.TypeOS},
		Name:      "home",
		Interface: "home",
	}
	s.slot = interfaces.NewConnectedSlot(s.slotInfo, nil)
	plugSnap := snaptest.MockInfo(c, mockPlugSnapInfo, nil)
	s.plugInfo = plugSnap.Plugs["home"]
	s.plug = interfaces.NewConnectedPlug(s.plugInfo, nil)
}

func (s *HomeInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "home")
}

func (s *HomeInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
	slot := &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "some-snap"},
		Name:      "home",
		Interface: "home",
	}
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		"home slots are reserved for the core snap")
}

func (s *HomeInterfaceSuite) TestSanitizePlugNoAttrib(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *HomeInterfaceSuite) TestSanitizePlugWithAttrib(c *C) {
	const mockSnapYaml = `name: home-plug-snap
version: 1.0
plugs:
 home:
  read: all
`
	info := snaptest.MockInfo(c, mockSnapYaml, nil)
	plug := info.Plugs["home"]
	c.Assert(interfaces.BeforePreparePlug(s.iface, plug), IsNil)
}

func (s *HomeInterfaceSuite) TestSanitizePlugWithBadAttrib(c *C) {
	const mockSnapYaml = `name: home-plug-snap
version: 1.0
plugs:
 home:
  read: bad
`
	info := snaptest.MockInfo(c, mockSnapYaml, nil)
	plug := info.Plugs["home"]
	c.Assert(interfaces.BeforePreparePlug(s.iface, plug), ErrorMatches,
		`home plug requires "read" be 'all'`)
}

func (s *HomeInterfaceSuite) TestSanitizePlugWithEmptyAttrib(c *C) {
	const mockSnapYaml = `name: home-plug-snap
version: 1.0
plugs:
 home:
  read: ""
`
	info := snaptest.MockInfo(c, mockSnapYaml, nil)
	plug := info.Plugs["home"]
	c.Assert(interfaces.BeforePreparePlug(s.iface, plug), ErrorMatches,
		`home plug requires "read" be 'all'`)
}

func (s *HomeInterfaceSuite) TestSanitizePlugWithBadAttribOwner(c *C) {
	const mockSnapYaml = `name: home-plug-snap
version: 1.0
plugs:
 home:
  read: owner
`
	info := snaptest.MockInfo(c, mockSnapYaml, nil)
	plug := info.Plugs["home"]
	c.Assert(interfaces.BeforePreparePlug(s.iface, plug), ErrorMatches,
		`home plug requires "read" be 'all'`)
}

func (s *HomeInterfaceSuite) TestSanitizePlugWithBadAttribDict(c *C) {
	const mockSnapYaml = `name: home-plug-snap
version: 1.0
plugs:
 home:
  read:
   all: bad
   bad: all
`
	info := snaptest.MockInfo(c, mockSnapYaml, nil)
	plug := info.Plugs["home"]
	c.Assert(interfaces.BeforePreparePlug(s.iface, plug), ErrorMatches,
		`home plug requires "read" be 'all'`)
}

func (s *HomeInterfaceSuite) TestConnectedPlugAppArmorWithoutAttrib(c *C) {
	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app"})
	c.Check(apparmorSpec.SnippetForTag("snap.other.app"), testutil.Contains, `owner @{HOME}/ r,`)
	c.Check(apparmorSpec.SnippetForTag("snap.other.app"), Not(testutil.Contains), `# Allow non-owner read`)
}

func (s *HomeInterfaceSuite) TestConnectedPlugAppArmorWithAttribAll(c *C) {
	const mockSnapYaml = `name: home-plug-snap
version: 1.0
plugs:
 home:
  read: all
apps:
 app2:
  command: foo
`
	info := snaptest.MockInfo(c, mockSnapYaml, nil)
	plug := interfaces.NewConnectedPlug(info.Plugs["home"], nil)

	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.home-plug-snap.app2"})
	c.Check(apparmorSpec.SnippetForTag("snap.home-plug-snap.app2"), testutil.Contains, `owner @{HOME}/ r,`)
	c.Check(apparmorSpec.SnippetForTag("snap.home-plug-snap.app2"), testutil.Contains, `# Allow non-owner read`)
}

func (s *HomeInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
