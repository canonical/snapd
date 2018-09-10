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
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type HidrawInterfaceSuite struct {
	testutil.BaseTest
	iface interfaces.Interface

	// OS Snap
	testSlot1            *interfaces.ConnectedSlot
	testSlot1Info        *snap.SlotInfo
	testSlot2            *interfaces.ConnectedSlot
	testSlot2Info        *snap.SlotInfo
	missingPathSlot      *interfaces.ConnectedSlot
	missingPathSlotInfo  *snap.SlotInfo
	badPathSlot1         *interfaces.ConnectedSlot
	badPathSlot1Info     *snap.SlotInfo
	badPathSlot2         *interfaces.ConnectedSlot
	badPathSlot2Info     *snap.SlotInfo
	badPathSlot3         *interfaces.ConnectedSlot
	badPathSlot3Info     *snap.SlotInfo
	badInterfaceSlot     *interfaces.ConnectedSlot
	badInterfaceSlotInfo *snap.SlotInfo

	// Gadget Snap
	testUDev1             *interfaces.ConnectedSlot
	testUDev1Info         *snap.SlotInfo
	testUDev2             *interfaces.ConnectedSlot
	testUDev2Info         *snap.SlotInfo
	testUDevBadValue1     *interfaces.ConnectedSlot
	testUDevBadValue1Info *snap.SlotInfo
	testUDevBadValue2     *interfaces.ConnectedSlot
	testUDevBadValue2Info *snap.SlotInfo
	testUDevBadValue3     *interfaces.ConnectedSlot
	testUDevBadValue3Info *snap.SlotInfo

	// Consuming Snap
	testPlugPort1     *interfaces.ConnectedPlug
	testPlugPort1Info *snap.PlugInfo
	testPlugPort2     *interfaces.ConnectedPlug
	testPlugPort2Info *snap.PlugInfo
	testPlugPort3     *interfaces.ConnectedPlug
	testPlugPort3Info *snap.PlugInfo
}

var _ = Suite(&HidrawInterfaceSuite{
	iface: builtin.MustInterface("hidraw"),
})

