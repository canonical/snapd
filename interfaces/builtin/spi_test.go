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
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type spiInterfaceSuite struct {
	testutil.BaseTest
	iface interfaces.Interface

	// OS Snap
	testSlot1 *interfaces.Slot
	testSlot2 *interfaces.Slot

	// Gadget Snap
	testUDev1         *interfaces.Slot
	testUDev2         *interfaces.Slot
	testUDevBadValue1 *interfaces.Slot
	testUDevBadValue2 *interfaces.Slot
	testUDevBadValue3 *interfaces.Slot
	testUDevBadValue4 *interfaces.Slot
	testUDevBadValue5 *interfaces.Slot

	// Consuming Snap
	testPlugPort1 *interfaces.Plug
	testPlugPort2 *interfaces.Plug
}

var _ = Suite(&spiInterfaceSuite{
	iface: builtin.MustInterface("spi"),
})

func (s *spiInterfaceSuite) SetUpTest(c *C) {
	// Mock for OS Snap
	osSnapInfo := snaptest.MockInfo(c, `
name: ubuntu-core
type: os
slots:
  test-port-1:
    interface: spi
    path: /dev/spidev0.0
  test-port-2:
    interface: spi
    path: /dev/spidev0.1
`, nil)
	s.testSlot1 = &interfaces.Slot{SlotInfo: osSnapInfo.Slots["test-port-1"]}
	s.testSlot2 = &interfaces.Slot{SlotInfo: osSnapInfo.Slots["test-port-2"]}

	// Mock for Gadget Snap
	gadgetSnapInfo := snaptest.MockInfo(c, `
name: some-device
type: gadget
slots:
  test-udev-1:
    interface: spi
    path: /dev/spidev0.0
  test-udev-2:
    interface: spi
    path: /dev/spidev0.1
  test-udev-bad-value-1:
    interface: spi
    path: /dev/spev0.0
  test-udev-bad-value-2:
    interface: spi
    path: /dev/sidv0.0
  test-udev-bad-value-3:
    interface: spi
    path: /dev/slpiv0.3
  test-udev-bad-value-4:
    interface: spi
    path: /dev/sdev-00
  test-udev-bad-value-5:
    interface: spi
    path: /dev/spi-foo
`, nil)
	s.testUDev1 = &interfaces.Slot{SlotInfo: gadgetSnapInfo.Slots["test-udev-1"]}
	s.testUDev2 = &interfaces.Slot{SlotInfo: gadgetSnapInfo.Slots["test-udev-2"]}
	s.testUDevBadValue1 = &interfaces.Slot{SlotInfo: gadgetSnapInfo.Slots["test-udev-bad-value-1"]}
	s.testUDevBadValue2 = &interfaces.Slot{SlotInfo: gadgetSnapInfo.Slots["test-udev-bad-value-2"]}
	s.testUDevBadValue3 = &interfaces.Slot{SlotInfo: gadgetSnapInfo.Slots["test-udev-bad-value-3"]}
	s.testUDevBadValue4 = &interfaces.Slot{SlotInfo: gadgetSnapInfo.Slots["test-udev-bad-value-4"]}
	s.testUDevBadValue5 = &interfaces.Slot{SlotInfo: gadgetSnapInfo.Slots["test-udev-bad-value-5"]}

	// Snap Consumers
	consumingSnapInfo := snaptest.MockInfo(c, `
name: client-snap
plugs:
  plug-for-port-1:
    interface: spi
    path: /dev/spidev.0.0
  plug-for-port-2:
    interface: spi
    path: /dev/spidev0.1
apps:
  app-accessing-1-port:
    command: foo
    plugs: [spi]
`, nil)
	s.testPlugPort1 = &interfaces.Plug{PlugInfo: consumingSnapInfo.Plugs["plug-for-port-1"]}
	s.testPlugPort2 = &interfaces.Plug{PlugInfo: consumingSnapInfo.Plugs["plug-for-port-2"]}
}

func (s *spiInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "spi")
}

func (s *spiInterfaceSuite) TestSanitizeCoreSnapSlot(c *C) {
	err := s.iface.SanitizeSlot(s.testSlot1)
	c.Assert(err, IsNil)
}

func (s *spiInterfaceSuite) TestSanitizeGadgetSnapSlot(c *C) {

	err := s.iface.SanitizeSlot(s.testUDev1)
	c.Assert(err, IsNil)

	err = s.iface.SanitizeSlot(s.testUDev2)
	c.Assert(err, IsNil)

}

func (s *spiInterfaceSuite) TestSanitizeBadGadgetSnapSlot(c *C) {

	err := s.iface.SanitizeSlot(s.testUDevBadValue1)
	c.Assert(err, ErrorMatches, "spi path attribute must be a valid device node")

	err = s.iface.SanitizeSlot(s.testUDevBadValue2)
	c.Assert(err, ErrorMatches, "spi path attribute must be a valid device node")

	err = s.iface.SanitizeSlot(s.testUDevBadValue3)
	c.Assert(err, ErrorMatches, "spi path attribute must be a valid device node")

	err = s.iface.SanitizeSlot(s.testUDevBadValue4)
	c.Assert(err, ErrorMatches, "spi path attribute must be a valid device node")

	err = s.iface.SanitizeSlot(s.testUDevBadValue5)
	c.Assert(err, ErrorMatches, "spi path attribute must be a valid device node")

}

func (s *spiInterfaceSuite) TestConnectedPlugUDevSnippets(c *C) {
	expectedSnippet1 := `KERNEL=="spidev0.0", TAG+="snap_client-snap_app-accessing-1-port"`
	spec := &udev.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.testPlugPort1, nil, s.testUDev1, nil), IsNil)
	c.Assert(spec.Snippets(), HasLen, 1)
	snippet := spec.Snippets()[0]
	c.Assert(snippet, Equals, expectedSnippet1)
}

func (s *spiInterfaceSuite) TestConnectedPlugAppArmorSnippets(c *C) {
	expectedSnippet1 := `/dev/spidev0.0 rw,
/sys/devices/platform/soc/**.spi/spi_master/spi0/spidev0.0/** rw,`
	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, s.testPlugPort1, nil, s.testUDev1, nil)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.client-snap.app-accessing-1-port"})
	snippet := apparmorSpec.SnippetForTag("snap.client-snap.app-accessing-1-port")
	c.Assert(snippet, DeepEquals, expectedSnippet1, Commentf("\nexpected:\n%s\nfound:\n%s", expectedSnippet1, snippet))

}

func (s *spiInterfaceSuite) TestAutoConnect(c *C) {
	c.Check(s.iface.AutoConnect(nil, nil), Equals, true)
}

func (s *spiInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
