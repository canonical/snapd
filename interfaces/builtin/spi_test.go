// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type spiInterfaceSuite struct {
	testutil.BaseTest
	iface interfaces.Interface

	slotOs1 *interfaces.Slot
	slotOs2 *interfaces.Slot

	slotGadget1    *interfaces.Slot
	slotGadget2    *interfaces.Slot
	slotGadgetBad1 *interfaces.Slot
	slotGadgetBad2 *interfaces.Slot
	slotGadgetBad3 *interfaces.Slot
	slotGadgetBad4 *interfaces.Slot
	slotGadgetBad5 *interfaces.Slot
	slotGadgetBad6 *interfaces.Slot

	plug1 *interfaces.Plug
	plug2 *interfaces.Plug
}

var _ = Suite(&spiInterfaceSuite{
	iface: builtin.MustInterface("spi"),
})

func (s *spiInterfaceSuite) SetUpTest(c *C) {
	info := snaptest.MockInfo(c, `
name: core
type: os
slots:
  spi-1:
    interface: spi
    path: /dev/spidev0.0
  spi-2:
    interface: spi
    path: /dev/spidev0.1
`, nil)
	s.slotOs1 = &interfaces.Slot{SlotInfo: info.Slots["spi-1"]}
	s.slotOs2 = &interfaces.Slot{SlotInfo: info.Slots["spi-2"]}

	info = snaptest.MockInfo(c, `
name: gadget
type: gadget
slots:
  spi-1:
    interface: spi
    path: /dev/spidev0.0
  spi-2:
    interface: spi
    path: /dev/spidev0.1
  bad-spi-1:
    interface: spi
    path: /dev/spev0.0
  bad-spi-2:
    interface: spi
    path: /dev/sidv0.0
  bad-spi-3:
    interface: spi
    path: /dev/slpiv0.3
  bad-spi-4:
    interface: spi
    path: /dev/sdev-00
  bad-spi-5:
    interface: spi
    path: /dev/spi-foo
  bad-spi-6:
    interface: spi
`, nil)
	s.slotGadget1 = &interfaces.Slot{SlotInfo: info.Slots["spi-1"]}
	s.slotGadget2 = &interfaces.Slot{SlotInfo: info.Slots["spi-2"]}
	s.slotGadgetBad1 = &interfaces.Slot{SlotInfo: info.Slots["bad-spi-1"]}
	s.slotGadgetBad2 = &interfaces.Slot{SlotInfo: info.Slots["bad-spi-2"]}
	s.slotGadgetBad3 = &interfaces.Slot{SlotInfo: info.Slots["bad-spi-3"]}
	s.slotGadgetBad4 = &interfaces.Slot{SlotInfo: info.Slots["bad-spi-4"]}
	s.slotGadgetBad5 = &interfaces.Slot{SlotInfo: info.Slots["bad-spi-5"]}
	s.slotGadgetBad6 = &interfaces.Slot{SlotInfo: info.Slots["bad-spi-6"]}

	info = snaptest.MockInfo(c, `
name: consumer
plugs:
  spi-1:
    interface: spi
    path: /dev/spidev.0.0
  spi-2:
    interface: spi
    path: /dev/spidev0.1
apps:
  app:
    command: foo
    plugs: [spi-1]
`, nil)
	s.plug1 = &interfaces.Plug{PlugInfo: info.Plugs["spi-1"]}
	s.plug2 = &interfaces.Plug{PlugInfo: info.Plugs["spi-2"]}
}

func (s *spiInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "spi")
}

func (s *spiInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(s.slotOs1.Sanitize(s.iface), IsNil)
	c.Assert(s.slotOs2.Sanitize(s.iface), IsNil)
	c.Assert(s.slotGadget1.Sanitize(s.iface), IsNil)
	c.Assert(s.slotGadget2.Sanitize(s.iface), IsNil)
	err := s.slotGadgetBad1.Sanitize(s.iface)
	c.Assert(err, ErrorMatches, `"/dev/spev0.0" is not a valid SPI device`)
	err = s.slotGadgetBad2.Sanitize(s.iface)
	c.Assert(err, ErrorMatches, `"/dev/sidv0.0" is not a valid SPI device`)
	err = s.slotGadgetBad3.Sanitize(s.iface)
	c.Assert(err, ErrorMatches, `"/dev/slpiv0.3" is not a valid SPI device`)
	err = s.slotGadgetBad4.Sanitize(s.iface)
	c.Assert(err, ErrorMatches, `"/dev/sdev-00" is not a valid SPI device`)
	err = s.slotGadgetBad5.Sanitize(s.iface)
	c.Assert(err, ErrorMatches, `"/dev/spi-foo" is not a valid SPI device`)
	err = s.slotGadgetBad6.Sanitize(s.iface)
	c.Assert(err, ErrorMatches, `slot "gadget:bad-spi-6" must have a path attribute`)
	slot := &interfaces.Slot{SlotInfo: &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "some-snap"},
		Name:      "spi",
		Interface: "spi",
	}}
	c.Assert(slot.Sanitize(s.iface), ErrorMatches,
		"spi slots are reserved for the core and gadget snaps")
}

func (s *spiInterfaceSuite) TestUDevSpec(c *C) {
	spec := &udev.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug1, nil, s.slotGadget1, nil), IsNil)
	c.Assert(spec.Snippets(), HasLen, 2)
	c.Assert(spec.Snippets(), testutil.Contains, `# spi
KERNEL=="spidev0.0", TAG+="snap_consumer_app"`)
}

func (s *spiInterfaceSuite) TestAppArmorSpec(c *C) {
	spec := &apparmor.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug1, nil, s.slotGadget1, nil), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), Equals, ""+
		"/dev/spidev0.0 rw,\n"+
		"/sys/devices/platform/**/**.spi/**/spidev0.0/** rw,")
}

func (s *spiInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, false)
	c.Assert(si.ImplicitOnClassic, Equals, false)
	c.Assert(si.Summary, Equals, "allows access to specific spi controller")
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "spi")
}

func (s *spiInterfaceSuite) TestAutoConnect(c *C) {
	c.Check(s.iface.AutoConnect(nil, nil), Equals, true)
}

func (s *spiInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
