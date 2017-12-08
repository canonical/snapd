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

type SerialPortInterfaceSuite struct {
	testutil.BaseTest
	iface interfaces.Interface

	// OS Snap
	testSlot1        *interfaces.Slot
	testSlot2        *interfaces.Slot
	testSlot3        *interfaces.Slot
	testSlot4        *interfaces.Slot
	testSlot5        *interfaces.Slot
	testSlot6        *interfaces.Slot
	testSlot7        *interfaces.Slot
	missingPathSlot  *interfaces.Slot
	badPathSlot1     *interfaces.Slot
	badPathSlot2     *interfaces.Slot
	badPathSlot3     *interfaces.Slot
	badPathSlot4     *interfaces.Slot
	badPathSlot5     *interfaces.Slot
	badPathSlot6     *interfaces.Slot
	badPathSlot7     *interfaces.Slot
	badPathSlot8     *interfaces.Slot
	badPathSlot9     *interfaces.Slot
	badPathSlot10    *interfaces.Slot
	badInterfaceSlot *interfaces.Slot

	// Gadget Snap
	testUDev1         *interfaces.Slot
	testUDev2         *interfaces.Slot
	testUDev3         *interfaces.Slot
	testUDevBadValue1 *interfaces.Slot
	testUDevBadValue2 *interfaces.Slot
	testUDevBadValue3 *interfaces.Slot
	testUDevBadValue4 *interfaces.Slot
	testUDevBadValue5 *interfaces.Slot

	// Consuming Snap
	testPlugPort1 *interfaces.Plug
	testPlugPort2 *interfaces.Plug
	testPlugPort3 *interfaces.Plug
}

