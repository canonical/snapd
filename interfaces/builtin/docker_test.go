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
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type DockerInterfaceSuite struct {
	iface interfaces.Interface
	slot  *interfaces.Slot
	plug  *interfaces.Plug
}

var _ = Suite(&DockerInterfaceSuite{
	iface: &builtin.DockerInterface{},
	slot: &interfaces.Slot{
		SlotInfo: &snap.SlotInfo{
			Snap:      &snap.Info{SuggestedName: "docker"},
			Name:      "docker-daemon",
			Interface: "docker",
		},
	},
	plug: &interfaces.Plug{
		PlugInfo: &snap.PlugInfo{
			Snap:      &snap.Info{SuggestedName: "docker"},
			Name:      "docker-client",
			Interface: "docker",
		},
	},
})

func (s *DockerInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "docker")
}

func (s *DockerInterfaceSuite) TestUnusedSecuritySystems(c *C) {
	for _, system := range [...]interfaces.SecuritySystem{
		interfaces.SecurityAppArmor,
		interfaces.SecurityDBus,
		interfaces.SecurityMount,
		interfaces.SecuritySecComp,
		interfaces.SecurityUDev,
	} {
		snippet, err := s.iface.PermanentPlugSnippet(s.plug, system)
		c.Assert(err, IsNil)
		c.Assert(snippet, IsNil)
		snippet, err = s.iface.ConnectedSlotSnippet(s.plug, s.slot, system)
		c.Assert(err, IsNil)
		c.Assert(snippet, IsNil)
	}
	for _, system := range [...]interfaces.SecuritySystem{
		interfaces.SecurityDBus,
		interfaces.SecurityMount,
		interfaces.SecurityUDev,
	} {
		snippet, err := s.iface.ConnectedPlugSnippet(s.plug, s.slot, system)
		c.Assert(err, IsNil)
		c.Assert(snippet, IsNil)
		snippet, err = s.iface.PermanentSlotSnippet(s.slot, system)
		c.Assert(err, IsNil)
		c.Assert(snippet, IsNil)
	}
}

func (s *DockerInterfaceSuite) TestUsedSecuritySystems(c *C) {
	for _, system := range [...]interfaces.SecuritySystem{
		interfaces.SecurityAppArmor,
		interfaces.SecuritySecComp,
	} {
		snippet, err := s.iface.ConnectedPlugSnippet(s.plug, s.slot, system)
		c.Assert(err, IsNil)
		c.Assert(snippet, Not(IsNil))
		snippet, err = s.iface.PermanentSlotSnippet(s.slot, system)
		c.Assert(err, IsNil)
		c.Assert(snippet, Not(IsNil))
	}
}

func (s *DockerInterfaceSuite) TestUnexpectedSecuritySystems(c *C) {
	snippet, err := s.iface.PermanentPlugSnippet(s.plug, "foo")
	c.Assert(err, Equals, interfaces.ErrUnknownSecurity)
	c.Assert(snippet, IsNil)
	snippet, err = s.iface.ConnectedPlugSnippet(s.plug, s.slot, "foo")
	c.Assert(err, Equals, interfaces.ErrUnknownSecurity)
	c.Assert(snippet, IsNil)
	snippet, err = s.iface.PermanentSlotSnippet(s.slot, "foo")
	c.Assert(err, Equals, interfaces.ErrUnknownSecurity)
	c.Assert(snippet, IsNil)
	snippet, err = s.iface.ConnectedSlotSnippet(s.plug, s.slot, "foo")
	c.Assert(err, Equals, interfaces.ErrUnknownSecurity)
	c.Assert(snippet, IsNil)
}

func (s *DockerInterfaceSuite) TestAutoConnect(c *C) {
	c.Check(s.iface.AutoConnect(), Equals, false)
}

