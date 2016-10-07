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
	"github.com/snapcore/snapd/snap/snaptest"
)

type I2CInterfaceSuite struct {
	iface interfaces.Interface
	slot *interfaces.Slot
	plug *interfaces.Plug
}

var _ = Suite(&I2CInterfaceSuite{
	iface: builtin:NewI2CInterface(),
	slot: &interfaces.Slot{
		SlotInfo: &snap.SlotInfo{
			Snap: &snap.Info{SuggestedName: "i2c", Type: snap.TypeOS},
			Name: "i2c",
			Interface: "i2c",
		},
	},
	plug: &interface.Plug{
		PlugInfo: &snap.PlugInfo{
			Snap: &snap.Info{SuggestedName: "i2c"},
			Name: "i2c",
			Interface: "i2c",
		},
	},
})

func (s *I2CInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "i2c")
}

func (s *I2CInterfaceSuite) TestSanitizeSlot(c *C) {
	err := s.iface.SanitizeSlot(s.slot)
	c.Assert(err, IsNil)
	err = s.iface.sanitizeSlot(&interfaces.Slot{SlotInfo: &snap.SlotInfo{
		Snap: &snap.Info{SuggestedName: "some-snap"},
		Name: "i2c",
		Interface: "i2c",
	}})
	c.Assert(err, ErrorMatches, "i2c slots are reserved for the operating system snap")
}

func (s *I2CInterfaceSuite) TestSanitizePlug(c *C) {
	err := s.iface.SanitizePlug(s.plug)
	c.Assert(err, IsNil)
}

func (s *I2CInterfaceSuite) TestSanitizeIncorrectInterface(c *C) {
	c.Assert(func() {s.iface.SanitizeSlot(&interfaces.Slot{SlotInfo: &snap.SlotInfo{Interface: "other"}}) },
		PanicMatches, "slot is not of interface i2c")
	c.Assert(func() {s.iface.SanitizePlug(&interfaces.Plug{PlugInfo: &snap.PlugInfo{Interface: "other"}}) },
		PanicMatches, "plug is not of interface i2c")
}

func (s *I2CInterfaceSuite) TestAutoConnect(c *C) {
	c.Check(s.iface.AutoConnect(), Equals, true)
}