var _ = Suite(&SerialPortInterfaceSuite{
	iface: builtin.MustInterface("serial-port"),
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
        path: /dev/ttyUSB927
    test-port-3:
        interface: serial-port
        path: /dev/ttyS42
    test-port-4:
        interface: serial-port
        path: /dev/ttyO0
    test-port-5:
        interface: serial-port
        path: /dev/ttyACM0
    test-port-6:
        interface: serial-port
        path: /dev/ttyAMA0
    test-port-7:
        interface: serial-port
        path: /dev/ttyXRUSB0
    missing-path: serial-port
    bad-path-1:
        interface: serial-port
        path: path
    bad-path-2:
        interface: serial-port
        path: /dev/tty
    bad-path-3:
        interface: serial-port
        path: /dev/tty0
    bad-path-4:
        interface: serial-port
        path: /dev/tty63
    bad-path-5:
        interface: serial-port
        path: /dev/ttyUSB
    bad-path-6:
        interface: serial-port
        path: /dev/usb
    bad-path-7:
        interface: serial-port
        path: /dev/ttyprintk
    bad-path-8:
        interface: serial-port
        path: /dev/ttyO
    bad-path-9:
        interface: serial-port
        path: /dev/ttyS
    bad-path-10:
        interface: serial-port
        path: /dev/ttyillegal0
    bad-interface: other-interface
`, nil)
	s.testSlot1 = &interfaces.Slot{SlotInfo: osSnapInfo.Slots["test-port-1"]}
	s.testSlot2 = &interfaces.Slot{SlotInfo: osSnapInfo.Slots["test-port-2"]}
	s.testSlot3 = &interfaces.Slot{SlotInfo: osSnapInfo.Slots["test-port-3"]}
	s.testSlot4 = &interfaces.Slot{SlotInfo: osSnapInfo.Slots["test-port-4"]}
	s.testSlot5 = &interfaces.Slot{SlotInfo: osSnapInfo.Slots["test-port-5"]}
	s.testSlot6 = &interfaces.Slot{SlotInfo: osSnapInfo.Slots["test-port-6"]}
	s.testSlot7 = &interfaces.Slot{SlotInfo: osSnapInfo.Slots["test-port-7"]}
	s.missingPathSlot = &interfaces.Slot{SlotInfo: osSnapInfo.Slots["missing-path"]}
	s.badPathSlot1 = &interfaces.Slot{SlotInfo: osSnapInfo.Slots["bad-path-1"]}
	s.badPathSlot2 = &interfaces.Slot{SlotInfo: osSnapInfo.Slots["bad-path-2"]}
	s.badPathSlot3 = &interfaces.Slot{SlotInfo: osSnapInfo.Slots["bad-path-3"]}
	s.badPathSlot4 = &interfaces.Slot{SlotInfo: osSnapInfo.Slots["bad-path-4"]}
	s.badPathSlot5 = &interfaces.Slot{SlotInfo: osSnapInfo.Slots["bad-path-5"]}
	s.badPathSlot6 = &interfaces.Slot{SlotInfo: osSnapInfo.Slots["bad-path-6"]}
	s.badPathSlot7 = &interfaces.Slot{SlotInfo: osSnapInfo.Slots["bad-path-7"]}
	s.badPathSlot8 = &interfaces.Slot{SlotInfo: osSnapInfo.Slots["bad-path-8"]}
	s.badPathSlot9 = &interfaces.Slot{SlotInfo: osSnapInfo.Slots["bad-path-9"]}
	s.badPathSlot10 = &interfaces.Slot{SlotInfo: osSnapInfo.Slots["bad-path-10"]}
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
  test-udev-3:
      interface: serial-port
      usb-vendor: 0xabcd
      usb-product: 0x1234
      usb-interface-number: 0
      path: /dev/serial-port-myserial
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
  test-udev-bad-value-4:
      interface: serial-port
      usb-vendor: 0x1234
      usb-product: 0x4321
      usb-interface-number: -1
      path: /dev/serial-port-mybadinterface
  test-udev-bad-value-5:
      interface: serial-port
      usb-vendor: 0x1234
      usb-product: 0x4321
      usb-interface-number: 32
      path: /dev/serial-port-overinterfacenumber
`, nil)
	s.testUDev1 = &interfaces.Slot{SlotInfo: gadgetSnapInfo.Slots["test-udev-1"]}
	s.testUDev2 = &interfaces.Slot{SlotInfo: gadgetSnapInfo.Slots["test-udev-2"]}
	s.testUDev3 = &interfaces.Slot{SlotInfo: gadgetSnapInfo.Slots["test-udev-3"]}
	s.testUDevBadValue1 = &interfaces.Slot{SlotInfo: gadgetSnapInfo.Slots["test-udev-bad-value-1"]}
	s.testUDevBadValue2 = &interfaces.Slot{SlotInfo: gadgetSnapInfo.Slots["test-udev-bad-value-2"]}
	s.testUDevBadValue3 = &interfaces.Slot{SlotInfo: gadgetSnapInfo.Slots["test-udev-bad-value-3"]}
	s.testUDevBadValue4 = &interfaces.Slot{SlotInfo: gadgetSnapInfo.Slots["test-udev-bad-value-4"]}
	s.testUDevBadValue5 = &interfaces.Slot{SlotInfo: gadgetSnapInfo.Slots["test-udev-bad-value-5"]}

	consumingSnapInfo := snaptest.MockInfo(c, `
name: client-snap
plugs:
    plug-for-port-1:
        interface: serial-port
    plug-for-port-2:
        interface: serial-port
    plug-for-port-3:
        interface: serial-port

apps:
    app-accessing-1-port:
        command: foo
        plugs: [serial-port]
    app-accessing-2-ports:
        command: bar
        plugs: [plug-for-port-1, plug-for-port-2]
    app-accessing-3rd-port:
        command: foo
        plugs: [plug-for-port-3]
`, nil)
	s.testPlugPort1 = &interfaces.Plug{PlugInfo: consumingSnapInfo.Plugs["plug-for-port-1"]}
	s.testPlugPort2 = &interfaces.Plug{PlugInfo: consumingSnapInfo.Plugs["plug-for-port-2"]}
	s.testPlugPort3 = &interfaces.Plug{PlugInfo: consumingSnapInfo.Plugs["plug-for-port-3"]}
}

func (s *SerialPortInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "serial-port")
}

func (s *SerialPortInterfaceSuite) TestSanitizeCoreSnapSlots(c *C) {
	for _, slot := range []*interfaces.Slot{s.testSlot1, s.testSlot2, s.testSlot3, s.testSlot4, s.testSlot5, s.testSlot6, s.testSlot7} {
		c.Assert(slot.Sanitize(s.iface), IsNil)
	}
}

