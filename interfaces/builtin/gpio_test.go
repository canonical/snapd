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

type GpioInterfaceSuite struct {
	testutil.BaseTest
	iface                   interfaces.Interface
	gadgetGpioSlot          *interfaces.Slot
	gadgetMissingNumberSlot *interfaces.Slot
	gadgetBadNumberSlot     *interfaces.Slot
	gadgetBadInterfaceSlot  *interfaces.Slot
	gadgetPlug              *interfaces.Plug
	gadgetBadInterfacePlug  *interfaces.Plug
	osGpioSlot              *interfaces.Slot
	appGpioSlot             *interfaces.Slot
}

var _ = Suite(&GpioInterfaceSuite{
	iface: &builtin.GpioInterface{},
})

func (s *GpioInterfaceSuite) SetUpTest(c *C) {
	gadgetInfo, gadgetErr := snap.InfoFromSnapYaml([]byte(`
name: my-device
type: gadget
slots:
    my-pin:
        interface: gpio
        number: 100
    missing-number:
        interface: gpio
    bad-number:
        interface: gpio
        number: forty-two
    bad-interface: other-interface
plugs:
    plug: gpio
    bad-interface: other-interface
`))
	c.Assert(gadgetErr, IsNil)
	s.gadgetGpioSlot = &interfaces.Slot{SlotInfo: gadgetInfo.Slots["my-pin"]}
	s.gadgetMissingNumberSlot = &interfaces.Slot{SlotInfo: gadgetInfo.Slots["missing-number"]}
	s.gadgetBadNumberSlot = &interfaces.Slot{SlotInfo: gadgetInfo.Slots["bad-number"]}
	s.gadgetBadInterfaceSlot = &interfaces.Slot{SlotInfo: gadgetInfo.Slots["bad-interface"]}
	s.gadgetPlug = &interfaces.Plug{PlugInfo: gadgetInfo.Plugs["plug"]}
	s.gadgetBadInterfacePlug = &interfaces.Plug{PlugInfo: gadgetInfo.Plugs["bad-interface"]}

	osInfo, osErr := snap.InfoFromSnapYaml([]byte(`
name: my-core
type: os
slots:
    my-pin:
        interface: gpio
        number: 777
        direction: out
`))
	c.Assert(osErr, IsNil)
	s.osGpioSlot = &interfaces.Slot{SlotInfo: osInfo.Slots["my-pin"]}

	appInfo, appErr := snap.InfoFromSnapYaml([]byte(`
name: my-app
slots:
    my-pin:
        interface: gpio
        number: 154
        direction: out
`))
	c.Assert(appErr, IsNil)
	s.appGpioSlot = &interfaces.Slot{SlotInfo: appInfo.Slots["my-pin"]}
}

func (s *GpioInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "gpio")
}

func (s *GpioInterfaceSuite) TestSanitizeSlotGadgetSnap(c *C) {
	// gpio slot on gadget accepted
	err := s.iface.SanitizeSlot(s.gadgetGpioSlot)
	c.Assert(err, IsNil)

	// slots without number attribute are rejected
	err = s.iface.SanitizeSlot(s.gadgetMissingNumberSlot)
	c.Assert(err, ErrorMatches, "gpio slot must have a number attribute")

	// slots with number attribute that isn't a number
	err = s.iface.SanitizeSlot(s.gadgetBadNumberSlot)
	c.Assert(err, ErrorMatches, "gpio slot number attribute must be an int")

	// Must be right interface type
	c.Assert(func() { s.iface.SanitizeSlot(s.gadgetBadInterfaceSlot) }, PanicMatches, `slot is not of interface "gpio"`)
}

func (s *GpioInterfaceSuite) TestSanitizeSlotOsSnap(c *C) {
	// gpio slot on OS accepted
	err := s.iface.SanitizeSlot(s.osGpioSlot)
	c.Assert(err, IsNil)
}

func (s *GpioInterfaceSuite) TestSanitizeSlotAppSnap(c *C) {
	// gpio slot not accepted on app snap
	err := s.iface.SanitizeSlot(s.appGpioSlot)
	c.Assert(err, ErrorMatches, "gpio slots only allowed on gadget or core snaps")
}

func (s *GpioInterfaceSuite) TestSanitizePlug(c *C) {
	err := s.iface.SanitizePlug(s.gadgetPlug)
	c.Assert(err, IsNil)

	// It is impossible to use "bool-file" interface to sanitize plugs of different interface.
	c.Assert(func() { s.iface.SanitizePlug(s.gadgetBadInterfacePlug) }, PanicMatches, `plug is not of interface "gpio"`)
}
