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
	"github.com/snapcore/snapd/testutil"
)

type SerialPortInterfaceSuite struct {
	testutil.BaseTest
	iface interfaces.Interface

	// OS Snap
	testSlot1        *interfaces.Slot
	testSlot2        *interfaces.Slot
	testSlot3        *interfaces.Slot
	testSlot4        *interfaces.Slot
	missingPathSlot  *interfaces.Slot
	badPathSlot1     *interfaces.Slot
	badPathSlot2     *interfaces.Slot
	badPathSlot3     *interfaces.Slot
	badInterfaceSlot *interfaces.Slot

	// Gadget Snap
	testUdev1         *interfaces.Slot
	testUdev2         *interfaces.Slot
	testUdevBadValue1 *interfaces.Slot
	testUdevBadValue2 *interfaces.Slot
	testUdevBadValue3 *interfaces.Slot

	// Consuming Snap
	testPlugPort1 *interfaces.Plug
	testPlugPort2 *interfaces.Plug
}

var _ = Suite(&SerialPortInterfaceSuite{
	iface: &builtin.SerialPortInterface{},
})

func (s *SerialPortInterfaceSuite) SetUpTest(c *C) {
	osSnapInfo := snaptest.MockInfo(c, `
name: ubuntu-core
type: os
slots:
    test-port-1:
        interface: serial-port
        path: /dev/ttyS0
    test-port-2:
        interface: serial-port
        path: /dev/ttyAMA2
    test-port-3:
        interface: serial-port
        path: /dev/ttyUSB927
    test-port-4:
        interface: serial-port
        path: /dev/ttyS42
    missing-path: serial-port
    bad-path-1:
        interface: serial-port
        path: path
    bad-path-2:
        interface: serial-port
        path: /dev/tty0
    bad-path-3:
        interface: serial-port
        path: /dev/ttyUSB9271
    bad-interface: other-interface
`, nil)
	s.testSlot1 = &interfaces.Slot{SlotInfo: osSnapInfo.Slots["test-port-1"]}
	s.testSlot2 = &interfaces.Slot{SlotInfo: osSnapInfo.Slots["test-port-2"]}
	s.testSlot3 = &interfaces.Slot{SlotInfo: osSnapInfo.Slots["test-port-3"]}
	s.testSlot4 = &interfaces.Slot{SlotInfo: osSnapInfo.Slots["test-port-4"]}
	s.missingPathSlot = &interfaces.Slot{SlotInfo: osSnapInfo.Slots["missing-path"]}
	s.badPathSlot1 = &interfaces.Slot{SlotInfo: osSnapInfo.Slots["bad-path-1"]}
	s.badPathSlot2 = &interfaces.Slot{SlotInfo: osSnapInfo.Slots["bad-path-2"]}
	s.badPathSlot3 = &interfaces.Slot{SlotInfo: osSnapInfo.Slots["bad-path-3"]}
	s.badInterfaceSlot = &interfaces.Slot{SlotInfo: osSnapInfo.Slots["bad-interface"]}

	gadgetSnapInfo := snaptest.MockInfo(c, `
name: some-device
type: gadget
slots:
  test-udev-1:
      interface: serial-port
      usb-vendor: 0x0001
      usb-product: 0x0001
      path: /dev/serial-port-zigbee
  test-udev-2:
      interface: serial-port
      usb-vendor: 0xffff
      usb-product: 0xffff
      path: /dev/serial-port-mydevice
  test-udev-bad-value-1:
      interface: serial-port
      usb-vendor: -1
      usb-product: 0xffff
      path: /dev/serial-port-mydevice
  test-udev-bad-value-2:
      interface: serial-port
      usb-vendor: 0x1234
      usb-product: 0x10000
      path: /dev/serial-port-mydevice
  test-udev-bad-value-3:
      interface: serial-port
      usb-vendor: 0x789a
      usb-product: 0x4321
      path: /dev/my-device
`, nil)
	s.testUdev1 = &interfaces.Slot{SlotInfo: gadgetSnapInfo.Slots["test-udev-1"]}
	s.testUdev2 = &interfaces.Slot{SlotInfo: gadgetSnapInfo.Slots["test-udev-2"]}
	s.testUdevBadValue1 = &interfaces.Slot{SlotInfo: gadgetSnapInfo.Slots["test-udev-bad-value-1"]}
	s.testUdevBadValue2 = &interfaces.Slot{SlotInfo: gadgetSnapInfo.Slots["test-udev-bad-value-2"]}
	s.testUdevBadValue3 = &interfaces.Slot{SlotInfo: gadgetSnapInfo.Slots["test-udev-bad-value-3"]}

	consumingSnapInfo := snaptest.MockInfo(c, `
name: client-snap
plugs:
    plug-for-port-1:
        interface: serial-port
    plug-for-port-2:
        interface: serial-port

apps:
    app-accessing-1-port:
        command: foo
        plugs: [serial-port]
    app-accessing-2-ports:
        command: bar
        plugs: [plug-for-port-1, plug-for-port-2]
`, nil)
	s.testPlugPort1 = &interfaces.Plug{PlugInfo: consumingSnapInfo.Plugs["plug-for-port-1"]}
	s.testPlugPort2 = &interfaces.Plug{PlugInfo: consumingSnapInfo.Plugs["plug-for-port-2"]}
}