func (s *SerialPortInterfaceSuite) TestSanitizeBadCoreSnapSlots(c *C) {
	// Slots without the "path" attribute are rejected.
	c.Assert(s.missingPathSlot.Sanitize(s.iface), ErrorMatches, `serial-port slot must have a path attribute`)

	// Slots with incorrect value of the "path" attribute are rejected.
	for _, slot := range []*interfaces.Slot{s.badPathSlot1, s.badPathSlot2, s.badPathSlot3, s.badPathSlot4, s.badPathSlot5, s.badPathSlot6, s.badPathSlot7, s.badPathSlot8, s.badPathSlot9, s.badPathSlot10} {
		c.Assert(slot.Sanitize(s.iface), ErrorMatches, "serial-port path attribute must be a valid device node")
	}
}

func (s *SerialPortInterfaceSuite) TestSanitizeGadgetSnapSlots(c *C) {
	c.Assert(s.testUDev1.Sanitize(s.iface), IsNil)
	c.Assert(s.testUDev2.Sanitize(s.iface), IsNil)
	c.Assert(s.testUDev3.Sanitize(s.iface), IsNil)
}

func (s *SerialPortInterfaceSuite) TestSanitizeBadGadgetSnapSlots(c *C) {
	c.Assert(s.testUDevBadValue1.Sanitize(s.iface), ErrorMatches, "serial-port usb-vendor attribute not valid: -1")
	c.Assert(s.testUDevBadValue2.Sanitize(s.iface), ErrorMatches, "serial-port usb-product attribute not valid: 65536")
	c.Assert(s.testUDevBadValue3.Sanitize(s.iface), ErrorMatches, "serial-port path attribute specifies invalid symlink location")
	c.Assert(s.testUDevBadValue4.Sanitize(s.iface), ErrorMatches, "serial-port usb-interface-number attribute cannot be negative or larger than 31")
	c.Assert(s.testUDevBadValue5.Sanitize(s.iface), ErrorMatches, "serial-port usb-interface-number attribute cannot be negative or larger than 31")
}

func (s *SerialPortInterfaceSuite) TestPermanentSlotUDevSnippets(c *C) {
	spec := &udev.Specification{}
	for _, slot := range []*interfaces.Slot{s.testSlot1, s.testSlot2, s.testSlot3, s.testSlot4} {
		err := spec.AddPermanentSlot(s.iface, slot.SlotInfo)
		c.Assert(err, IsNil)
		c.Assert(spec.Snippets(), HasLen, 0)
	}

	expectedSnippet1 := `# serial-port
IMPORT{builtin}="usb_id"
SUBSYSTEM=="tty", SUBSYSTEMS=="usb", ATTRS{idVendor}=="0001", ATTRS{idProduct}=="0001", SYMLINK+="serial-port-zigbee"`
	err := spec.AddPermanentSlot(s.iface, s.testUDev1.SlotInfo)
	c.Assert(err, IsNil)
	c.Assert(spec.Snippets(), HasLen, 1)
	snippet := spec.Snippets()[0]
	c.Assert(snippet, Equals, expectedSnippet1)

	spec = &udev.Specification{}
	expectedSnippet2 := `# serial-port
IMPORT{builtin}="usb_id"
SUBSYSTEM=="tty", SUBSYSTEMS=="usb", ATTRS{idVendor}=="ffff", ATTRS{idProduct}=="ffff", SYMLINK+="serial-port-mydevice"`
	err = spec.AddPermanentSlot(s.iface, s.testUDev2.SlotInfo)
	c.Assert(err, IsNil)
	c.Assert(spec.Snippets(), HasLen, 1)
	snippet = spec.Snippets()[0]
	c.Assert(snippet, Equals, expectedSnippet2)

	spec = &udev.Specification{}
	// The ENV{ID_USB_INTERFACE_NUM} is set to two hex digits
	// For instance, the expectedSnippet3 is set to 00
	expectedSnippet3 := `# serial-port
IMPORT{builtin}="usb_id"
SUBSYSTEM=="tty", SUBSYSTEMS=="usb", ATTRS{idVendor}=="abcd", ATTRS{idProduct}=="1234", ENV{ID_USB_INTERFACE_NUM}=="00", SYMLINK+="serial-port-myserial"`
	err = spec.AddPermanentSlot(s.iface, s.testUDev3.SlotInfo)
	c.Assert(err, IsNil)
	c.Assert(spec.Snippets(), HasLen, 1)
	snippet = spec.Snippets()[0]
	c.Assert(snippet, Equals, expectedSnippet3)
}

