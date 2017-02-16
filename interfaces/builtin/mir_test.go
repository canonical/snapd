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
)

type MirInterfaceSuite struct {
	iface interfaces.Interface
	slot  *interfaces.Slot
	plug  *interfaces.Plug
}

var _ = Suite(&MirInterfaceSuite{
	iface: &builtin.MirInterface{},
	slot: &interfaces.Slot{
		SlotInfo: &snap.SlotInfo{
			Snap:      &snap.Info{SuggestedName: "mir-server", Type: snap.TypeOS},
			Name:      "mir-server",
			Interface: "mir",
		},
	},
	plug: &interfaces.Plug{
		PlugInfo: &snap.PlugInfo{
			Snap:      &snap.Info{SuggestedName: "other"},
			Name:      "mir-client",
			Interface: "mir",
		},
	},
})

func (s *MirInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "mir")
}

func (s *MirInterfaceSuite) TestUsedSecuritySystems(c *C) {
	systems := [...]interfaces.SecuritySystem{interfaces.SecurityAppArmor,
		interfaces.SecuritySecComp}
	for _, system := range systems {
		snippet, err := s.iface.PermanentSlotSnippet(s.slot, system)
		c.Assert(err, IsNil)
		c.Assert(snippet, Not(IsNil))
		if system != interfaces.SecuritySecComp {
			snippet, err := s.iface.ConnectedSlotSnippet(s.plug, s.slot, system)
			c.Assert(err, IsNil)
			c.Assert(snippet, Not(IsNil))
			snippet, err = s.iface.ConnectedPlugSnippet(s.plug, s.slot, system)
			c.Assert(err, IsNil)
			c.Assert(snippet, Not(IsNil))
		}
	}
}