func (s *SerialPortInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "serial-port")
}

func (s *SerialPortInterfaceSuite) TestSanitizeCoreSnapSlots(c *C) {
	for _, slot := range []*interfaces.Slot{s.testSlot1, s.testSlot2, s.testSlot3, s.testSlot4} {
		err := s.iface.SanitizeSlot(slot)
		c.Assert(err, IsNil)
	}
}

func (s *SerialPortInterfaceSuite) TestSanitizeBadCoreSnapSlots(c *C) {
	// Slots without the "path" attribute are rejected.
	err := s.iface.SanitizeSlot(s.missingPathSlot)
	c.Assert(err, ErrorMatches, `serial-port slot must have a path attribute`)

	// Slots with incorrect value of the "path" attribute are rejected.
	for _, slot := range []*interfaces.Slot{s.badPathSlot1, s.badPathSlot2, s.badPathSlot3} {
		err := s.iface.SanitizeSlot(slot)
		c.Assert(err, ErrorMatches, "serial-port path attribute must be a valid device node")
	}

	// It is impossible to use "bool-file" interface to sanitize slots with other interfaces.
	c.Assert(func() { s.iface.SanitizeSlot(s.badInterfaceSlot) }, PanicMatches, `slot is not of interface "serial-port"`)
}

func (s *SerialPortInterfaceSuite) TestSanitizeGadgetSnapSlots(c *C) {
	err := s.iface.SanitizeSlot(s.testUdev1)
	c.Assert(err, IsNil)

	err = s.iface.SanitizeSlot(s.testUdev2)
	c.Assert(err, IsNil)
}

func (s *SerialPortInterfaceSuite) TestSanitizeBadGadgetSnapSlots(c *C) {
	err := s.iface.SanitizeSlot(s.testUdevBadValue1)
	c.Assert(err, ErrorMatches, "serial-port usb-vendor attribute not valid: -1")

	err = s.iface.SanitizeSlot(s.testUdevBadValue2)
	c.Assert(err, ErrorMatches, "serial-port usb-product attribute not valid: 65536")

	err = s.iface.SanitizeSlot(s.testUdevBadValue3)
	c.Assert(err, ErrorMatches, "serial-port path attribute specifies invalid symlink location")
}