func (s *SerialPortInterfaceSuite) TestConnectedPlugUDevSnippets(c *C) {
	// add the plug for the slot with just path
	spec := &udev.Specification{}
	err := spec.AddConnectedPlug(s.iface, s.testPlugPort1, nil, s.testSlot1, nil)
	c.Assert(err, IsNil)
	c.Assert(spec.Snippets(), HasLen, 2)
	snippet := spec.Snippets()[0]
	expectedSnippet1 := `# serial-port
SUBSYSTEM=="tty", KERNEL=="ttyS0", TAG+="snap_client-snap_app-accessing-2-ports"`
	c.Assert(snippet, Equals, expectedSnippet1)
	extraSnippet := spec.Snippets()[1]
	expectedExtraSnippet1 := `TAG=="snap_client-snap_app-accessing-2-ports", RUN+="/lib/udev/snappy-app-dev $env{ACTION} snap_client-snap_app-accessing-2-ports $devpath $major:$minor"`
	c.Assert(extraSnippet, Equals, expectedExtraSnippet1)

	// add plug for the first slot with product and vendor ids
	spec = &udev.Specification{}
	err = spec.AddConnectedPlug(s.iface, s.testPlugPort1, nil, s.testUDev1, nil)
	c.Assert(err, IsNil)
	c.Assert(spec.Snippets(), HasLen, 2)
	snippet = spec.Snippets()[0]
	expectedSnippet2 := `# serial-port
IMPORT{builtin}="usb_id"
SUBSYSTEM=="tty", SUBSYSTEMS=="usb", ATTRS{idVendor}=="0001", ATTRS{idProduct}=="0001", TAG+="snap_client-snap_app-accessing-2-ports"`
	c.Assert(snippet, Equals, expectedSnippet2)
	extraSnippet = spec.Snippets()[1]
	expectedExtraSnippet2 := `TAG=="snap_client-snap_app-accessing-2-ports", RUN+="/lib/udev/snappy-app-dev $env{ACTION} snap_client-snap_app-accessing-2-ports $devpath $major:$minor"`
	c.Assert(extraSnippet, Equals, expectedExtraSnippet2)

	// add plug for the first slot with product and vendor ids
	spec = &udev.Specification{}
	err = spec.AddConnectedPlug(s.iface, s.testPlugPort2, nil, s.testUDev2, nil)
	c.Assert(err, IsNil)
	c.Assert(spec.Snippets(), HasLen, 2)
	snippet = spec.Snippets()[0]
	expectedSnippet3 := `# serial-port
IMPORT{builtin}="usb_id"
SUBSYSTEM=="tty", SUBSYSTEMS=="usb", ATTRS{idVendor}=="ffff", ATTRS{idProduct}=="ffff", TAG+="snap_client-snap_app-accessing-2-ports"`
	c.Assert(snippet, Equals, expectedSnippet3)
	extraSnippet = spec.Snippets()[1]
	expectedExtraSnippet3 := `TAG=="snap_client-snap_app-accessing-2-ports", RUN+="/lib/udev/snappy-app-dev $env{ACTION} snap_client-snap_app-accessing-2-ports $devpath $major:$minor"`
	c.Assert(extraSnippet, Equals, expectedExtraSnippet3)

	// add plug for the first slot with product and vendor ids and usb interface number
	spec = &udev.Specification{}
	err = spec.AddConnectedPlug(s.iface, s.testPlugPort2, nil, s.testUDev3, nil)
	c.Assert(err, IsNil)
	c.Assert(spec.Snippets(), HasLen, 2)
	snippet = spec.Snippets()[0]
	expectedSnippet4 := `# serial-port
IMPORT{builtin}="usb_id"
SUBSYSTEM=="tty", SUBSYSTEMS=="usb", ATTRS{idVendor}=="abcd", ATTRS{idProduct}=="1234", ENV{ID_USB_INTERFACE_NUM}=="00", TAG+="snap_client-snap_app-accessing-2-ports"`
	c.Assert(snippet, Equals, expectedSnippet4)
	extraSnippet = spec.Snippets()[1]
	expectedExtraSnippet4 := `TAG=="snap_client-snap_app-accessing-2-ports", RUN+="/lib/udev/snappy-app-dev $env{ACTION} snap_client-snap_app-accessing-2-ports $devpath $major:$minor"`
	c.Assert(extraSnippet, Equals, expectedExtraSnippet4)
}

