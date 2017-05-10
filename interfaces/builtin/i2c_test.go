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

type I2cInterfaceSuite struct {
	testutil.BaseTest
	iface interfaces.Interface

	// OS Snap
	testSlot1 *interfaces.Slot

	// Gadget Snap
	testUDev1             *interfaces.Slot
	testUDev2             *interfaces.Slot
	testUDev3             *interfaces.Slot
	testUDevBadValue1     *interfaces.Slot
	testUDevBadValue2     *interfaces.Slot
	testUDevBadValue3     *interfaces.Slot
	testUDevBadValue4     *interfaces.Slot
	testUDevBadValue5     *interfaces.Slot
	testUDevBadValue6     *interfaces.Slot
	testUDevBadValue7     *interfaces.Slot
	testUDevBadInterface1 *interfaces.Slot

	// Consuming Snap
	testPlugPort1 *interfaces.Plug
}

var _ = Suite(&I2cInterfaceSuite{
	iface: &builtin.I2cInterface{},
})

func (s *I2cInterfaceSuite) SetUpTest(c *C) {
	// Mock for OS Snap
	osSnapInfo := snaptest.MockInfo(c, `
name: ubuntu-core
type: os
slots:
  test-port-1:
    interface: i2c
    path: /dev/i2c-0
`, nil)
	s.testSlot1 = &interfaces.Slot{SlotInfo: osSnapInfo.Slots["test-port-1"]}

	// Mock for Gadget Snap
	gadgetSnapInfo := snaptest.MockInfo(c, `
name: some-device
type: gadget
slots:
  test-udev-1:
    interface: i2c
    path: /dev/i2c-1
  test-udev-2:
    interface: i2c
    path: /dev/i2c-11
  test-udev-3:
    interface: i2c
    path: /dev/i2c-0
  test-udev-bad-value-1:
    interface: i2c
    path: /dev/i2c
  test-udev-bad-value-2:
    interface: i2c
    path: /dev/i2c-a
  test-udev-bad-value-3:
    interface: i2c
    path: /dev/i2c-2a
  test-udev-bad-value-4:
    interface: i2c
    path: /dev/foo-0
  test-udev-bad-value-5:
    interface: i2c
    path: /dev/i2c-foo
  test-udev-bad-value-6:
    interface: i2c
    path: ""
  test-udev-bad-value-7:
    interface: i2c
  test-udev-bad-interface-1:
    interface: other-interface
`, nil)
	s.testUDev1 = &interfaces.Slot{SlotInfo: gadgetSnapInfo.Slots["test-udev-1"]}
	s.testUDev2 = &interfaces.Slot{SlotInfo: gadgetSnapInfo.Slots["test-udev-2"]}
	s.testUDev3 = &interfaces.Slot{SlotInfo: gadgetSnapInfo.Slots["test-udev-3"]}
	s.testUDevBadValue1 = &interfaces.Slot{SlotInfo: gadgetSnapInfo.Slots["test-udev-bad-value-1"]}
	s.testUDevBadValue2 = &interfaces.Slot{SlotInfo: gadgetSnapInfo.Slots["test-udev-bad-value-2"]}
	s.testUDevBadValue3 = &interfaces.Slot{SlotInfo: gadgetSnapInfo.Slots["test-udev-bad-value-3"]}
	s.testUDevBadValue4 = &interfaces.Slot{SlotInfo: gadgetSnapInfo.Slots["test-udev-bad-value-4"]}
	s.testUDevBadValue5 = &interfaces.Slot{SlotInfo: gadgetSnapInfo.Slots["test-udev-bad-value-5"]}
	s.testUDevBadValue6 = &interfaces.Slot{SlotInfo: gadgetSnapInfo.Slots["test-udev-bad-value-6"]}
	s.testUDevBadValue7 = &interfaces.Slot{SlotInfo: gadgetSnapInfo.Slots["test-udev-bad-value-7"]}
	s.testUDevBadInterface1 = &interfaces.Slot{SlotInfo: gadgetSnapInfo.Slots["test-udev-bad-interface-1"]}

	// Snap Consumers
	consumingSnapInfo := snaptest.MockInfo(c, `
name: client-snap
plugs:
  plug-for-port-1:
    interface: i2c
apps:
  app-accessing-1-port:
    command: foo
    plugs: [i2c]
`, nil)
	s.testPlugPort1 = &interfaces.Plug{PlugInfo: consumingSnapInfo.Plugs["plug-for-port-1"]}
}

