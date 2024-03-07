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
	"fmt"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/hotplug"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type SerialPortInterfaceSuite struct {
	testutil.BaseTest
	iface interfaces.Interface

	// OS Snap
	osSnapInfo           *snap.Info
	testSlot1            *interfaces.ConnectedSlot
	testSlot1Info        *snap.SlotInfo
	testSlot2            *interfaces.ConnectedSlot
	testSlot2Info        *snap.SlotInfo
	testSlot3            *interfaces.ConnectedSlot
	testSlot3Info        *snap.SlotInfo
	testSlot4            *interfaces.ConnectedSlot
	testSlot4Info        *snap.SlotInfo
	testSlot5            *interfaces.ConnectedSlot
	testSlot5Info        *snap.SlotInfo
	testSlot6            *interfaces.ConnectedSlot
	testSlot6Info        *snap.SlotInfo
	testSlot7            *interfaces.ConnectedSlot
	testSlot7Info        *snap.SlotInfo
	testSlot8            *interfaces.ConnectedSlot
	testSlot8Info        *snap.SlotInfo
	testSlot9            *interfaces.ConnectedSlot
	testSlot9Info        *snap.SlotInfo
	testSlot10           *interfaces.ConnectedSlot
	testSlot10Info       *snap.SlotInfo
	testSlot11           *interfaces.ConnectedSlot
	testSlot11Info       *snap.SlotInfo
	testSlot12           *interfaces.ConnectedSlot
	testSlot13Info       *snap.SlotInfo
	testSlot13           *interfaces.ConnectedSlot
	testSlot12Info       *snap.SlotInfo
	testSlotCleaned      *interfaces.ConnectedSlot
	testSlotCleanedInfo  *snap.SlotInfo
	missingPathSlot      *interfaces.ConnectedSlot
	missingPathSlotInfo  *snap.SlotInfo
	badPathSlot1         *interfaces.ConnectedSlot
	badPathSlot1Info     *snap.SlotInfo
	badPathSlot2         *interfaces.ConnectedSlot
	badPathSlot2Info     *snap.SlotInfo
	badPathSlot3         *interfaces.ConnectedSlot
	badPathSlot3Info     *snap.SlotInfo
	badPathSlot4         *interfaces.ConnectedSlot
	badPathSlot4Info     *snap.SlotInfo
	badPathSlot5         *interfaces.ConnectedSlot
	badPathSlot5Info     *snap.SlotInfo
	badPathSlot6         *interfaces.ConnectedSlot
	badPathSlot6Info     *snap.SlotInfo
	badPathSlot7         *interfaces.ConnectedSlot
	badPathSlot7Info     *snap.SlotInfo
	badPathSlot8         *interfaces.ConnectedSlot
	badPathSlot8Info     *snap.SlotInfo
	badPathSlot9         *interfaces.ConnectedSlot
	badPathSlot9Info     *snap.SlotInfo
	badPathSlot10        *interfaces.ConnectedSlot
	badPathSlot10Info    *snap.SlotInfo
	badPathSlot11        *interfaces.ConnectedSlot
	badPathSlot11Info    *snap.SlotInfo
	badPathSlot12        *interfaces.ConnectedSlot
	badPathSlot12Info    *snap.SlotInfo
	badPathSlot13        *interfaces.ConnectedSlot
	badPathSlot13Info    *snap.SlotInfo
	badPathSlot14        *interfaces.ConnectedSlot
	badPathSlot14Info    *snap.SlotInfo
	badPathSlot100       *interfaces.ConnectedSlot
	badPathSlot100Info   *snap.SlotInfo
	badInterfaceSlot     *interfaces.ConnectedSlot
	badInterfaceSlotInfo *snap.SlotInfo

	// Gadget Snap
	gadgetSnapInfo        *snap.Info
	testUDev1             *interfaces.ConnectedSlot
	testUDev1Info         *snap.SlotInfo
	testUDev2             *interfaces.ConnectedSlot
	testUDev2Info         *snap.SlotInfo
	testUDev3             *interfaces.ConnectedSlot
	testUDev3Info         *snap.SlotInfo
	testUDevBadValue1     *interfaces.ConnectedSlot
	testUDevBadValue1Info *snap.SlotInfo
	testUDevBadValue2Info *snap.SlotInfo
	testUDevBadValue3     *interfaces.ConnectedSlot
	testUDevBadValue3Info *snap.SlotInfo
	testUDevBadValue4     *interfaces.ConnectedSlot
	testUDevBadValue4Info *snap.SlotInfo
	testUDevBadValue5     *interfaces.ConnectedSlot
	testUDevBadValue5Info *snap.SlotInfo

	// Consuming Snap
	testPlugPort1     *interfaces.ConnectedPlug
	testPlugPort1Info *snap.PlugInfo
	testPlugPort2     *interfaces.ConnectedPlug
	testPlugPort2Info *snap.PlugInfo
	testPlugPort3     *interfaces.ConnectedPlug
	testPlugPort3Info *snap.PlugInfo
}

var _ = Suite(&SerialPortInterfaceSuite{
	iface: builtin.MustInterface("serial-port"),
})

