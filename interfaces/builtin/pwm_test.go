// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

type PwmInterfaceSuite struct {
	testutil.BaseTest

	iface                           interfaces.Interface
	gadgetPwmSlotInfo               *snap.SlotInfo
	gadgetPwmSlot                   *interfaces.ConnectedSlot
	gadgetMissingChannelSlotInfo    *snap.SlotInfo
	gadgetMissingChannelSlot        *interfaces.ConnectedSlot
	gadgetBadChannelSlotInfo        *snap.SlotInfo
	gadgetBadChannelSlot            *interfaces.ConnectedSlot
	gadgetMissingChipNumberSlotInfo *snap.SlotInfo
	gadgetMissingChipNumberSlot     *interfaces.ConnectedSlot
	gadgetBadChipNumberSlotInfo     *snap.SlotInfo
	gadgetBadChipNumberSlot         *interfaces.ConnectedSlot
	gadgetBadInterfaceSlotInfo      *snap.SlotInfo
	gadgetBadInterfaceSlot          *interfaces.ConnectedSlot
	gadgetPlugInfo                  *snap.PlugInfo
	gadgetPlug                      *interfaces.ConnectedPlug
	gadgetBadInterfacePlugInfo      *snap.PlugInfo
	gadgetBadInterfacePlug          *interfaces.ConnectedPlug
	osPwmSlotInfo                   *snap.SlotInfo
	osPwmSlot                       *interfaces.ConnectedSlot
}

var _ = Suite(&PwmInterfaceSuite{
	iface: builtin.MustInterface("pwm"),
})

func (s *PwmInterfaceSuite) SetUpTest(c *C) {
	gadgetInfo := snaptest.MockInfo(c, `
name: my-device
version: 0
type: gadget
slots:
    my-pin:
        interface: pwm
        chip-number: 10
        channel: 100
    missing-channel:
        interface: pwm
        chip-number: 10
    bad-channel:
        interface: pwm
        chip-number: 10
        channel: forty-two
    missing-chip-number:
        interface: pwm
        channel: 100
    bad-chip-number:
        interface: pwm
        chip-number: forty-two
        channel: 100
    bad-interface-slot: other-interface
plugs:
    plug: pwm
    bad-interface-plug: other-interface
apps:
    svc:
        command: bin/foo.sh
`, nil)
	s.gadgetPwmSlotInfo = gadgetInfo.Slots["my-pin"]
	s.gadgetPwmSlot = interfaces.NewConnectedSlot(s.gadgetPwmSlotInfo, nil, nil)
	s.gadgetMissingChannelSlotInfo = gadgetInfo.Slots["missing-channel"]
	s.gadgetMissingChannelSlot = interfaces.NewConnectedSlot(s.gadgetMissingChannelSlotInfo, nil, nil)
	s.gadgetBadChannelSlotInfo = gadgetInfo.Slots["bad-channel"]
	s.gadgetBadChannelSlot = interfaces.NewConnectedSlot(s.gadgetBadChannelSlotInfo, nil, nil)
	s.gadgetMissingChipNumberSlotInfo = gadgetInfo.Slots["missing-chip-number"]
	s.gadgetMissingChipNumberSlot = interfaces.NewConnectedSlot(s.gadgetMissingChipNumberSlotInfo, nil, nil)
	s.gadgetBadChipNumberSlotInfo = gadgetInfo.Slots["bad-chip-number"]
	s.gadgetBadChipNumberSlot = interfaces.NewConnectedSlot(s.gadgetBadChipNumberSlotInfo, nil, nil)
	s.gadgetBadInterfaceSlotInfo = gadgetInfo.Slots["bad-interface-slot"]
	s.gadgetBadInterfaceSlot = interfaces.NewConnectedSlot(s.gadgetBadInterfaceSlotInfo, nil, nil)
	s.gadgetPlugInfo = gadgetInfo.Plugs["plug"]
	s.gadgetPlug = interfaces.NewConnectedPlug(s.gadgetPlugInfo, nil, nil)
	s.gadgetBadInterfacePlugInfo = gadgetInfo.Plugs["bad-interface-plug"]
	s.gadgetBadInterfacePlug = interfaces.NewConnectedPlug(s.gadgetBadInterfacePlugInfo, nil, nil)

	osInfo := snaptest.MockInfo(c, `
name: my-core
version: 0
type: os
slots:
    my-pin:
        interface: pwm
        chip-number: 10
        channel: 7
`, nil)
	s.osPwmSlotInfo = osInfo.Slots["my-pin"]
	s.osPwmSlot = interfaces.NewConnectedSlot(s.osPwmSlotInfo, nil, nil)
}

