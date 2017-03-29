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

type IioInterfaceSuite struct {
	testutil.BaseTest
	iface interfaces.Interface

	// OS Snap
	testSlot1 *interfaces.Slot

	// Gadget Snap
	testUdev1             *interfaces.Slot
	testUdev2             *interfaces.Slot
	testUdev3             *interfaces.Slot
	testUdevBadValue1     *interfaces.Slot
	testUdevBadValue2     *interfaces.Slot
	testUdevBadValue3     *interfaces.Slot
	testUdevBadValue4     *interfaces.Slot
	testUdevBadValue5     *interfaces.Slot
	testUdevBadValue6     *interfaces.Slot
	testUdevBadValue7     *interfaces.Slot
	testUdevBadValue8     *interfaces.Slot
	testUdevBadInterface1 *interfaces.Slot

	// Consuming Snap
	testPlugPort1 *interfaces.Plug
}

var _ = Suite(&IioInterfaceSuite{
	iface: &builtin.IioInterface{},
})

func (s *IioInterfaceSuite) SetUpTest(c *C) {
	// Mock for OS Snap
	osSnapInfo := snaptest.MockInfo(c, `
name: ubuntu-core
type: os
slots:
  test-port-1:
    interface: iio
    path: /dev/iio:device0
`, nil)
	s.testSlot1 = &interfaces.Slot{SlotInfo: osSnapInfo.Slots["test-port-1"]}

	// Mock for Gadget Snap
	gadgetSnapInfo := snaptest.MockInfo(c, `
name: some-device
type: gadget
slots:
  test-udev-1:
    interface: iio
    path: /dev/iio:device1
  test-udev-2:
    interface: iio
    path: /dev/iio:device2
  test-udev-3:
    interface: iio
    path: /dev/iio:device10000
  test-udev-bad-value-1:
    interface: iio
    path: /dev/iio
  test-udev-bad-value-2:
    interface: iio
    path: /dev/iio:devicea
  test-udev-bad-value-3:
    interface: iio
    path: /dev/iio:device2a
  test-udev-bad-value-4:
    interface: iio
    path: /dev/foo-0
  test-udev-bad-value-5:
    interface: iio
    path: /dev/iio:devicefoo
  test-udev-bad-value-6:
    interface: iio
    path: /dev/iio-device0
  test-udev-bad-value-7:
    interface: iio
    path: ""
  test-udev-bad-value-8:
    interface: iio
  test-udev-bad-interface-1:
    interface: other-interface
`, nil)
	s.testUdev1 = &interfaces.Slot{SlotInfo: gadgetSnapInfo.Slots["test-udev-1"]}
	s.testUdev2 = &interfaces.Slot{SlotInfo: gadgetSnapInfo.Slots["test-udev-2"]}
	s.testUdev3 = &interfaces.Slot{SlotInfo: gadgetSnapInfo.Slots["test-udev-3"]}
	s.testUdevBadValue1 = &interfaces.Slot{SlotInfo: gadgetSnapInfo.Slots["test-udev-bad-value-1"]}
	s.testUdevBadValue2 = &interfaces.Slot{SlotInfo: gadgetSnapInfo.Slots["test-udev-bad-value-2"]}
	s.testUdevBadValue3 = &interfaces.Slot{SlotInfo: gadgetSnapInfo.Slots["test-udev-bad-value-3"]}
	s.testUdevBadValue4 = &interfaces.Slot{SlotInfo: gadgetSnapInfo.Slots["test-udev-bad-value-4"]}
	s.testUdevBadValue5 = &interfaces.Slot{SlotInfo: gadgetSnapInfo.Slots["test-udev-bad-value-5"]}
	s.testUdevBadValue6 = &interfaces.Slot{SlotInfo: gadgetSnapInfo.Slots["test-udev-bad-value-6"]}
	s.testUdevBadValue7 = &interfaces.Slot{SlotInfo: gadgetSnapInfo.Slots["test-udev-bad-value-7"]}
	s.testUdevBadValue8 = &interfaces.Slot{SlotInfo: gadgetSnapInfo.Slots["test-udev-bad-value-8"]}
	s.testUdevBadInterface1 = &interfaces.Slot{SlotInfo: gadgetSnapInfo.Slots["test-udev-bad-interface-1"]}

	// Snap Consumers
	consumingSnapInfo := snaptest.MockInfo(c, `
name: client-snap
plugs:
  plug-for-port-1:
    interface: iio
apps:
  app-accessing-1-port:
    command: foo
    plugs: [iio]
`, nil)
	s.testPlugPort1 = &interfaces.Plug{PlugInfo: consumingSnapInfo.Plugs["plug-for-port-1"]}
}

func (s *IioInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "iio")
}

func (s *IioInterfaceSuite) TestSanitizeBadGadgetSnapSlot(c *C) {

	err := s.iface.SanitizeSlot(s.testUdevBadValue1)
	c.Assert(err, ErrorMatches, "iio path attribute must be a valid device node")

	err = s.iface.SanitizeSlot(s.testUdevBadValue2)
	c.Assert(err, ErrorMatches, "iio path attribute must be a valid device node")

	err = s.iface.SanitizeSlot(s.testUdevBadValue3)
	c.Assert(err, ErrorMatches, "iio path attribute must be a valid device node")

	err = s.iface.SanitizeSlot(s.testUdevBadValue4)
	c.Assert(err, ErrorMatches, "iio path attribute must be a valid device node")

	err = s.iface.SanitizeSlot(s.testUdevBadValue5)
	c.Assert(err, ErrorMatches, "iio path attribute must be a valid device node")

	err = s.iface.SanitizeSlot(s.testUdevBadValue6)
	c.Assert(err, ErrorMatches, "iio path attribute must be a valid device node")

	err = s.iface.SanitizeSlot(s.testUdevBadValue7)
	c.Assert(err, ErrorMatches, "iio slot must have a path attribute")

	err = s.iface.SanitizeSlot(s.testUdevBadValue8)
	c.Assert(err, ErrorMatches, "iio slot must have a path attribute")

	c.Assert(func() { s.iface.SanitizeSlot(s.testUdevBadInterface1) }, PanicMatches, `slot is not of interface "iio"`)
}

func (s *IioInterfaceSuite) TestConnectedPlugUdevSnippets(c *C) {
	expectedSnippet1 := `KERNEL=="iio:device1", TAG+="snap_client-snap_app-accessing-1-port"`

	spec := &udev.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.testPlugPort1, s.testUdev1), IsNil)
	c.Assert(spec.Snippets(), HasLen, 1)
	snippet := spec.Snippets()[0]
	c.Assert(snippet, Equals, expectedSnippet1)
}

func (s *IioInterfaceSuite) TestConnectedPlugAppArmorSnippets(c *C) {
	expectedSnippet1 := `
# Description: Give access to a specific IIO device on the system.

/dev/iio:device1 rw,
/sys/bus/iio/devices/iio:device1/ r,
/sys/bus/iio/devices/iio:device1/** rwk,
`
	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, s.testPlugPort1, s.testUdev1)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.client-snap.app-accessing-1-port"})
	snippet := apparmorSpec.SnippetForTag("snap.client-snap.app-accessing-1-port")
	c.Assert(snippet, DeepEquals, expectedSnippet1, Commentf("\nexpected:\n%s\nfound:\n%s", expectedSnippet1, snippet))
}

func (s *IioInterfaceSuite) TestAutoConnect(c *C) {
	c.Check(s.iface.AutoConnect(nil, nil), Equals, true)
}