func (s *SerialPortInterfaceSuite) SetUpTest(c *C) {
	s.osSnapInfo = snaptest.MockInfo(c, `
name: ubuntu-core
version: 0
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
    test-port-8:
        interface: serial-port
        path: /dev/ttymxc2
    test-port-9:
        interface: serial-port
        path: /dev/ttySC0
    test-port-10:
        interface: serial-port
        path: /dev/ttyMSM0
    test-port-11:
        interface: serial-port
        path: /dev/ttyHS0
    test-port-12:
        interface: serial-port
        path: /dev/ttyGS0
    test-port-13:
        interface: serial-port
        path: /dev/ttyLP0
    test-port-unclean:
        interface: serial-port
        path: /dev/./././ttyS1////
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
        path: /dev/ttySC
    bad-path-11:
        interface: serial-port
        path: /dev/ttyMSM
    bad-path-12:
        interface: serial-port
        path: /dev/ttyHS
    bad-path-13:
        interface: serial-port
        path: /dev/ttyGS
    bad-path-14:
        interface: serial-port
        path: /dev/ttyLP
    bad-path-100:
        interface: serial-port
        path: /dev/ttyillegal0
    bad-interface: other-interface
`, nil)
	s.testSlot1Info = s.osSnapInfo.Slots["test-port-1"]
	s.testSlot1 = interfaces.NewConnectedSlot(s.testSlot1Info, nil, nil)
	s.testSlot2Info = s.osSnapInfo.Slots["test-port-2"]
	s.testSlot2 = interfaces.NewConnectedSlot(s.testSlot2Info, nil, nil)
	s.testSlot3Info = s.osSnapInfo.Slots["test-port-3"]
	s.testSlot3 = interfaces.NewConnectedSlot(s.testSlot3Info, nil, nil)
	s.testSlot4Info = s.osSnapInfo.Slots["test-port-4"]
	s.testSlot4 = interfaces.NewConnectedSlot(s.testSlot4Info, nil, nil)
	s.testSlot5Info = s.osSnapInfo.Slots["test-port-5"]
	s.testSlot5 = interfaces.NewConnectedSlot(s.testSlot5Info, nil, nil)
	s.testSlot6Info = s.osSnapInfo.Slots["test-port-6"]
	s.testSlot6 = interfaces.NewConnectedSlot(s.testSlot6Info, nil, nil)
	s.testSlot7Info = s.osSnapInfo.Slots["test-port-7"]
	s.testSlot7 = interfaces.NewConnectedSlot(s.testSlot7Info, nil, nil)
	s.testSlot8Info = s.osSnapInfo.Slots["test-port-8"]
	s.testSlot8 = interfaces.NewConnectedSlot(s.testSlot8Info, nil, nil)
	s.testSlot9Info = s.osSnapInfo.Slots["test-port-9"]
	s.testSlot9 = interfaces.NewConnectedSlot(s.testSlot9Info, nil, nil)
	s.testSlot10Info = s.osSnapInfo.Slots["test-port-10"]
	s.testSlot10 = interfaces.NewConnectedSlot(s.testSlot10Info, nil, nil)
	s.testSlot11Info = s.osSnapInfo.Slots["test-port-11"]
	s.testSlot11 = interfaces.NewConnectedSlot(s.testSlot11Info, nil, nil)
	s.testSlot12Info = s.osSnapInfo.Slots["test-port-12"]
	s.testSlot12 = interfaces.NewConnectedSlot(s.testSlot12Info, nil, nil)
	s.testSlot13Info = s.osSnapInfo.Slots["test-port-13"]
	s.testSlot13 = interfaces.NewConnectedSlot(s.testSlot13Info, nil, nil)
	s.testSlotCleanedInfo = s.osSnapInfo.Slots["test-port-unclean"]
	s.testSlotCleaned = interfaces.NewConnectedSlot(s.testSlotCleanedInfo, nil, nil)
	s.missingPathSlotInfo = s.osSnapInfo.Slots["missing-path"]
	s.missingPathSlot = interfaces.NewConnectedSlot(s.missingPathSlotInfo, nil, nil)
	s.badPathSlot1Info = s.osSnapInfo.Slots["bad-path-1"]
	s.badPathSlot1 = interfaces.NewConnectedSlot(s.badPathSlot1Info, nil, nil)
	s.badPathSlot2Info = s.osSnapInfo.Slots["bad-path-2"]
	s.badPathSlot2 = interfaces.NewConnectedSlot(s.badPathSlot2Info, nil, nil)
	s.badPathSlot3Info = s.osSnapInfo.Slots["bad-path-3"]
	s.badPathSlot3 = interfaces.NewConnectedSlot(s.badPathSlot3Info, nil, nil)
	s.badPathSlot4Info = s.osSnapInfo.Slots["bad-path-4"]
	s.badPathSlot4 = interfaces.NewConnectedSlot(s.badPathSlot4Info, nil, nil)
	s.badPathSlot5Info = s.osSnapInfo.Slots["bad-path-5"]
	s.badPathSlot5 = interfaces.NewConnectedSlot(s.badPathSlot5Info, nil, nil)
	s.badPathSlot6Info = s.osSnapInfo.Slots["bad-path-6"]
	s.badPathSlot6 = interfaces.NewConnectedSlot(s.badPathSlot6Info, nil, nil)
	s.badPathSlot7Info = s.osSnapInfo.Slots["bad-path-7"]
	s.badPathSlot7 = interfaces.NewConnectedSlot(s.badPathSlot7Info, nil, nil)
	s.badPathSlot8Info = s.osSnapInfo.Slots["bad-path-8"]
	s.badPathSlot8 = interfaces.NewConnectedSlot(s.badPathSlot8Info, nil, nil)
	s.badPathSlot9Info = s.osSnapInfo.Slots["bad-path-9"]
	s.badPathSlot9 = interfaces.NewConnectedSlot(s.badPathSlot9Info, nil, nil)
	s.badPathSlot10Info = s.osSnapInfo.Slots["bad-path-10"]
	s.badPathSlot10 = interfaces.NewConnectedSlot(s.badPathSlot10Info, nil, nil)
	s.badPathSlot11Info = s.osSnapInfo.Slots["bad-path-11"]
	s.badPathSlot11 = interfaces.NewConnectedSlot(s.badPathSlot11Info, nil, nil)
	s.badPathSlot12Info = s.osSnapInfo.Slots["bad-path-12"]
	s.badPathSlot12 = interfaces.NewConnectedSlot(s.badPathSlot12Info, nil, nil)
	s.badPathSlot13Info = s.osSnapInfo.Slots["bad-path-13"]
	s.badPathSlot13 = interfaces.NewConnectedSlot(s.badPathSlot13Info, nil, nil)
	s.badPathSlot14Info = s.osSnapInfo.Slots["bad-path-14"]
	s.badPathSlot14 = interfaces.NewConnectedSlot(s.badPathSlot14Info, nil, nil)
	s.badPathSlot100Info = s.osSnapInfo.Slots["bad-path-100"]
	s.badPathSlot100 = interfaces.NewConnectedSlot(s.badPathSlot100Info, nil, nil)
	s.badInterfaceSlotInfo = s.osSnapInfo.Slots["bad-interface"]
	s.badInterfaceSlot = interfaces.NewConnectedSlot(s.badInterfaceSlotInfo, nil, nil)

	s.gadgetSnapInfo = snaptest.MockInfo(c, `
name: some-device
version: 0
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
	s.testUDev1Info = s.gadgetSnapInfo.Slots["test-udev-1"]
	s.testUDev1 = interfaces.NewConnectedSlot(s.testUDev1Info, nil, nil)
	s.testUDev2Info = s.gadgetSnapInfo.Slots["test-udev-2"]
	s.testUDev2 = interfaces.NewConnectedSlot(s.testUDev2Info, nil, nil)
	s.testUDev3Info = s.gadgetSnapInfo.Slots["test-udev-3"]
	s.testUDev3 = interfaces.NewConnectedSlot(s.testUDev3Info, nil, nil)
	s.testUDevBadValue1Info = s.gadgetSnapInfo.Slots["test-udev-bad-value-1"]
	s.testUDevBadValue1 = interfaces.NewConnectedSlot(s.testUDevBadValue1Info, nil, nil)
	s.testUDevBadValue2Info = s.gadgetSnapInfo.Slots["test-udev-bad-value-2"]
	s.testUDevBadValue3 = interfaces.NewConnectedSlot(s.testUDevBadValue2Info, nil, nil)
	s.testUDevBadValue3Info = s.gadgetSnapInfo.Slots["test-udev-bad-value-3"]
	s.testUDevBadValue3 = interfaces.NewConnectedSlot(s.testUDevBadValue3Info, nil, nil)
	s.testUDevBadValue4Info = s.gadgetSnapInfo.Slots["test-udev-bad-value-4"]
	s.testUDevBadValue4 = interfaces.NewConnectedSlot(s.testUDevBadValue4Info, nil, nil)
	s.testUDevBadValue5Info = s.gadgetSnapInfo.Slots["test-udev-bad-value-5"]
	s.testUDevBadValue5 = interfaces.NewConnectedSlot(s.testUDevBadValue5Info, nil, nil)

	consumingSnapInfo := snaptest.MockInfo(c, `
name: client-snap
version: 0
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
	s.testPlugPort1Info = consumingSnapInfo.Plugs["plug-for-port-1"]
	s.testPlugPort1 = interfaces.NewConnectedPlug(s.testPlugPort1Info, nil, nil)
	s.testPlugPort2Info = consumingSnapInfo.Plugs["plug-for-port-2"]
	s.testPlugPort2 = interfaces.NewConnectedPlug(s.testPlugPort2Info, nil, nil)
	s.testPlugPort3Info = consumingSnapInfo.Plugs["plug-for-port-3"]
	s.testPlugPort3 = interfaces.NewConnectedPlug(s.testPlugPort3Info, nil, nil)
}

func (s *SerialPortInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "serial-port")
}