func (s *PwmInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "pwm")
}

func (s *PwmInterfaceSuite) TestSanitizeSlotGadgetSnap(c *C) {
	// pwm slot on gadget accepeted
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.gadgetPwmSlotInfo), IsNil)

	// slots without channel attribute are rejected
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.gadgetMissingChannelSlotInfo), ErrorMatches,
		"pwm slot must have a channel attribute")

	// slots with channel attribute that isnt a number
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.gadgetBadChannelSlotInfo), ErrorMatches,
		"pwm slot channel attribute must be an int")

	// slots without chip-number attribute are rejected
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.gadgetMissingChipNumberSlotInfo), ErrorMatches,
		"pwm slot must have a chip-number attribute")

	// slots with chip-number attribute that isnt a number
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.gadgetBadChipNumberSlotInfo), ErrorMatches,
		"pwm slot chip-number attribute must be an int")
}

func (s *PwmInterfaceSuite) TestSanitizeSlotOsSnap(c *C) {
	// pwm slot on OS accepted
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.osPwmSlotInfo), IsNil)
}

func (s *PwmInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.gadgetPlugInfo), IsNil)
}

func (s *PwmInterfaceSuite) TestSystemdConnectedSlot(c *C) {
	spec := &systemd.Specification{}
	err := spec.AddConnectedSlot(s.iface, s.gadgetPlug, s.gadgetPwmSlot)
	c.Assert(err, IsNil)
	c.Assert(spec.Services(), DeepEquals, map[string]*systemd.Service{
		"pwmchip10-pwm100": {
			Type:            "oneshot",
			RemainAfterExit: true,
			ExecStart:       `/bin/sh -c 'test -e /sys/class/pwm/pwmchip10/pwm100 || echo 100 > /sys/class/pwm/pwmchip10/export'`,
			ExecStop:        `/bin/sh -c 'test ! -e /sys/class/pwm/pwmchip10/pwm100 || echo 100 > /sys/class/pwm/pwmchip10/unexport'`,
		},
	})
}

func (s *PwmInterfaceSuite) TestApparmorConnectedPlugIgnoresMissingSymlink(c *C) {
	log, restore := logger.MockLogger()
	defer restore()

	builtin.MockEvalSymlinks(&s.BaseTest, func(path string) (string, error) {
		c.Assert(path, Equals, "/sys/class/pwm/pwmchip10")
		return "", os.ErrNotExist
	})

	spec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.gadgetPlug.Snap()))
	err := spec.AddConnectedPlug(s.iface, s.gadgetPlug, s.gadgetPwmSlot)
	c.Assert(err, IsNil)
	c.Assert(spec.Snippets(), HasLen, 0)
	c.Assert(log.String(), testutil.Contains, "cannot find not existing pwm chipbase /sys/class/pwm/pwmchip10")
}

func (s *PwmInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}

func (s *PwmInterfaceSuite) TestApparmorConnectedPlug(c *C) {
	builtin.MockEvalSymlinks(&s.BaseTest, func(path string) (string, error) {
		c.Assert(path, Equals, "/sys/class/pwm/pwmchip10")
		// TODO: what is this actually a symlink to on a real device?
		return "/sys/dev/foo/class/pwm/pwmchip10", nil
	})

	spec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.gadgetPlug.Snap()))
	err := spec.AddConnectedPlug(s.iface, s.gadgetPlug, s.gadgetPwmSlot)
	c.Assert(err, IsNil)
	c.Assert(spec.SnippetForTag("snap.my-device.svc"), testutil.Contains, `/sys/dev/foo/class/pwm/pwmchip10/pwm100/* rwk`)
}
