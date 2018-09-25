// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

type dotfilesInterfaceSuite struct {
	iface    interfaces.Interface
	slot     *interfaces.ConnectedSlot
	slotInfo *snap.SlotInfo
	plug     *interfaces.ConnectedPlug
	plugInfo *snap.PlugInfo
}

var _ = Suite(&dotfilesInterfaceSuite{
	iface: builtin.MustInterface("dotfiles"),
})

func (s *dotfilesInterfaceSuite) SetUpTest(c *C) {
	const mockPlugSnapInfo = `name: other
version: 1.0
plugs:
 dotfiles:
  dirs: [.dir1/]
  files: [.file1]
apps:
 app:
  command: foo
  plugs: [dotfiles]
`
	s.slotInfo = &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "core", Type: snap.TypeOS},
		Name:      "dotfiles",
		Interface: "dotfiles",
	}
	s.slot = interfaces.NewConnectedSlot(s.slotInfo, nil, nil)
	plugSnap := snaptest.MockInfo(c, mockPlugSnapInfo, nil)
	s.plugInfo = plugSnap.Plugs["dotfiles"]
	s.plug = interfaces.NewConnectedPlug(s.plugInfo, nil, nil)
}

func (s *dotfilesInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "dotfiles")
}

func (s *dotfilesInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
	slot := &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "some-snap"},
		Name:      "dotfiles",
		Interface: "dotfiles",
	}
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		"dotfiles slots are reserved for the core snap")
}

func (s *dotfilesInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *dotfilesInterfaceSuite) TestSanitizePlugHappy(c *C) {
	const mockSnapYaml = `name: dotfiles-plug-snap
version: 1.0
plugs:
 dotfiles:
  files: [".file1"]
  dirs: [".dir1/"]
`
	info := snaptest.MockInfo(c, mockSnapYaml, nil)
	plug := info.Plugs["dotfiles"]
	c.Assert(interfaces.BeforePreparePlug(s.iface, plug), IsNil)
}

func (s *dotfilesInterfaceSuite) TestSanitizePlugWithEmptyFilesAttrib(c *C) {
	const mockSnapYaml = `name: dotfiles-plug-snap
version: 1.0
plugs:
 dotfiles:
  files: ""
`
	info := snaptest.MockInfo(c, mockSnapYaml, nil)
	plug := info.Plugs["dotfiles"]
	c.Assert(interfaces.BeforePreparePlug(s.iface, plug), ErrorMatches,
		`cannot add dotfiles plug: "files" must be a list of strings`)
}

func (s *dotfilesInterfaceSuite) TestSanitizePlugWithWrongFileAttrType(c *C) {
	const mockSnapYaml = `name: dotfiles-plug-snap
version: 1.0
plugs:
 dotfiles:
  files: [ 121 ]
`
	info := snaptest.MockInfo(c, mockSnapYaml, nil)
	plug := info.Plugs["dotfiles"]
	c.Assert(interfaces.BeforePreparePlug(s.iface, plug), ErrorMatches,
		`cannot add dotfiles plug: "files" must be a list of strings`)
}

func (s *dotfilesInterfaceSuite) TestSanitizePlugWithUncleanPath(c *C) {
	const mockSnapYaml = `name: dotfiles-plug-snap
version: 1.0
plugs:
 dotfiles:
  files: [ "./foo/./bar" ]
`
	info := snaptest.MockInfo(c, mockSnapYaml, nil)
	plug := info.Plugs["dotfiles"]
	c.Assert(interfaces.BeforePreparePlug(s.iface, plug), ErrorMatches,
		`cannot add dotfiles plug: "./foo/./bar" must be clean`)
}

func (s *dotfilesInterfaceSuite) TestSanitizePlugDots(c *C) {
	const mockSnapYaml = `name: dotfiles-plug-snap
version: 1.0
plugs:
 dotfiles:
  files: [ "../foo" ]
`
	info := snaptest.MockInfo(c, mockSnapYaml, nil)
	plug := info.Plugs["dotfiles"]
	c.Assert(interfaces.BeforePreparePlug(s.iface, plug), ErrorMatches,
		`cannot add dotfiles plug: "../foo" contains invalid ".."`)
}

func (s *dotfilesInterfaceSuite) TestSanitizePlugFilesWithTrailingSlash(c *C) {
	const mockSnapYaml = `name: dotfiles-plug-snap
version: 1.0
plugs:
 dotfiles:
  files: [ "foo/" ]
`
	info := snaptest.MockInfo(c, mockSnapYaml, nil)
	plug := info.Plugs["dotfiles"]
	c.Assert(interfaces.BeforePreparePlug(s.iface, plug), ErrorMatches,
		`cannot add dotfiles plug: "foo/" must be clean`)
}

func (s *dotfilesInterfaceSuite) TestSanitizePlugAARE(c *C) {
	const mockSnapYaml = `name: dotfiles-plug-snap
version: 1.0
plugs:
 dotfiles:
  files: [ "foo[" ]
`
	info := snaptest.MockInfo(c, mockSnapYaml, nil)
	plug := info.Plugs["dotfiles"]
	c.Assert(interfaces.BeforePreparePlug(s.iface, plug), ErrorMatches,
		`cannot add dotfiles plug: "foo\[" contains a reserved apparmor char from .*`)
}

func (s *dotfilesInterfaceSuite) TestSanitizePlugWithEmptyDirsAttrib(c *C) {
	const mockSnapYaml = `name: dotfiles-plug-snap
version: 1.0
plugs:
 dotfiles:
  dirs: ""
`
	info := snaptest.MockInfo(c, mockSnapYaml, nil)
	plug := info.Plugs["dotfiles"]
	c.Assert(interfaces.BeforePreparePlug(s.iface, plug), ErrorMatches,
		`cannot add dotfiles plug: "dirs" must be a list of strings`)
}

func (s *dotfilesInterfaceSuite) TestSanitizePlugWithBadAttrib(c *C) {
	const mockSnapYaml = `name: dotfiles-plug-snap
version: 1.0
plugs:
 dotfiles:
  foo: bar
`
	info := snaptest.MockInfo(c, mockSnapYaml, nil)
	plug := info.Plugs["dotfiles"]
	c.Assert(interfaces.BeforePreparePlug(s.iface, plug), ErrorMatches,
		`cannot add dotfiles plug without valid "files" or "dirs" attribute`)
}

func (s *dotfilesInterfaceSuite) TestSanitizePlugFilesWithTilde(c *C) {
	const mockSnapYaml = `name: dotfiles-plug-snap
version: 1.0
plugs:
 dotfiles:
  files: [ "~/foo" ]
`
	info := snaptest.MockInfo(c, mockSnapYaml, nil)
	plug := info.Plugs["dotfiles"]
	c.Assert(interfaces.BeforePreparePlug(s.iface, plug), ErrorMatches,
		`cannot add dotfiles plug: "~/foo" contains invalid "~"`)
}

func (s *dotfilesInterfaceSuite) TestConnectedPlugAppArmor(c *C) {
	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app"})
	c.Check(apparmorSpec.SnippetForTag("snap.other.app"), testutil.Contains, `owner @${HOME}/.file1 rwklix,`)
	c.Check(apparmorSpec.SnippetForTag("snap.other.app"), testutil.Contains, `owner @${HOME}/.dir1/** rwklix,`)

}

func (s *dotfilesInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