func (s *SerialPortInterfaceSuite) TestSanitizeCoreSnapSlots(c *C) {
	for _, slot := range []*snap.SlotInfo{
		s.testSlot1Info,
		s.testSlot2Info,
		s.testSlot3Info,
		s.testSlot4Info,
		s.testSlot5Info,
		s.testSlot6Info,
		s.testSlot7Info,
		s.testSlot8Info,
		s.testSlot9Info,
		s.testSlot10Info,
		s.testSlot11Info,
		s.testSlot12Info,
		s.testSlot13Info,
	} {
		c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), IsNil)
	}
}

func (s *SerialPortInterfaceSuite) TestSanitizeBadCoreSnapSlots(c *C) {
	// Slots without the "path" attribute are rejected.
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.missingPathSlotInfo), ErrorMatches, `serial-port slot must have a path attribute`)

	// Slots with incorrect value of the "path" attribute are rejected.
	for _, slot := range []*snap.SlotInfo{
		s.badPathSlot1Info,
		s.badPathSlot2Info,
		s.badPathSlot3Info,
		s.badPathSlot4Info,
		s.badPathSlot5Info,
		s.badPathSlot6Info,
		s.badPathSlot7Info,
		s.badPathSlot8Info,
		s.badPathSlot9Info,
		s.badPathSlot10Info,
		s.badPathSlot11Info,
		s.badPathSlot12Info,
		s.badPathSlot13Info,
		s.badPathSlot14Info,
		s.badPathSlot100Info,
	} {
		c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches, "serial-port path attribute must be a valid device node")
	}
}

func (s *SerialPortInterfaceSuite) TestSanitizeGadgetSnapSlots(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.testUDev1Info), IsNil)
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.testUDev2Info), IsNil)
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.testUDev3Info), IsNil)
}

