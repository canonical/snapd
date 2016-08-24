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

type LxdSupportInterfaceSuite struct {
	iface interfaces.Interface
	slot  *interfaces.Slot
	plug  *interfaces.Plug
}

var _ = Suite(&LxdSupportInterfaceSuite{
	iface: &builtin.LxdSupportInterface{},
	slot: &interfaces.Slot{
		SlotInfo: &snap.SlotInfo{
			Snap:      &snap.Info{SuggestedName: "ubuntu-core", Type: snap.TypeOS},
			Name:      "lxd-support",
			Interface: "lxd-support",
		},
	},

	plug: &interfaces.Plug{
		PlugInfo: &snap.PlugInfo{
			Snap: &snap.Info{
				SuggestedName: "lxd",
				SideInfo:      snap.SideInfo{Developer: "canonical"},
			},
			Name:      "lxd-support",
			Interface: "lxd-support",
		},
	},
})

func (s *LxdSupportInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "lxd-support")
}

func (s *LxdSupportInterfaceSuite) TestSanitizeSlot(c *C) {
	err := s.iface.SanitizeSlot(s.slot)
	c.Assert(err, IsNil)
}

func (s *LxdSupportInterfaceSuite) TestSanitizePlugLxdFromCanonical(c *C) {
	err := s.iface.SanitizePlug(s.plug)
	c.Assert(err, IsNil)
}

func (s *LxdSupportInterfaceSuite) TestSanitizePlugNotLxdFromCanonical(c *C) {
	err := s.iface.SanitizePlug(&interfaces.Plug{PlugInfo: &snap.PlugInfo{
		Snap: &snap.Info{
			SuggestedName: "notlxd",
			SideInfo:      snap.SideInfo{Developer: "canonical"},
		},
		Name:      "lxd-support",
		Interface: "lxd-support",
	}})
	c.Assert(err, ErrorMatches, "lxd-support plug reserved \\(snap name 'notlxd' != 'lxd'\\)")
}

func (s *LxdSupportInterfaceSuite) TestSanitizePlugLxdNotFromCanonical(c *C) {
	err := s.iface.SanitizePlug(&interfaces.Plug{PlugInfo: &snap.PlugInfo{
		Snap: &snap.Info{
			SuggestedName: "lxd",
			SideInfo:      snap.SideInfo{Developer: "foo"},
		},
		Name:      "lxd-support",
		Interface: "lxd-support",
	}})
	c.Assert(err, ErrorMatches, "lxd-support interface is reserved for the upstream LXD project")
}

func (s *LxdSupportInterfaceSuite) TestSanitizePlugNotLxdNotFromCanonical(c *C) {
	err := s.iface.SanitizePlug(&interfaces.Plug{PlugInfo: &snap.PlugInfo{
		Snap: &snap.Info{
			SuggestedName: "notlxd",
			SideInfo:      snap.SideInfo{Developer: "foo"},
		},
		Name:      "lxd-support",
		Interface: "lxd-support",
	}})
	c.Assert(err, ErrorMatches, "lxd-support plug reserved \\(snap name 'notlxd' != 'lxd'\\)")
}

func (s *LxdSupportInterfaceSuite) TestSanitizePlugLxdSideload(c *C) {
	err := s.iface.SanitizePlug(&interfaces.Plug{PlugInfo: &snap.PlugInfo{
		Snap: &snap.Info{
			SuggestedName: "lxd",
			SideInfo:      snap.SideInfo{Developer: ""},
		},
		Name:      "lxd-support",
		Interface: "lxd-support",
	}})
	c.Assert(err, ErrorMatches, "lxd-support interface is reserved for the upstream LXD project")
}

func (s *LxdSupportInterfaceSuite) TestUnusedSecuritySystems(c *C) {
	systems := [...]interfaces.SecuritySystem{interfaces.SecurityAppArmor,
		interfaces.SecuritySecComp, interfaces.SecurityDBus,
		interfaces.SecurityUDev}
	for _, system := range systems {
		snippet, err := s.iface.PermanentPlugSnippet(s.plug, system)
		c.Assert(err, IsNil)
		c.Assert(snippet, IsNil)
		snippet, err = s.iface.PermanentSlotSnippet(s.slot, system)
		c.Assert(err, IsNil)
		c.Assert(snippet, IsNil)
		snippet, err = s.iface.ConnectedSlotSnippet(s.plug, s.slot, system)
		c.Assert(err, IsNil)
		c.Assert(snippet, IsNil)
	}
	snippet, err := s.iface.ConnectedPlugSnippet(s.plug, s.slot, interfaces.SecurityDBus)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
	snippet, err = s.iface.ConnectedPlugSnippet(s.plug, s.slot, interfaces.SecurityUDev)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
}

func (s *LxdSupportInterfaceSuite) TestUsedSecuritySystems(c *C) {
	// connected plugs have a non-nil security snippet for apparmor
	snippet, err := s.iface.ConnectedPlugSnippet(s.plug, s.slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, Not(IsNil))
	// connected plugs have a non-nil security snippet for seccomp
	snippet, err = s.iface.ConnectedPlugSnippet(s.plug, s.slot, interfaces.SecuritySecComp)
	c.Assert(err, IsNil)
	c.Assert(snippet, Not(IsNil))
}

func (s *LxdSupportInterfaceSuite) TestUnexpectedSecuritySystems(c *C) {
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

func (s *LxdSupportInterfaceSuite) TestPermanentSlotPolicyAppArmor(c *C) {
	snippet, err := s.iface.ConnectedPlugSnippet(s.plug, s.slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Check(string(snippet), testutil.Contains, "/usr/sbin/aa-exec ux,\n")
}

func (s *LxdSupportInterfaceSuite) TestPermanentSlotPolicySecComp(c *C) {
	snippet, err := s.iface.ConnectedPlugSnippet(s.plug, s.slot, interfaces.SecuritySecComp)
	c.Assert(err, IsNil)
	c.Check(string(snippet), testutil.Contains, "@unrestricted\n")
}

func (s *LxdSupportInterfaceSuite) TestAutoConnect(c *C) {
	c.Check(s.iface.AutoConnect(), Equals, true)
}
