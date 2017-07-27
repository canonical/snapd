// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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
	"github.com/snapcore/snapd/interfaces/systemd"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type GpioInterfaceSuite struct {
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
	iface: builtin.MustInterface("gpio"),
})

func (s *GpioInterfaceSuite) SetUpTest(c *C) {
	gadgetInfo := snaptest.MockInfo(c, `
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
    bad-interface-slot: other-interface
plugs:
    plug: gpio
    bad-interface-plug: other-interface
`, nil)
	s.gadgetGpioSlot = &interfaces.Slot{SlotInfo: gadgetInfo.Slots["my-pin"]}
	s.gadgetMissingNumberSlot = &interfaces.Slot{SlotInfo: gadgetInfo.Slots["missing-number"]}
	s.gadgetBadNumberSlot = &interfaces.Slot{SlotInfo: gadgetInfo.Slots["bad-number"]}
	s.gadgetBadInterfaceSlot = &interfaces.Slot{SlotInfo: gadgetInfo.Slots["bad-interface-slot"]}
	s.gadgetPlug = &interfaces.Plug{PlugInfo: gadgetInfo.Plugs["plug"]}
	s.gadgetBadInterfacePlug = &interfaces.Plug{PlugInfo: gadgetInfo.Plugs["bad-interface-plug"]}

	osInfo := snaptest.MockInfo(c, `
name: my-core
type: os
slots:
    my-pin:
        interface: gpio
        number: 777
        direction: out
`, nil)
	s.osGpioSlot = &interfaces.Slot{SlotInfo: osInfo.Slots["my-pin"]}

	appInfo := snaptest.MockInfo(c, `
name: my-app
slots:
    my-pin:
        interface: gpio
        number: 154
        direction: out
`, nil)
	s.appGpioSlot = &interfaces.Slot{SlotInfo: appInfo.Slots["my-pin"]}
}

func (s *GpioInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "gpio")
}

func (s *GpioInterfaceSuite) TestSanitizeSlotGadgetSnap(c *C) {
	// gpio slot on gadget accepeted
	c.Assert(s.gadgetGpioSlot.Sanitize(s.iface), IsNil)

	// slots without number attribute are rejected
	c.Assert(s.gadgetMissingNumberSlot.Sanitize(s.iface), ErrorMatches,
		"gpio slot must have a number attribute")

	// slots with number attribute that isnt a number
	c.Assert(s.gadgetBadNumberSlot.Sanitize(s.iface), ErrorMatches,
		"gpio slot number attribute must be an int")
}

func (s *GpioInterfaceSuite) TestSanitizeSlotOsSnap(c *C) {
	// gpio slot on OS accepeted
	c.Assert(s.osGpioSlot.Sanitize(s.iface), IsNil)
}

func (s *GpioInterfaceSuite) TestSanitizeSlotAppSnap(c *C) {
	// gpio slot not accepted on app snap
	c.Assert(s.appGpioSlot.Sanitize(s.iface), ErrorMatches,
		"gpio slots are reserved for the operating system and gadget snaps")
}

func (s *GpioInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(s.gadgetPlug.Sanitize(s.iface), IsNil)
}

func (s *GpioInterfaceSuite) TestSystemdConnectedSlot(c *C) {
	spec := &systemd.Specification{}
	err := spec.AddConnectedSlot(s.iface, s.gadgetPlug, nil, s.gadgetGpioSlot, nil)
	c.Assert(err, IsNil)
	c.Assert(spec.Services(), DeepEquals, map[string]*systemd.Service{
		"snap.my-device.interface.gpio-100.service": {
			Type:            "oneshot",
			RemainAfterExit: true,
			ExecStart:       `/bin/sh -c 'test -e /sys/class/gpio/gpio100 || echo 100 > /sys/class/gpio/export'`,
			ExecStop:        `/bin/sh -c 'test ! -e /sys/class/gpio/gpio100 || echo 100 > /sys/class/gpio/unexport'`,
		},
	})
}

func (s *GpioInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