func (s *SerialPortInterfaceSuite) TestSanitizeBadGadgetSnapSlots(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.testUDevBadValue1Info), ErrorMatches, "serial-port usb-vendor attribute not valid: -1")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.testUDevBadValue2Info), ErrorMatches, "serial-port usb-product attribute not valid: 65536")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.testUDevBadValue3Info), ErrorMatches, "serial-port path attribute specifies invalid symlink location")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.testUDevBadValue4Info), ErrorMatches, "serial-port usb-interface-number attribute cannot be negative or larger than 31")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.testUDevBadValue5Info), ErrorMatches, "serial-port usb-interface-number attribute cannot be negative or larger than 31")
}

func (s *SerialPortInterfaceSuite) TestPermanentSlotUDevSnippets(c *C) {
	spec := udev.NewSpecification(interfaces.NewSnapAppSet(s.osSnapInfo, nil))
	for _, slot := range []*snap.SlotInfo{s.testSlot1Info, s.testSlot2Info, s.testSlot3Info, s.testSlot4Info} {
		err := spec.AddPermanentSlot(s.iface, slot)
		c.Assert(err, IsNil)
		c.Assert(spec.Snippets(), HasLen, 0)
	}

	spec = udev.NewSpecification(interfaces.NewSnapAppSet(s.gadgetSnapInfo, nil))
	expectedSnippet1 := `# serial-port
IMPORT{builtin}="usb_id"
SUBSYSTEM=="tty", SUBSYSTEMS=="usb", ATTRS{idVendor}=="0001", ATTRS{idProduct}=="0001", SYMLINK+="serial-port-zigbee"`
	err := spec.AddPermanentSlot(s.iface, s.testUDev1Info)
	c.Assert(err, IsNil)
	c.Assert(spec.Snippets(), HasLen, 1)
	snippet := spec.Snippets()[0]
	c.Assert(snippet, Equals, expectedSnippet1)

	spec = udev.NewSpecification(interfaces.NewSnapAppSet(s.gadgetSnapInfo, nil))
	expectedSnippet2 := `# serial-port
IMPORT{builtin}="usb_id"
SUBSYSTEM=="tty", SUBSYSTEMS=="usb", ATTRS{idVendor}=="ffff", ATTRS{idProduct}=="ffff", SYMLINK+="serial-port-mydevice"`
	err = spec.AddPermanentSlot(s.iface, s.testUDev2Info)
	c.Assert(err, IsNil)
	c.Assert(spec.Snippets(), HasLen, 1)
	snippet = spec.Snippets()[0]
	c.Assert(snippet, Equals, expectedSnippet2)

	spec = udev.NewSpecification(interfaces.NewSnapAppSet(s.gadgetSnapInfo, nil))
	// The ENV{ID_USB_INTERFACE_NUM} is set to two hex digits
	// For instance, the expectedSnippet3 is set to 00
	expectedSnippet3 := `# serial-port
IMPORT{builtin}="usb_id"
SUBSYSTEM=="tty", SUBSYSTEMS=="usb", ATTRS{idVendor}=="abcd", ATTRS{idProduct}=="1234", ENV{ID_USB_INTERFACE_NUM}=="00", SYMLINK+="serial-port-myserial"`
	err = spec.AddPermanentSlot(s.iface, s.testUDev3Info)
	c.Assert(err, IsNil)
	c.Assert(spec.Snippets(), HasLen, 1)
	snippet = spec.Snippets()[0]
	c.Assert(snippet, Equals, expectedSnippet3)
}