func (s *SerialPortInterfaceSuite) TestConnectedPlugAppArmorSnippets(c *C) {
	checkConnectedPlugSnippet := func(plug *interfaces.Plug, slot *interfaces.Slot, expectedSnippet string) {
		apparmorSpec := &apparmor.Specification{}
		err := apparmorSpec.AddConnectedPlug(s.iface, plug, nil, slot, nil)
		c.Assert(err, IsNil)

		c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.client-snap.app-accessing-2-ports"})
		snippet := apparmorSpec.SnippetForTag("snap.client-snap.app-accessing-2-ports")
		c.Assert(snippet, DeepEquals, expectedSnippet)
	}

	expectedSnippet1 := `/dev/ttyS0 rw,`
	checkConnectedPlugSnippet(s.testPlugPort1, s.testSlot1, expectedSnippet1)
	expectedSnippet2 := `/dev/ttyUSB927 rw,`
	checkConnectedPlugSnippet(s.testPlugPort1, s.testSlot2, expectedSnippet2)

	expectedSnippet3 := `/dev/ttyS42 rw,`
	checkConnectedPlugSnippet(s.testPlugPort1, s.testSlot3, expectedSnippet3)

	expectedSnippet4 := `/dev/ttyO0 rw,`
	checkConnectedPlugSnippet(s.testPlugPort1, s.testSlot4, expectedSnippet4)

	expectedSnippet5 := `/dev/ttyACM0 rw,`
	checkConnectedPlugSnippet(s.testPlugPort1, s.testSlot5, expectedSnippet5)

	expectedSnippet6 := `/dev/ttyAMA0 rw,`
	checkConnectedPlugSnippet(s.testPlugPort1, s.testSlot6, expectedSnippet6)

	expectedSnippet7 := `/dev/ttyXRUSB0 rw,`
	checkConnectedPlugSnippet(s.testPlugPort1, s.testSlot7, expectedSnippet7)

	expectedSnippet8 := `/dev/tty[A-Z]*[0-9] rw,`
	checkConnectedPlugSnippet(s.testPlugPort1, s.testUDev1, expectedSnippet8)

	expectedSnippet9 := `/dev/tty[A-Z]*[0-9] rw,`
	checkConnectedPlugSnippet(s.testPlugPort2, s.testUDev2, expectedSnippet9)

	expectedSnippet10 := `/dev/tty[A-Z]*[0-9] rw,`
	checkConnectedPlugSnippet(s.testPlugPort2, s.testUDev3, expectedSnippet10)
}