func (s *HidrawInterfaceSuite) SetUpTest(c *C) {
	osSnapInfo := snaptest.MockInfo(c, `
name: ubuntu-core
version: 0
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
	s.testSlot1Info = osSnapInfo.Slots["test-port-1"]
	s.testSlot1 = interfaces.NewConnectedSlot(s.testSlot1Info, nil, nil)
	s.testSlot2Info = osSnapInfo.Slots["test-port-2"]
	s.testSlot2 = interfaces.NewConnectedSlot(s.testSlot2Info, nil, nil)
	s.missingPathSlotInfo = osSnapInfo.Slots["missing-path"]
	s.missingPathSlot = interfaces.NewConnectedSlot(s.missingPathSlotInfo, nil, nil)
	s.badPathSlot1Info = osSnapInfo.Slots["bad-path-1"]
	s.badPathSlot1 = interfaces.NewConnectedSlot(s.badPathSlot1Info, nil, nil)
	s.badPathSlot2Info = osSnapInfo.Slots["bad-path-2"]
	s.badPathSlot2 = interfaces.NewConnectedSlot(s.badPathSlot2Info, nil, nil)
	s.badPathSlot3Info = osSnapInfo.Slots["bad-path-3"]
	s.badPathSlot3 = interfaces.NewConnectedSlot(s.badPathSlot3Info, nil, nil)
	s.badInterfaceSlotInfo = osSnapInfo.Slots["bad-interface"]
	s.badInterfaceSlot = interfaces.NewConnectedSlot(s.badInterfaceSlotInfo, nil, nil)

	gadgetSnapInfo := snaptest.MockInfo(c, `
name: some-device
version: 0
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
	s.testUDev1Info = gadgetSnapInfo.Slots["test-udev-1"]
	s.testUDev1 = interfaces.NewConnectedSlot(s.testUDev1Info, nil, nil)
	s.testUDev2Info = gadgetSnapInfo.Slots["test-udev-2"]
	s.testUDev2 = interfaces.NewConnectedSlot(s.testUDev2Info, nil, nil)
	s.testUDevBadValue1Info = gadgetSnapInfo.Slots["test-udev-bad-value-1"]
	s.testUDevBadValue1 = interfaces.NewConnectedSlot(s.testUDevBadValue1Info, nil, nil)
	s.testUDevBadValue2Info = gadgetSnapInfo.Slots["test-udev-bad-value-2"]
	s.testUDevBadValue2 = interfaces.NewConnectedSlot(s.testUDevBadValue2Info, nil, nil)
	s.testUDevBadValue3Info = gadgetSnapInfo.Slots["test-udev-bad-value-3"]
	s.testUDevBadValue3 = interfaces.NewConnectedSlot(s.testUDevBadValue3Info, nil, nil)

	consumingSnapInfo := snaptest.MockInfo(c, `
name: client-snap
version: 0
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
	s.testPlugPort1Info = consumingSnapInfo.Plugs["plug-for-device-1"]
	s.testPlugPort1 = interfaces.NewConnectedPlug(s.testPlugPort1Info, nil, nil)
	s.testPlugPort2Info = consumingSnapInfo.Plugs["plug-for-device-2"]
	s.testPlugPort2 = interfaces.NewConnectedPlug(s.testPlugPort2Info, nil, nil)
	s.testPlugPort3Info = consumingSnapInfo.Plugs["plug-for-device-3"]
	s.testPlugPort3 = interfaces.NewConnectedPlug(s.testPlugPort3Info, nil, nil)
}

func (s *HidrawInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "hidraw")
}

func (s *HidrawInterfaceSuite) TestSanitizeCoreSnapSlots(c *C) {
	for _, slot := range []*snap.SlotInfo{s.testSlot1Info, s.testSlot2Info} {
		c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), IsNil)
	}
}

func (s *HidrawInterfaceSuite) TestSanitizeBadCoreSnapSlots(c *C) {
	// Slots without the "path" attribute are rejected.
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.missingPathSlotInfo), ErrorMatches,
		`hidraw slots must have a path attribute`)

	// Slots with incorrect value of the "path" attribute are rejected.
	for _, slot := range []*snap.SlotInfo{s.badPathSlot1Info, s.badPathSlot2Info, s.badPathSlot3Info} {
		c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches, "hidraw path attribute must be a valid device node")
	}
}

func (s *HidrawInterfaceSuite) TestSanitizeGadgetSnapSlots(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.testUDev1Info), IsNil)
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.testUDev2Info), IsNil)
}

func (s *HidrawInterfaceSuite) TestSanitizeBadGadgetSnapSlots(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.testUDevBadValue1Info), ErrorMatches, "hidraw usb-vendor attribute not valid: -1")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.testUDevBadValue2Info), ErrorMatches, "hidraw usb-product attribute not valid: 65536")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.testUDevBadValue3Info), ErrorMatches, "hidraw path attribute specifies invalid symlink location")
}

func (s *HidrawInterfaceSuite) TestPermanentSlotUDevSnippets(c *C) {
	spec := &udev.Specification{}
	for _, slot := range []*snap.SlotInfo{s.testSlot1Info, s.testSlot2Info} {
		c.Assert(spec.AddPermanentSlot(s.iface, slot), IsNil)
		c.Assert(spec.Snippets(), HasLen, 0)
	}

	expectedSnippet1 := `# hidraw
IMPORT{builtin}="usb_id"
SUBSYSTEM=="hidraw", SUBSYSTEMS=="usb", ATTRS{idVendor}=="0001", ATTRS{idProduct}=="0001", SYMLINK+="hidraw-canbus"`
	c.Assert(spec.AddPermanentSlot(s.iface, s.testUDev1Info), IsNil)
	c.Assert(spec.Snippets(), HasLen, 1)
	snippet := spec.Snippets()[0]
	c.Assert(snippet, Equals, expectedSnippet1)

	expectedSnippet2 := `# hidraw
IMPORT{builtin}="usb_id"
SUBSYSTEM=="hidraw", SUBSYSTEMS=="usb", ATTRS{idVendor}=="ffff", ATTRS{idProduct}=="ffff", SYMLINK+="hidraw-mydevice"`
	spec = &udev.Specification{}
	c.Assert(spec.AddPermanentSlot(s.iface, s.testUDev2Info), IsNil)
	c.Assert(spec.Snippets(), HasLen, 1)
	snippet = spec.Snippets()[0]
	c.Assert(snippet, Equals, expectedSnippet2)
}

func (s *HidrawInterfaceSuite) TestConnectedPlugUDevSnippets(c *C) {
	// add the plug for the slot with just path
	spec := &udev.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.testPlugPort1, s.testSlot1), IsNil)
	c.Assert(spec.Snippets(), HasLen, 2)
	snippet := spec.Snippets()[0]
	expectedSnippet1 := `# hidraw
SUBSYSTEM=="hidraw", KERNEL=="hidraw0", TAG+="snap_client-snap_app-accessing-2-devices"`
	c.Assert(snippet, Equals, expectedSnippet1)
	extraSnippet := spec.Snippets()[1]
	expectedExtraSnippet1 := `TAG=="snap_client-snap_app-accessing-2-devices", RUN+="/usr/lib/snapd/snap-device-helper $env{ACTION} snap_client-snap_app-accessing-2-devices $devpath $major:$minor"`
	c.Assert(extraSnippet, Equals, expectedExtraSnippet1)

	// add the plug for the first slot with vendor and product ids
	spec = &udev.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.testPlugPort1, s.testUDev1), IsNil)
	c.Assert(spec.Snippets(), HasLen, 2)
	snippet = spec.Snippets()[0]
	expectedSnippet2 := `# hidraw
IMPORT{builtin}="usb_id"
SUBSYSTEM=="hidraw", SUBSYSTEMS=="usb", ATTRS{idVendor}=="0001", ATTRS{idProduct}=="0001", TAG+="snap_client-snap_app-accessing-2-devices"`
	c.Assert(snippet, Equals, expectedSnippet2)
	extraSnippet = spec.Snippets()[1]
	expectedExtraSnippet2 := `TAG=="snap_client-snap_app-accessing-2-devices", RUN+="/usr/lib/snapd/snap-device-helper $env{ACTION} snap_client-snap_app-accessing-2-devices $devpath $major:$minor"`
	c.Assert(extraSnippet, Equals, expectedExtraSnippet2)

	// add the plug for the second slot with vendor and product ids
	spec = &udev.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.testPlugPort2, s.testUDev2), IsNil)
	c.Assert(spec.Snippets(), HasLen, 2)
	snippet = spec.Snippets()[0]
	expectedSnippet3 := `# hidraw
IMPORT{builtin}="usb_id"
SUBSYSTEM=="hidraw", SUBSYSTEMS=="usb", ATTRS{idVendor}=="ffff", ATTRS{idProduct}=="ffff", TAG+="snap_client-snap_app-accessing-2-devices"`
	c.Assert(snippet, Equals, expectedSnippet3)
	extraSnippet = spec.Snippets()[1]
	expectedExtraSnippet3 := `TAG=="snap_client-snap_app-accessing-2-devices", RUN+="/usr/lib/snapd/snap-device-helper $env{ACTION} snap_client-snap_app-accessing-2-devices $devpath $major:$minor"`
	c.Assert(extraSnippet, Equals, expectedExtraSnippet3)
}

func (s *HidrawInterfaceSuite) TestConnectedPlugAppArmorSnippets(c *C) {
	expectedSnippet1 := `/dev/hidraw0 rw,`
	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, s.testPlugPort1, s.testSlot1)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.client-snap.app-accessing-2-devices"})
	snippet := apparmorSpec.SnippetForTag("snap.client-snap.app-accessing-2-devices")
	c.Assert(snippet, DeepEquals, expectedSnippet1)

	expectedSnippet2 := `/dev/hidraw[0-9]{,[0-9],[0-9][0-9]} rw,`
	apparmorSpec = &apparmor.Specification{}
	err = apparmorSpec.AddConnectedPlug(s.iface, s.testPlugPort1, s.testUDev1)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.client-snap.app-accessing-2-devices"})
	snippet = apparmorSpec.SnippetForTag("snap.client-snap.app-accessing-2-devices")
	c.Assert(snippet, DeepEquals, expectedSnippet2)

	expectedSnippet3 := `/dev/hidraw[0-9]{,[0-9],[0-9][0-9]} rw,`
	apparmorSpec = &apparmor.Specification{}
	err = apparmorSpec.AddConnectedPlug(s.iface, s.testPlugPort2, s.testUDev2)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.client-snap.app-accessing-2-devices"})
	snippet = apparmorSpec.SnippetForTag("snap.client-snap.app-accessing-2-devices")
	c.Assert(snippet, DeepEquals, expectedSnippet3)
}

func (s *HidrawInterfaceSuite) TestConnectedPlugUDevSnippetsForPath(c *C) {
	expectedSnippet1 := `# hidraw
SUBSYSTEM=="hidraw", KERNEL=="hidraw0", TAG+="snap_client-snap_app-accessing-2-devices"`
	expectedExtraSnippet1 := `TAG=="snap_client-snap_app-accessing-2-devices", RUN+="/usr/lib/snapd/snap-device-helper $env{ACTION} snap_client-snap_app-accessing-2-devices $devpath $major:$minor"`
	udevSpec := &udev.Specification{}
	err := udevSpec.AddConnectedPlug(s.iface, s.testPlugPort1, s.testSlot1)
	c.Assert(err, IsNil)
	c.Assert(udevSpec.Snippets(), HasLen, 2)
	snippet := udevSpec.Snippets()[0]
	c.Assert(snippet, Equals, expectedSnippet1)
	extraSnippet := udevSpec.Snippets()[1]
	c.Assert(extraSnippet, Equals, expectedExtraSnippet1)

	expectedSnippet2 := `# hidraw
IMPORT{builtin}="usb_id"
SUBSYSTEM=="hidraw", SUBSYSTEMS=="usb", ATTRS{idVendor}=="0001", ATTRS{idProduct}=="0001", TAG+="snap_client-snap_app-accessing-2-devices"`
	expectedExtraSnippet2 := `TAG=="snap_client-snap_app-accessing-2-devices", RUN+="/usr/lib/snapd/snap-device-helper $env{ACTION} snap_client-snap_app-accessing-2-devices $devpath $major:$minor"`
	udevSpec = &udev.Specification{}
	err = udevSpec.AddConnectedPlug(s.iface, s.testPlugPort1, s.testUDev1)
	c.Assert(err, IsNil)
	c.Assert(udevSpec.Snippets(), HasLen, 2)
	snippet = udevSpec.Snippets()[0]
	c.Assert(snippet, Equals, expectedSnippet2)
	extraSnippet = udevSpec.Snippets()[1]
	c.Assert(extraSnippet, Equals, expectedExtraSnippet2)

	expectedSnippet3 := `# hidraw
IMPORT{builtin}="usb_id"
SUBSYSTEM=="hidraw", SUBSYSTEMS=="usb", ATTRS{idVendor}=="ffff", ATTRS{idProduct}=="ffff", TAG+="snap_client-snap_app-accessing-2-devices"`
	expectedExtraSnippet3 := `TAG=="snap_client-snap_app-accessing-2-devices", RUN+="/usr/lib/snapd/snap-device-helper $env{ACTION} snap_client-snap_app-accessing-2-devices $devpath $major:$minor"`
	udevSpec = &udev.Specification{}
	err = udevSpec.AddConnectedPlug(s.iface, s.testPlugPort2, s.testUDev2)
	c.Assert(err, IsNil)
	c.Assert(udevSpec.Snippets(), HasLen, 2)
	snippet = udevSpec.Snippets()[0]
	c.Assert(snippet, Equals, expectedSnippet3)
	extraSnippet = udevSpec.Snippets()[1]
	c.Assert(extraSnippet, Equals, expectedExtraSnippet3)
}

func (s *HidrawInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
