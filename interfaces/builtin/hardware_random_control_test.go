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
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type HardwareRandomControlInterfaceSuite struct {
	iface interfaces.Interface
	slot  *interfaces.Slot
	plug  *interfaces.Plug
}

var _ = Suite(&HardwareRandomControlInterfaceSuite{
	iface: builtin.MustInterface("hardware-random-control"),
})

const hardwareRandomControlConsumerYaml = `name: consumer
apps:
 app:
  plugs: [hardware-random-control]
`

const hardwareRandomControlCoreYaml = `name: core
type: os
slots:
  hardware-random-control:
`

func (s *HardwareRandomControlInterfaceSuite) SetUpTest(c *C) {
	s.plug = MockPlug(c, hardwareRandomControlConsumerYaml, nil, "hardware-random-control")
	s.slot = MockSlot(c, hardwareRandomControlCoreYaml, nil, "hardware-random-control")
}

func (s *HardwareRandomControlInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "hardware-random-control")
}

func (s *HardwareRandomControlInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(s.slot.Sanitize(s.iface), IsNil)
	slot := &interfaces.Slot{SlotInfo: &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "some-snap"},
		Name:      "hardware-random-control",
		Interface: "hardware-random-control",
	}}
	c.Assert(slot.Sanitize(s.iface), ErrorMatches,
		"hardware-random-control slots are reserved for the core snap")
}

func (s *HardwareRandomControlInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(s.plug.Sanitize(s.iface), IsNil)
}

func (s *HardwareRandomControlInterfaceSuite) TestAppArmorSpec(c *C) {
	spec := &apparmor.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, nil, s.slot, nil), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "hw_random/rng_current w,")
}

func (s *HardwareRandomControlInterfaceSuite) TestUDevSpec(c *C) {
	spec := &udev.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, nil, s.slot, nil), IsNil)
	c.Assert(spec.Snippets(), HasLen, 1)
	c.Assert(spec.Snippets(), testutil.Contains, `# hardware-random-control
KERNEL=="hwrng", TAG+="snap_consumer_app"`)
}

func (s *HardwareRandomControlInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, true)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows control over the hardware random number generator`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "hardware-random-control")
}

func (s *HardwareRandomControlInterfaceSuite) TestAutoConnect(c *C) {
	c.Assert(s.iface.AutoConnect(s.plug, s.slot), Equals, true)
}

func (s *HardwareRandomControlInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