func (s *I2cInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "i2c")
}

func (s *I2cInterfaceSuite) TestSanitizeCoreSnapSlot(c *C) {
	err := s.iface.SanitizeSlot(s.testSlot1)
	c.Assert(err, IsNil)
}

func (s *I2cInterfaceSuite) TestSanitizeGadgetSnapSlot(c *C) {

	err := s.iface.SanitizeSlot(s.testUDev1)
	c.Assert(err, IsNil)

	err = s.iface.SanitizeSlot(s.testUDev2)
	c.Assert(err, IsNil)

	err = s.iface.SanitizeSlot(s.testUDev3)
	c.Assert(err, IsNil)
}

func (s *I2cInterfaceSuite) TestSanitizeBadGadgetSnapSlot(c *C) {

	err := s.iface.SanitizeSlot(s.testUDevBadValue1)
	c.Assert(err, ErrorMatches, "i2c path attribute must be a valid device node")

	err = s.iface.SanitizeSlot(s.testUDevBadValue2)
	c.Assert(err, ErrorMatches, "i2c path attribute must be a valid device node")

	err = s.iface.SanitizeSlot(s.testUDevBadValue3)
	c.Assert(err, ErrorMatches, "i2c path attribute must be a valid device node")

	err = s.iface.SanitizeSlot(s.testUDevBadValue4)
	c.Assert(err, ErrorMatches, "i2c path attribute must be a valid device node")

	err = s.iface.SanitizeSlot(s.testUDevBadValue5)
	c.Assert(err, ErrorMatches, "i2c path attribute must be a valid device node")

	err = s.iface.SanitizeSlot(s.testUDevBadValue6)
	c.Assert(err, ErrorMatches, "i2c slot must have a path attribute")

	err = s.iface.SanitizeSlot(s.testUDevBadValue7)
	c.Assert(err, ErrorMatches, "i2c slot must have a path attribute")

	c.Assert(func() { s.iface.SanitizeSlot(s.testUDevBadInterface1) }, PanicMatches, `slot is not of interface "i2c"`)
}

func (s *I2cInterfaceSuite) TestConnectedPlugUDevSnippets(c *C) {
	expectedSnippet1 := `KERNEL=="i2c-1", TAG+="snap_client-snap_app-accessing-1-port"`

	spec := &udev.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.testPlugPort1, nil, s.testUDev1, nil), IsNil)
	c.Assert(spec.Snippets(), HasLen, 1)
	snippet := spec.Snippets()[0]
	c.Assert(snippet, Equals, expectedSnippet1)
}

func (s *I2cInterfaceSuite) TestConnectedPlugAppArmorSnippets(c *C) {
	expectedSnippet1 := `/dev/i2c-1 rw,
/sys/devices/platform/**.i2c/i2c-1/** rw,`
	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, s.testPlugPort1, nil, s.testUDev1, nil)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.client-snap.app-accessing-1-port"})
	snippet := apparmorSpec.SnippetForTag("snap.client-snap.app-accessing-1-port")
	c.Assert(snippet, DeepEquals, expectedSnippet1, Commentf("\nexpected:\n%s\nfound:\n%s", expectedSnippet1, snippet))
}

func (s *I2cInterfaceSuite) TestAutoConnect(c *C) {
	c.Check(s.iface.AutoConnect(nil, nil), Equals, true)
}