func (s *SerialPortInterfaceSuite) TestConnectedPlugUDevSnippetsForPath(c *C) {
	checkConnectedPlugSnippet := func(plug *interfaces.Plug, slot *interfaces.Slot, expectedSnippet string, expectedExtraSnippet string) {
		udevSpec := &udev.Specification{}
		err := udevSpec.AddConnectedPlug(s.iface, plug, nil, slot, nil)
		c.Assert(err, IsNil)

		c.Assert(udevSpec.Snippets(), HasLen, 2)
		snippet := udevSpec.Snippets()[0]
		c.Assert(snippet, Equals, expectedSnippet)
		extraSnippet := udevSpec.Snippets()[1]
		c.Assert(extraSnippet, Equals, expectedExtraSnippet)
	}

	// these have only path
	expectedSnippet1 := `# serial-port
SUBSYSTEM=="tty", KERNEL=="ttyS0", TAG+="snap_client-snap_app-accessing-3rd-port"`
	expectedExtraSnippet1 := `TAG=="snap_client-snap_app-accessing-3rd-port", RUN+="/lib/udev/snappy-app-dev $env{ACTION} snap_client-snap_app-accessing-3rd-port $devpath $major:$minor"`
	checkConnectedPlugSnippet(s.testPlugPort3, s.testSlot1, expectedSnippet1, expectedExtraSnippet1)

	expectedSnippet2 := `# serial-port
SUBSYSTEM=="tty", KERNEL=="ttyUSB927", TAG+="snap_client-snap_app-accessing-3rd-port"`
	expectedExtraSnippet2 := `TAG=="snap_client-snap_app-accessing-3rd-port", RUN+="/lib/udev/snappy-app-dev $env{ACTION} snap_client-snap_app-accessing-3rd-port $devpath $major:$minor"`
	checkConnectedPlugSnippet(s.testPlugPort3, s.testSlot2, expectedSnippet2, expectedExtraSnippet2)

	expectedSnippet3 := `# serial-port
SUBSYSTEM=="tty", KERNEL=="ttyS42", TAG+="snap_client-snap_app-accessing-3rd-port"`
	expectedExtraSnippet3 := `TAG=="snap_client-snap_app-accessing-3rd-port", RUN+="/lib/udev/snappy-app-dev $env{ACTION} snap_client-snap_app-accessing-3rd-port $devpath $major:$minor"`
	checkConnectedPlugSnippet(s.testPlugPort3, s.testSlot3, expectedSnippet3, expectedExtraSnippet3)

	expectedSnippet4 := `# serial-port
SUBSYSTEM=="tty", KERNEL=="ttyO0", TAG+="snap_client-snap_app-accessing-3rd-port"`
	expectedExtraSnippet4 := `TAG=="snap_client-snap_app-accessing-3rd-port", RUN+="/lib/udev/snappy-app-dev $env{ACTION} snap_client-snap_app-accessing-3rd-port $devpath $major:$minor"`
	checkConnectedPlugSnippet(s.testPlugPort3, s.testSlot4, expectedSnippet4, expectedExtraSnippet4)

	expectedSnippet5 := `# serial-port
SUBSYSTEM=="tty", KERNEL=="ttyACM0", TAG+="snap_client-snap_app-accessing-3rd-port"`
	expectedExtraSnippet5 := `TAG=="snap_client-snap_app-accessing-3rd-port", RUN+="/lib/udev/snappy-app-dev $env{ACTION} snap_client-snap_app-accessing-3rd-port $devpath $major:$minor"`
	checkConnectedPlugSnippet(s.testPlugPort3, s.testSlot5, expectedSnippet5, expectedExtraSnippet5)

	expectedSnippet6 := `# serial-port
SUBSYSTEM=="tty", KERNEL=="ttyAMA0", TAG+="snap_client-snap_app-accessing-3rd-port"`
	expectedExtraSnippet6 := `TAG=="snap_client-snap_app-accessing-3rd-port", RUN+="/lib/udev/snappy-app-dev $env{ACTION} snap_client-snap_app-accessing-3rd-port $devpath $major:$minor"`
	checkConnectedPlugSnippet(s.testPlugPort3, s.testSlot6, expectedSnippet6, expectedExtraSnippet6)

	expectedSnippet7 := `# serial-port
SUBSYSTEM=="tty", KERNEL=="ttyXRUSB0", TAG+="snap_client-snap_app-accessing-3rd-port"`
	expectedExtraSnippet7 := `TAG=="snap_client-snap_app-accessing-3rd-port", RUN+="/lib/udev/snappy-app-dev $env{ACTION} snap_client-snap_app-accessing-3rd-port $devpath $major:$minor"`
	checkConnectedPlugSnippet(s.testPlugPort3, s.testSlot7, expectedSnippet7, expectedExtraSnippet7)

	// these have product and vendor ids
	expectedSnippet8 := `# serial-port
IMPORT{builtin}="usb_id"
SUBSYSTEM=="tty", SUBSYSTEMS=="usb", ATTRS{idVendor}=="0001", ATTRS{idProduct}=="0001", TAG+="snap_client-snap_app-accessing-3rd-port"`
	expectedExtraSnippet8 := `TAG=="snap_client-snap_app-accessing-3rd-port", RUN+="/lib/udev/snappy-app-dev $env{ACTION} snap_client-snap_app-accessing-3rd-port $devpath $major:$minor"`
	checkConnectedPlugSnippet(s.testPlugPort3, s.testUDev1, expectedSnippet8, expectedExtraSnippet8)

	expectedSnippet9 := `# serial-port
IMPORT{builtin}="usb_id"
SUBSYSTEM=="tty", SUBSYSTEMS=="usb", ATTRS{idVendor}=="ffff", ATTRS{idProduct}=="ffff", TAG+="snap_client-snap_app-accessing-3rd-port"`
	expectedExtraSnippet9 := `TAG=="snap_client-snap_app-accessing-3rd-port", RUN+="/lib/udev/snappy-app-dev $env{ACTION} snap_client-snap_app-accessing-3rd-port $devpath $major:$minor"`
	checkConnectedPlugSnippet(s.testPlugPort3, s.testUDev2, expectedSnippet9, expectedExtraSnippet9)
}

func (s *SerialPortInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
