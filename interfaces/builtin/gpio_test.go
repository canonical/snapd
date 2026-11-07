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
	"os"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/systemd"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type GpioInterfaceSuite struct {
	testutil.BaseTest

	iface                       interfaces.Interface
	gadgetGpioSlotInfo          *snap.SlotInfo
	gadgetGpioSlot              *interfaces.ConnectedSlot
	gadgetMissingNumberSlotInfo *snap.SlotInfo
	gadgetMissingNumberSlot     *interfaces.ConnectedSlot
	gadgetBadNumberSlotInfo     *snap.SlotInfo
	gadgetBadNumberSlot         *interfaces.ConnectedSlot
	gadgetBadInterfaceSlotInfo  *snap.SlotInfo
	gadgetBadInterfaceSlot      *interfaces.ConnectedSlot
	gadgetPlugInfo              *snap.PlugInfo
	gadgetPlug                  *interfaces.ConnectedPlug
	gadgetBadInterfacePlugInfo  *snap.PlugInfo
	gadgetBadInterfacePlug      *interfaces.ConnectedPlug
	osGpioSlotInfo              *snap.SlotInfo
	osGpioSlot                  *interfaces.ConnectedSlot
}

var _ = Suite(&GpioInterfaceSuite{
	iface: builtin.MustInterface("gpio"),
})

func (s *GpioInterfaceSuite) SetUpTest(c *C) {
	gadgetInfo := snaptest.MockInfo(c, `
name: my-device
version: 0
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
apps:
    svc:
        command: bin/foo.sh
`, nil)
	appSet, err := interfaces.NewSnapAppSet(gadgetInfo, nil)
	c.Assert(err, IsNil)

	s.gadgetGpioSlotInfo = gadgetInfo.Slots["my-pin"]
	s.gadgetGpioSlot = interfaces.NewConnectedSlot(s.gadgetGpioSlotInfo, appSet, nil, nil)
	s.gadgetMissingNumberSlotInfo = gadgetInfo.Slots["missing-number"]
	s.gadgetMissingNumberSlot = interfaces.NewConnectedSlot(s.gadgetMissingNumberSlotInfo, appSet, nil, nil)
	s.gadgetBadNumberSlotInfo = gadgetInfo.Slots["bad-number"]
	s.gadgetBadNumberSlot = interfaces.NewConnectedSlot(s.gadgetBadNumberSlotInfo, appSet, nil, nil)
	s.gadgetBadInterfaceSlotInfo = gadgetInfo.Slots["bad-interface-slot"]
	s.gadgetBadInterfaceSlot = interfaces.NewConnectedSlot(s.gadgetBadInterfaceSlotInfo, appSet, nil, nil)
	s.gadgetPlugInfo = gadgetInfo.Plugs["plug"]
	s.gadgetPlug = interfaces.NewConnectedPlug(s.gadgetPlugInfo, appSet, nil, nil)
	s.gadgetBadInterfacePlugInfo = gadgetInfo.Plugs["bad-interface-plug"]
	s.gadgetBadInterfacePlug = interfaces.NewConnectedPlug(s.gadgetBadInterfacePlugInfo, appSet, nil, nil)

	osInfo := snaptest.MockInfo(c, `
name: my-core
version: 0
type: os
slots:
    my-pin:
        interface: gpio
        number: 777
        direction: out
`, nil)
	appSet, err = interfaces.NewSnapAppSet(osInfo, nil)
	c.Assert(err, IsNil)

	s.osGpioSlotInfo = osInfo.Slots["my-pin"]
	s.osGpioSlot = interfaces.NewConnectedSlot(s.osGpioSlotInfo, appSet, nil, nil)
}

func (s *GpioInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "gpio")
}

func (s *GpioInterfaceSuite) TestSanitizeSlotGadgetSnap(c *C) {
	// gpio slot on gadget accepeted
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.gadgetGpioSlotInfo), IsNil)

	// slots without number attribute are rejected
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.gadgetMissingNumberSlotInfo), ErrorMatches,
		"gpio slot must have a number attribute")

	// slots with number attribute that isnt a number
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.gadgetBadNumberSlotInfo), ErrorMatches,
		"gpio slot number attribute must be an int")
}

func (s *GpioInterfaceSuite) TestSanitizeSlotOsSnap(c *C) {
	// gpio slot on OS accepeted
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.osGpioSlotInfo), IsNil)
}

func (s *GpioInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.gadgetPlugInfo), IsNil)
}

func (s *GpioInterfaceSuite) TestSystemdConnectedSlot(c *C) {
	spec := &systemd.Specification{}
	err := spec.AddConnectedSlot(s.iface, s.gadgetPlug, s.gadgetGpioSlot)
	c.Assert(err, IsNil)
	c.Assert(spec.Services(), DeepEquals, map[string]*systemd.Service{
		"gpio-100": {
			Type:            "oneshot",
			RemainAfterExit: true,
			ExecStart:       `/bin/sh -c 'test -e /sys/class/gpio/gpio100 || echo 100 > /sys/class/gpio/export'`,
			ExecStop:        `/bin/sh -c 'test ! -e /sys/class/gpio/gpio100 || echo 100 > /sys/class/gpio/unexport'`,
		},
	})
}

func (s *GpioInterfaceSuite) TestApparmorConnectedPlugIgnoresMissingSymlink(c *C) {
	log, restore := logger.MockLogger()
	defer restore()

	builtin.MockEvalSymlinks(&s.BaseTest, func(path string) (string, error) {
		c.Assert(path, Equals, "/sys/class/gpio/gpio100")
		return "", os.ErrNotExist
	})

	spec := apparmor.NewSpecification(s.gadgetPlug.AppSet())
	err := spec.AddConnectedPlug(s.iface, s.gadgetPlug, s.gadgetGpioSlot)
	c.Assert(err, IsNil)
	c.Assert(spec.Snippets(), HasLen, 0)
	c.Assert(log.String(), testutil.Contains, "cannot export not existing gpio /sys/class/gpio/gpio100")
}

func (s *GpioInterfaceSuite) TestApparmorConnectedPlug(c *C) {
	builtin.MockEvalSymlinks(&s.BaseTest, func(path string) (string, error) {
		c.Assert(path, Equals, "/sys/class/gpio/gpio100")
		// TODO: what is this actually a symlink to on a real device?
		return "/sys/dev/foo/class/gpio/gpio100", nil
	})

	spec := apparmor.NewSpecification(s.gadgetPlug.AppSet())
	err := spec.AddConnectedPlug(s.iface, s.gadgetPlug, s.gadgetGpioSlot)
	c.Assert(err, IsNil)
	c.Assert(spec.SnippetForTag("snap.my-device.svc"), testutil.Contains, `/sys/dev/foo/class/gpio/gpio100/* rwk`)
}

func (s *GpioInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