func (s *DockerInterfaceSuite) TestConnectedPlugSnippet(c *C) {
	snippet, err := s.iface.ConnectedPlugSnippet(s.plug, s.slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(string(snippet), testutil.Contains, `run/docker.sock`)

	snippet, err = s.iface.ConnectedPlugSnippet(s.plug, s.slot, interfaces.SecuritySecComp)
	c.Assert(err, IsNil)
	c.Assert(string(snippet), testutil.Contains, `bind`)
}

func (s *DockerInterfaceSuite) TestPermanentSlotSnippet(c *C) {
	snippet, err := s.iface.PermanentSlotSnippet(s.slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(string(snippet), testutil.Contains, `run/docker.sock`)
	c.Assert(string(snippet), testutil.Contains, `pivot_root`)

	snippet, err = s.iface.PermanentSlotSnippet(s.slot, interfaces.SecuritySecComp)
	c.Assert(err, IsNil)
	c.Assert(string(snippet), testutil.Contains, `pivot_root`)
}


func (s *DockerInterfaceSuite) TestSanitizePlugNoAttrib(c *C) {
	err := s.iface.SanitizePlug(s.plug)
	c.Assert(err, IsNil)
}

func (s *DockerInterfaceSuite) TestSanitizePlugWithAttrib(c *C) {
	var mockSnapYaml = []byte(`name: docker-plug-snap
version: 1.0
plugs:
 docker-plug:
  interface: docker
  daemon-privileged: true
`)

	info, err := snap.InfoFromSnapYaml(mockSnapYaml)
	c.Assert(err, IsNil)

	plug := &interfaces.Plug{PlugInfo: info.Plugs["docker-plug"]}
	err = s.iface.SanitizePlug(plug)
	c.Assert(err, IsNil)
}

func (s *DockerInterfaceSuite) TestSanitizePlugWithBadAttrib(c *C) {
	var mockSnapYaml = []byte(`name: docker-plug-snap
version: 1.0
plugs:
 docker-plug:
  interface: docker
  daemon-privileged: bad
`)

	info, err := snap.InfoFromSnapYaml(mockSnapYaml)
	c.Assert(err, IsNil)

	plug := &interfaces.Plug{PlugInfo: info.Plugs["docker-plug"]}
	err = s.iface.SanitizePlug(plug)
	c.Assert(err, Not(IsNil))
	c.Assert(err, ErrorMatches, "docker plug requires bool with 'daemon-privileged'")
}

func (s *DockerInterfaceSuite) TestConnectedPlugSnippetWithoutAttrib(c *C) {
	snippet, err := s.iface.ConnectedPlugSnippet(s.plug, s.slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(string(snippet), Not(testutil.Contains), `# TODO: privileged daemon apparmor policy`)

	snippet, err = s.iface.ConnectedPlugSnippet(s.plug, s.slot, interfaces.SecuritySecComp)
	c.Assert(err, IsNil)
	c.Assert(string(snippet), Not(testutil.Contains), `# TODO: privileged daemon seccomp policy`)
}

func (s *DockerInterfaceSuite) TestConnectedPlugSnippetWithAttribFalse(c *C) {
	var mockSnapYaml = []byte(`name: docker-plug-snap
version: 1.0
plugs:
 docker-plug:
  interface: docker
  daemon-privileged: false
`)

	info, err := snap.InfoFromSnapYaml(mockSnapYaml)
	c.Assert(err, IsNil)

	plug := &interfaces.Plug{PlugInfo: info.Plugs["docker-plug"]}

	snippet, err := s.iface.ConnectedPlugSnippet(plug, s.slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(string(snippet), Not(testutil.Contains), `# TODO: privileged daemon apparmor policy`)

	snippet, err = s.iface.ConnectedPlugSnippet(plug, s.slot, interfaces.SecuritySecComp)
	c.Assert(err, IsNil)
	c.Assert(string(snippet), Not(testutil.Contains), `# TODO: privileged daemon seccomp policy`)
}

func (s *DockerInterfaceSuite) TestConnectedPlugSnippetWithAttribTrue(c *C) {
	var mockSnapYaml = []byte(`name: docker-plug-snap
version: 1.0
plugs:
 docker-plug:
  interface: docker
  daemon-privileged: true
`)

	info, err := snap.InfoFromSnapYaml(mockSnapYaml)
	c.Assert(err, IsNil)

	plug := &interfaces.Plug{PlugInfo: info.Plugs["docker-plug"]}

	snippet, err := s.iface.ConnectedPlugSnippet(plug, s.slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(string(snippet), testutil.Contains, `# TODO: privileged daemon apparmor policy`)

	snippet, err = s.iface.ConnectedPlugSnippet(plug, s.slot, interfaces.SecuritySecComp)
	c.Assert(err, IsNil)
	c.Assert(string(snippet), testutil.Contains, `# TODO: privileged daemon seccomp policy`)
}
