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

type LxdClientInterfaceSuite struct {
	iface interfaces.Interface
	slot  *interfaces.Slot
	plug  *interfaces.Plug
}

var _ = Suite(&LxdClientInterfaceSuite{
	iface: &builtin.LxdClientInterface{},
	slot: &interfaces.Slot{
		SlotInfo: &snap.SlotInfo{
			Snap:      &snap.Info{SuggestedName: "core", Type: snap.TypeOS},
			Name:      "lxd-client",
			Interface: "lxd-client",
		},
	},

	plug: &interfaces.Plug{
		PlugInfo: &snap.PlugInfo{
			Snap: &snap.Info{
				SuggestedName: "lxd",
				SideInfo:      snap.SideInfo{Developer: "canonical"},
			},
			Name:      "lxd-client",
			Interface: "lxd-client",
		},
	},
})

func (s *LxdClientInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "lxd-client")
}

func (s *LxdClientInterfaceSuite) TestSanitizeSlot(c *C) {
	err := s.iface.SanitizeSlot(s.slot)
	c.Assert(err, IsNil)
}

func (s *LxdClientInterfaceSuite) TestSanitizePlug(c *C) {
	err := s.iface.SanitizePlug(s.plug)
	c.Assert(err, IsNil)
}

func (s *LxdClientInterfaceSuite) TestUsedSecuritySystems(c *C) {
	// connected plugs have a non-nil security snippet for apparmor
	snippet, err := s.iface.ConnectedPlugSnippet(s.plug, s.slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, Not(IsNil))
}

func (s *LxdClientInterfaceSuite) TestPermanentSlotPolicyAppArmor(c *C) {
	snippet, err := s.iface.ConnectedPlugSnippet(s.plug, s.slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Check(string(snippet), testutil.Contains, "/var/snap/lxd/common/lxd/unix.socket rw,\n")
}

func (s *LxdClientInterfaceSuite) TestLegacyAutoConnect(c *C) {
	c.Check(s.iface.LegacyAutoConnect(), Equals, false)
}

func (s *LxdClientInterfaceSuite) TestAutoConnect(c *C) {
	c.Check(s.iface.AutoConnect(nil, nil), Equals, false)
}
