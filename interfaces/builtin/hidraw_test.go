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

type HidrawInterfaceSuite struct {
	testutil.BaseTest
	iface interfaces.Interface

	// OS Snap
	testSlot1        *interfaces.Slot
	testSlot2        *interfaces.Slot
	missingPathSlot  *interfaces.Slot
	badPathSlot1     *interfaces.Slot
	badPathSlot2     *interfaces.Slot
	badPathSlot3     *interfaces.Slot
	badInterfaceSlot *interfaces.Slot

	// Gadget Snap
	testUDev1         *interfaces.Slot
	testUDev2         *interfaces.Slot
	testUDevBadValue1 *interfaces.Slot
	testUDevBadValue2 *interfaces.Slot
	testUDevBadValue3 *interfaces.Slot

	// Consuming Snap
	testPlugPort1 *interfaces.Plug
	testPlugPort2 *interfaces.Plug
	testPlugPort3 *interfaces.Plug
}

var _ = Suite(&HidrawInterfaceSuite{
	iface: builtin.MustInterface("hidraw"),
})

func (s *HidrawInterfaceSuite) SetUpTest(c *C) {
	osSnapInfo := snaptest.MockInfo(c, `
name: ubuntu-core
type: os
slots:
    test-port-1:
        interface: hidraw
        path: /dev/hidraw0
    test-port-2:
        interface: hidraw
        path: /dev/hidraw987
    missing-path: hidraw
    bad-path-1:
        interface: hidraw
        path: path
    bad-path-2:
        interface: hidraw
        path: /dev/hid0
    bad-path-3:
        interface: hidraw
        path: /dev/hidraw9271
    bad-interface: other-interface
`, nil)
	s.testSlot1 = &interfaces.Slot{SlotInfo: osSnapInfo.Slots["test-port-1"]}
	s.testSlot2 = &interfaces.Slot{SlotInfo: osSnapInfo.Slots["test-port-2"]}
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
      interface: hidraw
      usb-vendor: 0x0001
      usb-product: 0x0001
      path: /dev/hidraw-canbus
  test-udev-2:
      interface: hidraw
      usb-vendor: 0xffff
      usb-product: 0xffff
      path: /dev/hidraw-mydevice
  test-udev-bad-value-1:
      interface: hidraw
      usb-vendor: -1
      usb-product: 0xffff
      path: /dev/hidraw-mydevice
  test-udev-bad-value-2:
      interface: hidraw
      usb-vendor: 0x1234
      usb-product: 0x10000
      path: /dev/hidraw-mydevice
  test-udev-bad-value-3:
      interface: hidraw
      usb-vendor: 0x789a
      usb-product: 0x4321
      path: /dev/my-device
`, nil)
	s.testUDev1 = &interfaces.Slot{SlotInfo: gadgetSnapInfo.Slots["test-udev-1"]}
	s.testUDev2 = &interfaces.Slot{SlotInfo: gadgetSnapInfo.Slots["test-udev-2"]}
	s.testUDevBadValue1 = &interfaces.Slot{SlotInfo: gadgetSnapInfo.Slots["test-udev-bad-value-1"]}
	s.testUDevBadValue2 = &interfaces.Slot{SlotInfo: gadgetSnapInfo.Slots["test-udev-bad-value-2"]}
	s.testUDevBadValue3 = &interfaces.Slot{SlotInfo: gadgetSnapInfo.Slots["test-udev-bad-value-3"]}

	consumingSnapInfo := snaptest.MockInfo(c, `
name: client-snap
plugs:
    plug-for-device-1:
        interface: hidraw
    plug-for-device-2:
        interface: hidraw
    plug-for-device-3:
        interface: hidraw

apps:
    app-accessing-1-device:
        command: foo
        plugs: [hidraw]
    app-accessing-2-devices:
        command: bar
        plugs: [plug-for-device-1, plug-for-device-2]
    app-accessing-3rd-device:
        command: baz
        plugs: [plug-for-device-3]
`, nil)
	s.testPlugPort1 = &interfaces.Plug{PlugInfo: consumingSnapInfo.Plugs["plug-for-device-1"]}
	s.testPlugPort2 = &interfaces.Plug{PlugInfo: consumingSnapInfo.Plugs["plug-for-device-2"]}
	s.testPlugPort3 = &interfaces.Plug{PlugInfo: consumingSnapInfo.Plugs["plug-for-device-3"]}
}

func (s *HidrawInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "hidraw")
}

func (s *HidrawInterfaceSuite) TestSanitizeCoreSnapSlots(c *C) {
	for _, slot := range []*interfaces.Slot{s.testSlot1, s.testSlot2} {
		c.Assert(slot.Sanitize(s.iface), IsNil)
	}
}

func (s *HidrawInterfaceSuite) TestSanitizeBadCoreSnapSlots(c *C) {
	// Slots without the "path" attribute are rejected.
	c.Assert(s.missingPathSlot.Sanitize(s.iface), ErrorMatches, `hidraw slots must have a path attribute`)

	// Slots with incorrect value of the "path" attribute are rejected.
	for _, slot := range []*interfaces.Slot{s.badPathSlot1, s.badPathSlot2, s.badPathSlot3} {
		c.Assert(slot.Sanitize(s.iface), ErrorMatches, "hidraw path attribute must be a valid device node")
	}
}

func (s *HidrawInterfaceSuite) TestSanitizeGadgetSnapSlots(c *C) {
	c.Assert(s.testUDev1.Sanitize(s.iface), IsNil)
	c.Assert(s.testUDev2.Sanitize(s.iface), IsNil)
}

func (s *HidrawInterfaceSuite) TestSanitizeBadGadgetSnapSlots(c *C) {
	c.Assert(s.testUDevBadValue1.Sanitize(s.iface), ErrorMatches, "hidraw usb-vendor attribute not valid: -1")
	c.Assert(s.testUDevBadValue2.Sanitize(s.iface), ErrorMatches, "hidraw usb-product attribute not valid: 65536")
	c.Assert(s.testUDevBadValue3.Sanitize(s.iface), ErrorMatches, "hidraw path attribute specifies invalid symlink location")
}

func (s *HidrawInterfaceSuite) TestPermanentSlotUDevSnippets(c *C) {
	spec := &udev.Specification{}
	for _, slot := range []*interfaces.Slot{s.testSlot1, s.testSlot2} {
		c.Assert(spec.AddPermanentSlot(s.iface, slot.SlotInfo), IsNil)
		c.Assert(spec.Snippets(), HasLen, 0)
	}

	expectedSnippet1 := `# hidraw
IMPORT{builtin}="usb_id"
SUBSYSTEM=="hidraw", SUBSYSTEMS=="usb", ATTRS{idVendor}=="0001", ATTRS{idProduct}=="0001", SYMLINK+="hidraw-canbus"`
	c.Assert(spec.AddPermanentSlot(s.iface, s.testUDev1.SlotInfo), IsNil)
	c.Assert(spec.Snippets(), HasLen, 1)
	snippet := spec.Snippets()[0]
	c.Assert(snippet, Equals, expectedSnippet1)

	expectedSnippet2 := `# hidraw
IMPORT{builtin}="usb_id"
SUBSYSTEM=="hidraw", SUBSYSTEMS=="usb", ATTRS{idVendor}=="ffff", ATTRS{idProduct}=="ffff", SYMLINK+="hidraw-mydevice"`
	spec = &udev.Specification{}
	c.Assert(spec.AddPermanentSlot(s.iface, s.testUDev2.SlotInfo), IsNil)
	c.Assert(spec.Snippets(), HasLen, 1)
	snippet = spec.Snippets()[0]
	c.Assert(snippet, Equals, expectedSnippet2)
}

func (s *HidrawInterfaceSuite) TestConnectedPlugUDevSnippets(c *C) {
	// add the plug for the slot with just path
	spec := &udev.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.testPlugPort1, nil, s.testSlot1, nil), IsNil)
	c.Assert(spec.Snippets(), HasLen, 1)
	snippet := spec.Snippets()[0]
	expectedSnippet1 := `# hidraw
SUBSYSTEM=="hidraw", KERNEL=="hidraw0", TAG+="snap_client-snap_app-accessing-2-devices"`
	c.Assert(snippet, Equals, expectedSnippet1)

	// add the plug for the first slot with vendor and product ids
	spec = &udev.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.testPlugPort1, nil, s.testUDev1, nil), IsNil)
	c.Assert(spec.Snippets(), HasLen, 1)
	snippet = spec.Snippets()[0]
	expectedSnippet2 := `# hidraw
IMPORT{builtin}="usb_id"
SUBSYSTEM=="hidraw", SUBSYSTEMS=="usb", ATTRS{idVendor}=="0001", ATTRS{idProduct}=="0001", TAG+="snap_client-snap_app-accessing-2-devices"`
	c.Assert(snippet, Equals, expectedSnippet2)

	// add the plug for the second slot with vendor and product ids
	spec = &udev.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.testPlugPort2, nil, s.testUDev2, nil), IsNil)
	c.Assert(spec.Snippets(), HasLen, 1)
	snippet = spec.Snippets()[0]
	expectedSnippet3 := `# hidraw
IMPORT{builtin}="usb_id"
SUBSYSTEM=="hidraw", SUBSYSTEMS=="usb", ATTRS{idVendor}=="ffff", ATTRS{idProduct}=="ffff", TAG+="snap_client-snap_app-accessing-2-devices"`
	c.Assert(snippet, Equals, expectedSnippet3)
}

func (s *HidrawInterfaceSuite) TestConnectedPlugAppArmorSnippets(c *C) {
	expectedSnippet1 := `/dev/hidraw0 rw,`
	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, s.testPlugPort1, nil, s.testSlot1, nil)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.client-snap.app-accessing-2-devices"})
	snippet := apparmorSpec.SnippetForTag("snap.client-snap.app-accessing-2-devices")
	c.Assert(snippet, DeepEquals, expectedSnippet1)

	expectedSnippet2 := `/dev/hidraw[0-9]{,[0-9],[0-9][0-9]} rw,`
	apparmorSpec = &apparmor.Specification{}
	err = apparmorSpec.AddConnectedPlug(s.iface, s.testPlugPort1, nil, s.testUDev1, nil)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.client-snap.app-accessing-2-devices"})
	snippet = apparmorSpec.SnippetForTag("snap.client-snap.app-accessing-2-devices")
	c.Assert(snippet, DeepEquals, expectedSnippet2)

	expectedSnippet3 := `/dev/hidraw[0-9]{,[0-9],[0-9][0-9]} rw,`
	apparmorSpec = &apparmor.Specification{}
	err = apparmorSpec.AddConnectedPlug(s.iface, s.testPlugPort2, nil, s.testUDev2, nil)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.client-snap.app-accessing-2-devices"})
	snippet = apparmorSpec.SnippetForTag("snap.client-snap.app-accessing-2-devices")
	c.Assert(snippet, DeepEquals, expectedSnippet3)
}

func (s *HidrawInterfaceSuite) TestConnectedPlugUDevSnippetsForPath(c *C) {
	expectedSnippet1 := `# hidraw
SUBSYSTEM=="hidraw", KERNEL=="hidraw0", TAG+="snap_client-snap_app-accessing-2-devices"`
	udevSpec := &udev.Specification{}
	err := udevSpec.AddConnectedPlug(s.iface, s.testPlugPort1, nil, s.testSlot1, nil)
	c.Assert(err, IsNil)
	c.Assert(udevSpec.Snippets(), HasLen, 1)
	snippet := udevSpec.Snippets()[0]
	c.Assert(snippet, Equals, expectedSnippet1)

	expectedSnippet2 := `# hidraw
IMPORT{builtin}="usb_id"
SUBSYSTEM=="hidraw", SUBSYSTEMS=="usb", ATTRS{idVendor}=="0001", ATTRS{idProduct}=="0001", TAG+="snap_client-snap_app-accessing-2-devices"`
	udevSpec = &udev.Specification{}
	err = udevSpec.AddConnectedPlug(s.iface, s.testPlugPort1, nil, s.testUDev1, nil)
	c.Assert(err, IsNil)
	c.Assert(udevSpec.Snippets(), HasLen, 1)
	snippet = udevSpec.Snippets()[0]
	c.Assert(snippet, Equals, expectedSnippet2)

	expectedSnippet3 := `# hidraw
IMPORT{builtin}="usb_id"
SUBSYSTEM=="hidraw", SUBSYSTEMS=="usb", ATTRS{idVendor}=="ffff", ATTRS{idProduct}=="ffff", TAG+="snap_client-snap_app-accessing-2-devices"`
	udevSpec = &udev.Specification{}
	err = udevSpec.AddConnectedPlug(s.iface, s.testPlugPort2, nil, s.testUDev2, nil)
	c.Assert(err, IsNil)
	c.Assert(udevSpec.Snippets(), HasLen, 1)
	snippet = udevSpec.Snippets()[0]
	c.Assert(snippet, Equals, expectedSnippet3)
}

func (s *HidrawInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
