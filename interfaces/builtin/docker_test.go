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
			Snap: &snap.Info{
				SuggestedName: "docker",
				SideInfo:      snap.SideInfo{Developer: "docker"},
			},
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

func (s *DockerInterfaceSuite) TestUsedSecuritySystems(c *C) {
	// connected plugs have a non-nil security snippet for apparmor
	snippet, err := s.iface.ConnectedPlugSnippet(s.plug, s.slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, Not(IsNil))
	// connected plugs have a non-nil security snippet for seccomp
	snippet, err = s.iface.ConnectedPlugSnippet(s.plug, s.slot, interfaces.SecuritySecComp)
	c.Assert(err, IsNil)
	c.Assert(snippet, Not(IsNil))
}

func (s *DockerInterfaceSuite) TestLegacyAutoConnect(c *C) {
	c.Check(s.iface.LegacyAutoConnect(), Equals, false)
}

func (s *DockerInterfaceSuite) TestConnectedPlugSnippet(c *C) {
	snippet, err := s.iface.ConnectedPlugSnippet(s.plug, s.slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(string(snippet), testutil.Contains, `run/docker.sock`)

	snippet, err = s.iface.ConnectedPlugSnippet(s.plug, s.slot, interfaces.SecuritySecComp)
	c.Assert(err, IsNil)
	c.Assert(string(snippet), testutil.Contains, `bind`)
}

func (s *DockerInterfaceSuite) TestSanitizeSlotDockerDev(c *C) {
	err := s.iface.SanitizeSlot(&interfaces.Slot{SlotInfo: &snap.SlotInfo{
		Snap: &snap.Info{
			SuggestedName: "docker",
			SideInfo:      snap.SideInfo{Developer: "docker"},
		},
		Name:      "docker",
		Interface: "docker",
	}})
	c.Assert(err, IsNil)
}

func (s *DockerInterfaceSuite) TestSanitizeSlotCanonicalDev(c *C) {
	err := s.iface.SanitizeSlot(&interfaces.Slot{SlotInfo: &snap.SlotInfo{
		Snap: &snap.Info{
			SuggestedName: "docker",
			SideInfo:      snap.SideInfo{Developer: "canonical"},
		},
		Name:      "docker",
		Interface: "docker",
	}})
	c.Assert(err, IsNil)
}

func (s *DockerInterfaceSuite) TestSanitizeSlotOtherDev(c *C) {
	err := s.iface.SanitizeSlot(&interfaces.Slot{SlotInfo: &snap.SlotInfo{
		Snap: &snap.Info{
			SuggestedName: "docker",
			SideInfo:      snap.SideInfo{Developer: "notdocker"},
		},
		Name:      "docker",
		Interface: "docker",
	}})
	c.Assert(err, ErrorMatches, "docker slot interface is reserved for the upstream docker project")
}

func (s *DockerInterfaceSuite) TestSanitizeSlotNotDockerDockerDev(c *C) {
	err := s.iface.SanitizeSlot(&interfaces.Slot{SlotInfo: &snap.SlotInfo{
		Snap: &snap.Info{
			SuggestedName: "notdocker",
			SideInfo:      snap.SideInfo{Developer: "docker"},
		},
		Name:      "notdocker",
		Interface: "docker",
	}})
	c.Assert(err, ErrorMatches, "docker slot interface is reserved for the upstream docker project")
}

func (s *DockerInterfaceSuite) TestSanitizeSlotNotDockerCanonicalDev(c *C) {
	err := s.iface.SanitizeSlot(&interfaces.Slot{SlotInfo: &snap.SlotInfo{
		Snap: &snap.Info{
			SuggestedName: "notdocker",
			SideInfo:      snap.SideInfo{Developer: "canonical"},
		},
		Name:      "notdocker",
		Interface: "docker",
	}})
	c.Assert(err, ErrorMatches, "docker slot interface is reserved for the upstream docker project")
}

func (s *DockerInterfaceSuite) TestSanitizeSlotNotDockerOtherDev(c *C) {
	err := s.iface.SanitizeSlot(&interfaces.Slot{SlotInfo: &snap.SlotInfo{
		Snap: &snap.Info{
			SuggestedName: "notdocker",
			SideInfo:      snap.SideInfo{Developer: "notdocker"},
		},
		Name:      "notdocker",
		Interface: "docker",
	}})
	c.Assert(err, ErrorMatches, "docker slot interface is reserved for the upstream docker project")
}

func (s *DockerInterfaceSuite) TestSanitizePlug(c *C) {
	err := s.iface.SanitizePlug(s.plug)
	c.Assert(err, IsNil)
}