func (s *SerialPortInterfaceSuite) TestConnectedPlugUDevSnippets(c *C) {
	// add the plug for the slot with just path
	spec := udev.NewSpecification(interfaces.NewSnapAppSet(s.testPlugPort1.Snap(), nil))
	err := spec.AddConnectedPlug(s.iface, s.testPlugPort1, s.testSlot1)
	c.Assert(err, IsNil)
	c.Assert(spec.Snippets(), HasLen, 2)
	snippet := spec.Snippets()[0]
	expectedSnippet1 := `# serial-port
SUBSYSTEM=="tty", KERNEL=="ttyS0", TAG+="snap_client-snap_app-accessing-2-ports"`
	c.Assert(snippet, Equals, expectedSnippet1)
	extraSnippet := spec.Snippets()[1]
	expectedExtraSnippet1 := fmt.Sprintf(`TAG=="snap_client-snap_app-accessing-2-ports", SUBSYSTEM!="module", SUBSYSTEM!="subsystem", RUN+="%v/snap-device-helper $env{ACTION} snap_client-snap_app-accessing-2-ports $devpath $major:$minor"`, dirs.DistroLibExecDir)
	c.Assert(extraSnippet, Equals, expectedExtraSnippet1)

	// add plug for the first slot with product and vendor ids
	spec = udev.NewSpecification(interfaces.NewSnapAppSet(s.testPlugPort1.Snap(), nil))
	err = spec.AddConnectedPlug(s.iface, s.testPlugPort1, s.testUDev1)
	c.Assert(err, IsNil)
	c.Assert(spec.Snippets(), HasLen, 2)
	snippet = spec.Snippets()[0]
	expectedSnippet2 := `# serial-port
IMPORT{builtin}="usb_id"
SUBSYSTEM=="tty", SUBSYSTEMS=="usb", ATTRS{idVendor}=="0001", ATTRS{idProduct}=="0001", TAG+="snap_client-snap_app-accessing-2-ports"`
	c.Assert(snippet, Equals, expectedSnippet2)
	extraSnippet = spec.Snippets()[1]
	expectedExtraSnippet2 := fmt.Sprintf(`TAG=="snap_client-snap_app-accessing-2-ports", SUBSYSTEM!="module", SUBSYSTEM!="subsystem", RUN+="%v/snap-device-helper $env{ACTION} snap_client-snap_app-accessing-2-ports $devpath $major:$minor"`, dirs.DistroLibExecDir)
	c.Assert(extraSnippet, Equals, expectedExtraSnippet2)

	// add plug for the first slot with product and vendor ids
	spec = udev.NewSpecification(interfaces.NewSnapAppSet(s.testPlugPort2.Snap(), nil))
	err = spec.AddConnectedPlug(s.iface, s.testPlugPort2, s.testUDev2)
	c.Assert(err, IsNil)
	c.Assert(spec.Snippets(), HasLen, 2)
	snippet = spec.Snippets()[0]
	expectedSnippet3 := `# serial-port
IMPORT{builtin}="usb_id"
SUBSYSTEM=="tty", SUBSYSTEMS=="usb", ATTRS{idVendor}=="ffff", ATTRS{idProduct}=="ffff", TAG+="snap_client-snap_app-accessing-2-ports"`
	c.Assert(snippet, Equals, expectedSnippet3)
	extraSnippet = spec.Snippets()[1]
	expectedExtraSnippet3 := fmt.Sprintf(`TAG=="snap_client-snap_app-accessing-2-ports", SUBSYSTEM!="module", SUBSYSTEM!="subsystem", RUN+="%v/snap-device-helper $env{ACTION} snap_client-snap_app-accessing-2-ports $devpath $major:$minor"`, dirs.DistroLibExecDir)
	c.Assert(extraSnippet, Equals, expectedExtraSnippet3)

	// add plug for the first slot with product and vendor ids and usb interface number
	spec = udev.NewSpecification(interfaces.NewSnapAppSet(s.testPlugPort2.Snap(), nil))
	err = spec.AddConnectedPlug(s.iface, s.testPlugPort2, s.testUDev3)
	c.Assert(err, IsNil)
	c.Assert(spec.Snippets(), HasLen, 2)
	snippet = spec.Snippets()[0]
	expectedSnippet4 := `# serial-port
IMPORT{builtin}="usb_id"
SUBSYSTEM=="tty", SUBSYSTEMS=="usb", ATTRS{idVendor}=="abcd", ATTRS{idProduct}=="1234", ENV{ID_USB_INTERFACE_NUM}=="00", TAG+="snap_client-snap_app-accessing-2-ports"`
	c.Assert(snippet, Equals, expectedSnippet4)
	extraSnippet = spec.Snippets()[1]
	expectedExtraSnippet4 := fmt.Sprintf(`TAG=="snap_client-snap_app-accessing-2-ports", SUBSYSTEM!="module", SUBSYSTEM!="subsystem", RUN+="%v/snap-device-helper $env{ACTION} snap_client-snap_app-accessing-2-ports $devpath $major:$minor"`, dirs.DistroLibExecDir)
	c.Assert(extraSnippet, Equals, expectedExtraSnippet4)
}

func (s *SerialPortInterfaceSuite) TestConnectedPlugAppArmorSnippets(c *C) {
	checkConnectedPlugSnippet := func(plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot, expectedSnippet string) {
		apparmorSpec := apparmor.NewSpecification(interfaces.NewSnapAppSet(plug.Snap(), nil))
		err := apparmorSpec.AddConnectedPlug(s.iface, plug, slot)
		c.Assert(err, IsNil)

		c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.client-snap.app-accessing-2-ports"})
		snippet := apparmorSpec.SnippetForTag("snap.client-snap.app-accessing-2-ports")
		c.Assert(snippet, DeepEquals, expectedSnippet)
	}

	expectedSnippet1 := `/dev/ttyS0 rwk,`
	checkConnectedPlugSnippet(s.testPlugPort1, s.testSlot1, expectedSnippet1)
	expectedSnippet2 := `/dev/ttyUSB927 rwk,`
	checkConnectedPlugSnippet(s.testPlugPort1, s.testSlot2, expectedSnippet2)

	expectedSnippet3 := `/dev/ttyS42 rwk,`
	checkConnectedPlugSnippet(s.testPlugPort1, s.testSlot3, expectedSnippet3)

	expectedSnippet4 := `/dev/ttyO0 rwk,`
	checkConnectedPlugSnippet(s.testPlugPort1, s.testSlot4, expectedSnippet4)

	expectedSnippet5 := `/dev/ttyACM0 rwk,`
	checkConnectedPlugSnippet(s.testPlugPort1, s.testSlot5, expectedSnippet5)

	expectedSnippet6 := `/dev/ttyAMA0 rwk,`
	checkConnectedPlugSnippet(s.testPlugPort1, s.testSlot6, expectedSnippet6)

	expectedSnippet7 := `/dev/ttyXRUSB0 rwk,`
	checkConnectedPlugSnippet(s.testPlugPort1, s.testSlot7, expectedSnippet7)

	expectedSnippet8 := `/dev/ttymxc2 rwk,`
	checkConnectedPlugSnippet(s.testPlugPort1, s.testSlot8, expectedSnippet8)

	expectedSnippet9 := `/dev/ttySC0 rwk,`
	checkConnectedPlugSnippet(s.testPlugPort1, s.testSlot9, expectedSnippet9)

	expectedSnippet10 := `/dev/ttyMSM0 rwk,`
	checkConnectedPlugSnippet(s.testPlugPort1, s.testSlot10, expectedSnippet10)

	expectedSnippet11 := `/dev/ttyHS0 rwk,`
	checkConnectedPlugSnippet(s.testPlugPort1, s.testSlot11, expectedSnippet11)

	expectedSnippet12 := `/dev/ttyGS0 rwk,`
	checkConnectedPlugSnippet(s.testPlugPort1, s.testSlot12, expectedSnippet12)

	expectedSnippet13 := `/dev/ttyLP0 rwk,`
	checkConnectedPlugSnippet(s.testPlugPort1, s.testSlot13, expectedSnippet13)

	expectedSnippet100 := `/dev/tty[A-Z]*[0-9] rwk,`
	checkConnectedPlugSnippet(s.testPlugPort1, s.testUDev1, expectedSnippet100)

	expectedSnippet101 := `/dev/tty[A-Z]*[0-9] rwk,`
	checkConnectedPlugSnippet(s.testPlugPort2, s.testUDev2, expectedSnippet101)

	expectedSnippet102 := `/dev/tty[A-Z]*[0-9] rwk,`
	checkConnectedPlugSnippet(s.testPlugPort2, s.testUDev3, expectedSnippet102)
}