func (s *SerialPortInterfaceSuite) TestPermanentSlotUdevSnippets(c *C) {
	for _, slot := range []*interfaces.Slot{s.testSlot1, s.testSlot2, s.testSlot3, s.testSlot4} {
		snippet, err := s.iface.PermanentSlotSnippet(slot, interfaces.SecurityUDev)
		c.Assert(err, IsNil)
		c.Assert(snippet, IsNil)
	}

	expectedSnippet1 := []byte(`IMPORT{builtin}="usb_id"
SUBSYSTEM=="tty", SUBSYSTEMS=="usb", ATTRS{idVendor}=="0001", ATTRS{idProduct}=="0001", SYMLINK+="serial-port-zigbee"
`)
	snippet, err := s.iface.PermanentSlotSnippet(s.testUdev1, interfaces.SecurityUDev)
	c.Assert(err, IsNil)
	c.Assert(snippet, DeepEquals, expectedSnippet1, Commentf("\nexpected:\n%s\nfound:\n%s", expectedSnippet1, snippet))

	expectedSnippet2 := []byte(`IMPORT{builtin}="usb_id"
SUBSYSTEM=="tty", SUBSYSTEMS=="usb", ATTRS{idVendor}=="ffff", ATTRS{idProduct}=="ffff", SYMLINK+="serial-port-mydevice"
`)
	snippet, err = s.iface.PermanentSlotSnippet(s.testUdev2, interfaces.SecurityUDev)
	c.Assert(err, IsNil)
	c.Assert(snippet, DeepEquals, expectedSnippet2, Commentf("\nexpected:\n%s\nfound:\n%s", expectedSnippet2, snippet))
}

func (s *SerialPortInterfaceSuite) TestConnectedPlugUdevSnippets(c *C) {
	snippet, err := s.iface.ConnectedPlugSnippet(s.testPlugPort1, s.testSlot1, interfaces.SecurityUDev)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)

	expectedSnippet1 := []byte(`IMPORT{builtin}="usb_id"
SUBSYSTEM=="tty", SUBSYSTEMS=="usb", ATTRS{idVendor}=="0001", ATTRS{idProduct}=="0001", TAG+="snap_client-snap_app-accessing-2-ports"
`)
	snippet, err = s.iface.ConnectedPlugSnippet(s.testPlugPort1, s.testUdev1, interfaces.SecurityUDev)
	c.Assert(err, IsNil)
	c.Assert(snippet, DeepEquals, expectedSnippet1, Commentf("\nexpected:\n%s\nfound:\n%s", expectedSnippet1, snippet))

	expectedSnippet2 := []byte(`IMPORT{builtin}="usb_id"
SUBSYSTEM=="tty", SUBSYSTEMS=="usb", ATTRS{idVendor}=="ffff", ATTRS{idProduct}=="ffff", TAG+="snap_client-snap_app-accessing-2-ports"
`)
	snippet, err = s.iface.ConnectedPlugSnippet(s.testPlugPort2, s.testUdev2, interfaces.SecurityUDev)
	c.Assert(err, IsNil)
	c.Assert(snippet, DeepEquals, expectedSnippet2, Commentf("\nexpected:\n%s\nfound:\n%s", expectedSnippet2, snippet))
}

func (s *SerialPortInterfaceSuite) TestConnectedPlugAppArmorSnippets(c *C) {
	expectedSnippet1 := []byte(`/dev/ttyS0 rw,
`)
	snippet, err := s.iface.ConnectedPlugSnippet(s.testPlugPort1, s.testSlot1, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, DeepEquals, expectedSnippet1, Commentf("\nexpected:\n%s\nfound:\n%s", expectedSnippet1, snippet))

	expectedSnippet2 := []byte(`/dev/tty[A-Z]{,[A-Z],[A-Z][A-Z]}[0-9]{,[0-9],[0-9][0-9]} rw,
`)
	snippet, err = s.iface.ConnectedPlugSnippet(s.testPlugPort1, s.testUdev1, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, DeepEquals, expectedSnippet2, Commentf("\nexpected:\n%s\nfound:\n%s", expectedSnippet2, snippet))

	expectedSnippet3 := []byte(`/dev/tty[A-Z]{,[A-Z],[A-Z][A-Z]}[0-9]{,[0-9],[0-9][0-9]} rw,
`)
	snippet, err = s.iface.ConnectedPlugSnippet(s.testPlugPort2, s.testUdev2, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, DeepEquals, expectedSnippet3, Commentf("\nexpected:\n%s\nfound:\n%s", expectedSnippet3, snippet))
}