func (s *SerialPortInterfaceSuite) TestConnectedPlugUDevSnippetsForPath(c *C) {
	checkConnectedPlugSnippet := func(plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot, expectedSnippet string, expectedExtraSnippet string) {
		udevSpec := udev.NewSpecification(interfaces.NewSnapAppSet(plug.Snap(), nil))
		err := udevSpec.AddConnectedPlug(s.iface, plug, slot)
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
	expectedExtraSnippet1 := fmt.Sprintf(`TAG=="snap_client-snap_app-accessing-3rd-port", SUBSYSTEM!="module", SUBSYSTEM!="subsystem", RUN+="%v/snap-device-helper $env{ACTION} snap_client-snap_app-accessing-3rd-port $devpath $major:$minor"`, dirs.DistroLibExecDir)
	checkConnectedPlugSnippet(s.testPlugPort3, s.testSlot1, expectedSnippet1, expectedExtraSnippet1)

	expectedSnippet2 := `# serial-port
SUBSYSTEM=="tty", KERNEL=="ttyUSB927", TAG+="snap_client-snap_app-accessing-3rd-port"`
	expectedExtraSnippet2 := fmt.Sprintf(`TAG=="snap_client-snap_app-accessing-3rd-port", SUBSYSTEM!="module", SUBSYSTEM!="subsystem", RUN+="%v/snap-device-helper $env{ACTION} snap_client-snap_app-accessing-3rd-port $devpath $major:$minor"`, dirs.DistroLibExecDir)
	checkConnectedPlugSnippet(s.testPlugPort3, s.testSlot2, expectedSnippet2, expectedExtraSnippet2)

	expectedSnippet3 := `# serial-port
SUBSYSTEM=="tty", KERNEL=="ttyS42", TAG+="snap_client-snap_app-accessing-3rd-port"`
	expectedExtraSnippet3 := fmt.Sprintf(`TAG=="snap_client-snap_app-accessing-3rd-port", SUBSYSTEM!="module", SUBSYSTEM!="subsystem", RUN+="%v/snap-device-helper $env{ACTION} snap_client-snap_app-accessing-3rd-port $devpath $major:$minor"`, dirs.DistroLibExecDir)
	checkConnectedPlugSnippet(s.testPlugPort3, s.testSlot3, expectedSnippet3, expectedExtraSnippet3)

	expectedSnippet4 := `# serial-port
SUBSYSTEM=="tty", KERNEL=="ttyO0", TAG+="snap_client-snap_app-accessing-3rd-port"`
	expectedExtraSnippet4 := fmt.Sprintf(`TAG=="snap_client-snap_app-accessing-3rd-port", SUBSYSTEM!="module", SUBSYSTEM!="subsystem", RUN+="%v/snap-device-helper $env{ACTION} snap_client-snap_app-accessing-3rd-port $devpath $major:$minor"`, dirs.DistroLibExecDir)
	checkConnectedPlugSnippet(s.testPlugPort3, s.testSlot4, expectedSnippet4, expectedExtraSnippet4)

	expectedSnippet5 := `# serial-port
SUBSYSTEM=="tty", KERNEL=="ttyACM0", TAG+="snap_client-snap_app-accessing-3rd-port"`
	expectedExtraSnippet5 := fmt.Sprintf(`TAG=="snap_client-snap_app-accessing-3rd-port", SUBSYSTEM!="module", SUBSYSTEM!="subsystem", RUN+="%v/snap-device-helper $env{ACTION} snap_client-snap_app-accessing-3rd-port $devpath $major:$minor"`, dirs.DistroLibExecDir)
	checkConnectedPlugSnippet(s.testPlugPort3, s.testSlot5, expectedSnippet5, expectedExtraSnippet5)

	expectedSnippet6 := `# serial-port
SUBSYSTEM=="tty", KERNEL=="ttyAMA0", TAG+="snap_client-snap_app-accessing-3rd-port"`
	expectedExtraSnippet6 := fmt.Sprintf(`TAG=="snap_client-snap_app-accessing-3rd-port", SUBSYSTEM!="module", SUBSYSTEM!="subsystem", RUN+="%v/snap-device-helper $env{ACTION} snap_client-snap_app-accessing-3rd-port $devpath $major:$minor"`, dirs.DistroLibExecDir)
	checkConnectedPlugSnippet(s.testPlugPort3, s.testSlot6, expectedSnippet6, expectedExtraSnippet6)

	expectedSnippet7 := `# serial-port
SUBSYSTEM=="tty", KERNEL=="ttyXRUSB0", TAG+="snap_client-snap_app-accessing-3rd-port"`
	expectedExtraSnippet7 := fmt.Sprintf(`TAG=="snap_client-snap_app-accessing-3rd-port", SUBSYSTEM!="module", SUBSYSTEM!="subsystem", RUN+="%v/snap-device-helper $env{ACTION} snap_client-snap_app-accessing-3rd-port $devpath $major:$minor"`, dirs.DistroLibExecDir)
	checkConnectedPlugSnippet(s.testPlugPort3, s.testSlot7, expectedSnippet7, expectedExtraSnippet7)

	expectedSnippet8 := `# serial-port
SUBSYSTEM=="tty", KERNEL=="ttymxc2", TAG+="snap_client-snap_app-accessing-3rd-port"`
	expectedExtraSnippet8 := fmt.Sprintf(`TAG=="snap_client-snap_app-accessing-3rd-port", SUBSYSTEM!="module", SUBSYSTEM!="subsystem", RUN+="%v/snap-device-helper $env{ACTION} snap_client-snap_app-accessing-3rd-port $devpath $major:$minor"`, dirs.DistroLibExecDir)
	checkConnectedPlugSnippet(s.testPlugPort3, s.testSlot8, expectedSnippet8, expectedExtraSnippet8)

	expectedSnippet9 := `# serial-port
SUBSYSTEM=="tty", KERNEL=="ttySC0", TAG+="snap_client-snap_app-accessing-3rd-port"`
	expectedExtraSnippet9 := fmt.Sprintf(`TAG=="snap_client-snap_app-accessing-3rd-port", SUBSYSTEM!="module", SUBSYSTEM!="subsystem", RUN+="%v/snap-device-helper $env{ACTION} snap_client-snap_app-accessing-3rd-port $devpath $major:$minor"`, dirs.DistroLibExecDir)
	checkConnectedPlugSnippet(s.testPlugPort3, s.testSlot9, expectedSnippet9, expectedExtraSnippet9)

	expectedSnippet10 := `# serial-port
SUBSYSTEM=="tty", KERNEL=="ttyMSM0", TAG+="snap_client-snap_app-accessing-3rd-port"`
	expectedExtraSnippet10 := fmt.Sprintf(`TAG=="snap_client-snap_app-accessing-3rd-port", SUBSYSTEM!="module", SUBSYSTEM!="subsystem", RUN+="%v/snap-device-helper $env{ACTION} snap_client-snap_app-accessing-3rd-port $devpath $major:$minor"`, dirs.DistroLibExecDir)
	checkConnectedPlugSnippet(s.testPlugPort3, s.testSlot10, expectedSnippet10, expectedExtraSnippet10)

	expectedSnippet11 := `# serial-port
SUBSYSTEM=="tty", KERNEL=="ttyHS0", TAG+="snap_client-snap_app-accessing-3rd-port"`
	expectedExtraSnippet11 := fmt.Sprintf(`TAG=="snap_client-snap_app-accessing-3rd-port", SUBSYSTEM!="module", SUBSYSTEM!="subsystem", RUN+="%v/snap-device-helper $env{ACTION} snap_client-snap_app-accessing-3rd-port $devpath $major:$minor"`, dirs.DistroLibExecDir)
	checkConnectedPlugSnippet(s.testPlugPort3, s.testSlot11, expectedSnippet11, expectedExtraSnippet11)

	expectedSnippet12 := `# serial-port
SUBSYSTEM=="tty", KERNEL=="ttyGS0", TAG+="snap_client-snap_app-accessing-3rd-port"`
	expectedExtraSnippet12 := fmt.Sprintf(`TAG=="snap_client-snap_app-accessing-3rd-port", SUBSYSTEM!="module", SUBSYSTEM!="subsystem", RUN+="%v/snap-device-helper $env{ACTION} snap_client-snap_app-accessing-3rd-port $devpath $major:$minor"`, dirs.DistroLibExecDir)
	checkConnectedPlugSnippet(s.testPlugPort3, s.testSlot12, expectedSnippet12, expectedExtraSnippet12)

	expectedSnippet13 := `# serial-port
SUBSYSTEM=="tty", KERNEL=="ttyLP0", TAG+="snap_client-snap_app-accessing-3rd-port"`
	expectedExtraSnippet13 := fmt.Sprintf(`TAG=="snap_client-snap_app-accessing-3rd-port", SUBSYSTEM!="module", SUBSYSTEM!="subsystem", RUN+="%v/snap-device-helper $env{ACTION} snap_client-snap_app-accessing-3rd-port $devpath $major:$minor"`, dirs.DistroLibExecDir)
	checkConnectedPlugSnippet(s.testPlugPort3, s.testSlot13, expectedSnippet13, expectedExtraSnippet13)

	// these have product and vendor ids
	expectedSnippet100 := `# serial-port
IMPORT{builtin}="usb_id"
SUBSYSTEM=="tty", SUBSYSTEMS=="usb", ATTRS{idVendor}=="0001", ATTRS{idProduct}=="0001", TAG+="snap_client-snap_app-accessing-3rd-port"`
	expectedExtraSnippet100 := fmt.Sprintf(`TAG=="snap_client-snap_app-accessing-3rd-port", SUBSYSTEM!="module", SUBSYSTEM!="subsystem", RUN+="%v/snap-device-helper $env{ACTION} snap_client-snap_app-accessing-3rd-port $devpath $major:$minor"`, dirs.DistroLibExecDir)
	checkConnectedPlugSnippet(s.testPlugPort3, s.testUDev1, expectedSnippet100, expectedExtraSnippet100)

	expectedSnippet101 := `# serial-port
IMPORT{builtin}="usb_id"
SUBSYSTEM=="tty", SUBSYSTEMS=="usb", ATTRS{idVendor}=="ffff", ATTRS{idProduct}=="ffff", TAG+="snap_client-snap_app-accessing-3rd-port"`
	expectedExtraSnippet101 := fmt.Sprintf(`TAG=="snap_client-snap_app-accessing-3rd-port", SUBSYSTEM!="module", SUBSYSTEM!="subsystem", RUN+="%v/snap-device-helper $env{ACTION} snap_client-snap_app-accessing-3rd-port $devpath $major:$minor"`, dirs.DistroLibExecDir)
	checkConnectedPlugSnippet(s.testPlugPort3, s.testUDev2, expectedSnippet101, expectedExtraSnippet101)
}

func (s *SerialPortInterfaceSuite) TestHotplugDeviceDetected(c *C) {
	hotplugIface := s.iface.(hotplug.Definer)
	di, err := hotplug.NewHotplugDeviceInfo(map[string]string{"DEVPATH": "/sys/foo/bar", "DEVNAME": "/dev/ttyUSB0", "ID_VENDOR_ID": "1234", "ID_MODEL_ID": "5678", "ACTION": "add", "SUBSYSTEM": "tty", "ID_BUS": "usb"})
	c.Assert(err, IsNil)
	proposedSlot, err := hotplugIface.HotplugDeviceDetected(di)
	c.Assert(err, IsNil)
	c.Assert(proposedSlot, DeepEquals, &hotplug.ProposedSlot{Attrs: map[string]interface{}{"path": "/dev/ttyUSB0", "usb-vendor": "1234", "usb-product": "5678"}})
}

func (s *SerialPortInterfaceSuite) TestHotplugDeviceDetectedNotSerialPort(c *C) {
	hotplugIface := s.iface.(hotplug.Definer)
	di, err := hotplug.NewHotplugDeviceInfo(map[string]string{"DEVPATH": "/sys/foo/bar", "DEVNAME": "/dev/other", "ID_VENDOR_ID": "1234", "ID_MODEL_ID": "5678", "ACTION": "add", "SUBSYSTEM": "tty", "ID_BUS": "usb"})
	c.Assert(err, IsNil)
	proposedSlot, err := hotplugIface.HotplugDeviceDetected(di)
	c.Assert(err, IsNil)
	c.Assert(proposedSlot, IsNil)
}

func (s *SerialPortInterfaceSuite) TestHotplugHandledByGadget(c *C) {
	byGadgetPred := s.iface.(hotplug.HandledByGadgetPredicate)
	di, err := hotplug.NewHotplugDeviceInfo(map[string]string{"DEVPATH": "/sys/foo/bar", "DEVNAME": "/dev/ttyXRUSB0", "ACTION": "add", "SUBSYSTEM": "tty", "ID_BUS": "usb"})
	c.Assert(err, IsNil)

	c.Assert(byGadgetPred.HandledByGadget(di, s.testSlot5Info), Equals, false)
	// matching path /dev/ttyXRUSB0
	c.Assert(byGadgetPred.HandledByGadget(di, s.testSlot7Info), Equals, true)

	// matching on vendor, model, usb interface num
	di, err = hotplug.NewHotplugDeviceInfo(map[string]string{"DEVPATH": "/sys/foo/bar", "DEVNAME": "/dev/path", "ID_VENDOR_ID": "abcd", "ID_MODEL_ID": "1234", "ID_USB_INTERFACE_NUM": "00", "ACTION": "add", "SUBSYSTEM": "tty", "ID_BUS": "usb"})
	c.Assert(err, IsNil)
	c.Assert(byGadgetPred.HandledByGadget(di, s.testUDev3Info), Equals, true)
	// model doesn't match, everything else matches
	di, err = hotplug.NewHotplugDeviceInfo(map[string]string{"DEVPATH": "/sys/foo/bar", "DEVNAME": "/dev/path", "ID_VENDOR_ID": "abcd", "ID_MODEL_ID": "ffff", "ID_USB_INTERFACE_NUM": "00", "ACTION": "add", "SUBSYSTEM": "tty", "ID_BUS": "usb"})
	c.Assert(err, IsNil)
	c.Assert(byGadgetPred.HandledByGadget(di, s.testUDev3Info), Equals, false)
	// vendor doesn't match, everything else matches
	di, err = hotplug.NewHotplugDeviceInfo(map[string]string{"DEVPATH": "/sys/foo/bar", "DEVNAME": "/dev/path", "ID_VENDOR_ID": "eeee", "ID_MODEL_ID": "1234", "ID_USB_INTERFACE_NUM": "00", "ACTION": "add", "SUBSYSTEM": "tty", "ID_BUS": "usb"})
	c.Assert(err, IsNil)
	c.Assert(byGadgetPred.HandledByGadget(di, s.testUDev3Info), Equals, false)
	// usb interface doesn't match, everything else matches
	di, err = hotplug.NewHotplugDeviceInfo(map[string]string{"DEVPATH": "/sys/foo/bar", "DEVNAME": "/dev/path", "ID_VENDOR_ID": "abcd", "ID_MODEL_ID": "1234", "ID_USB_INTERFACE_NUM": "ff", "ACTION": "add", "SUBSYSTEM": "tty", "ID_BUS": "usb"})
	c.Assert(err, IsNil)
	c.Assert(byGadgetPred.HandledByGadget(di, s.testUDev3Info), Equals, false)

	// usb interface num is optional, match on vendor/model
	di, err = hotplug.NewHotplugDeviceInfo(map[string]string{"DEVPATH": "/sys/foo/bar", "DEVNAME": "/dev/path", "ID_VENDOR_ID": "ffff", "ID_MODEL_ID": "ffff", "ACTION": "add", "SUBSYSTEM": "tty", "ID_BUS": "usb"})
	c.Assert(err, IsNil)
	c.Assert(byGadgetPred.HandledByGadget(di, s.testUDev2Info), Equals, true)
}

func (s *SerialPortInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
