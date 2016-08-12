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
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type SerialPortInterfaceSuite struct {
	testutil.BaseTest
	iface            interfaces.Interface
	testSlot1        *interfaces.Slot
	testSlot2        *interfaces.Slot
	testSlot3        *interfaces.Slot
	testSlot4        *interfaces.Slot
	missingPathSlot  *interfaces.Slot
	badPathSlot1     *interfaces.Slot
	badPathSlot2     *interfaces.Slot
	badPathSlot3     *interfaces.Slot
	badInterfaceSlot *interfaces.Slot
	udevPlug1        *interfaces.Plug
	pathPlug1        *interfaces.Plug
	pathPlug2        *interfaces.Plug
	pathPlug3        *interfaces.Plug
	pathPlug4        *interfaces.Plug
	emptyBadPlug1    *interfaces.Plug
	udevBadPlug1     *interfaces.Plug
	udevBadPlug2     *interfaces.Plug
	udevBadPlug3     *interfaces.Plug
	udevBadPlug4     *interfaces.Plug
	badInterfacePlug *interfaces.Plug
	osSlot           *interfaces.Slot
}

var _ = Suite(&SerialPortInterfaceSuite{
	iface: &builtin.SerialPortInterface{},
})

func (s *SerialPortInterfaceSuite) SetUpTest(c *C) {
	info, err := snap.InfoFromSnapYaml([]byte(`
name: my-snap
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
plugs:
    test-udev-1:
        interface: serial-port
        vendor-id: "0000"
        product-id: "aaaa"
    test-plug-1:
        interface: serial-port
        path: /dev/ttyS0
    test-plug-2:
        interface: serial-port
        path: /dev/ttyAMA2
    test-plug-3:
        interface: serial-port
        path: /dev/ttyUSB927
    test-plug-4:
        interface: serial-port
        path: /dev/ttyS42
    bad-empty-plug: serial-port
    bad-udev-attrs-1:
        interface: serial-port
        product-id: "1111"
    bad-udev-attrs-2:
        interface: serial-port
        vendor-id: "1111"
    bad-udev-attrs-3:
        interface: serial-port
        product-id: "1"
        vendor-id: "abcd"
    bad-udev-attrs-4:
        interface: serial-port
        product-id: "1234"
        vendor-id: "adc"
    bad-interface: other-interface
`))
	c.Assert(err, IsNil)
	// Slots
	s.testSlot1 = &interfaces.Slot{SlotInfo: info.Slots["test-port-1"]}
	s.testSlot2 = &interfaces.Slot{SlotInfo: info.Slots["test-port-2"]}
	s.testSlot3 = &interfaces.Slot{SlotInfo: info.Slots["test-port-3"]}
	s.testSlot4 = &interfaces.Slot{SlotInfo: info.Slots["test-port-4"]}
	s.missingPathSlot = &interfaces.Slot{SlotInfo: info.Slots["missing-path"]}
	s.badPathSlot1 = &interfaces.Slot{SlotInfo: info.Slots["bad-path-1"]}
	s.badPathSlot2 = &interfaces.Slot{SlotInfo: info.Slots["bad-path-2"]}
	s.badPathSlot3 = &interfaces.Slot{SlotInfo: info.Slots["bad-path-3"]}
	s.badInterfaceSlot = &interfaces.Slot{SlotInfo: info.Slots["bad-interface"]}

	// Plugs
	s.udevPlug1 = &interfaces.Plug{PlugInfo: info.Plugs["test-udev-1"]}
	s.pathPlug1 = &interfaces.Plug{PlugInfo: info.Plugs["test-plug-1"]}
	s.pathPlug2 = &interfaces.Plug{PlugInfo: info.Plugs["test-plug-2"]}
	s.pathPlug3 = &interfaces.Plug{PlugInfo: info.Plugs["test-plug-3"]}
	s.pathPlug4 = &interfaces.Plug{PlugInfo: info.Plugs["test-plug-4"]}
	s.emptyBadPlug1 = &interfaces.Plug{PlugInfo: info.Plugs["bad-empty-plug"]}
	s.udevBadPlug1 = &interfaces.Plug{PlugInfo: info.Plugs["bad-udev-attrs-1"]}
	s.udevBadPlug2 = &interfaces.Plug{PlugInfo: info.Plugs["bad-udev-attrs-2"]}
	s.udevBadPlug3 = &interfaces.Plug{PlugInfo: info.Plugs["bad-udev-attrs-3"]}
	s.udevBadPlug4 = &interfaces.Plug{PlugInfo: info.Plugs["bad-udev-attrs-4"]}
	s.badInterfacePlug = &interfaces.Plug{PlugInfo: info.Plugs["bad-interface"]}

	osInfo, osErr := snap.InfoFromSnapYaml([]byte(`
name: my-core
type: os
slots:
    slot: serial-port
`))
	c.Assert(osErr, IsNil)
	s.osSlot = &interfaces.Slot{SlotInfo: osInfo.Slots["slot"]}
}

func (s *SerialPortInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "serial-port")
}

func (s *SerialPortInterfaceSuite) TestSanitizePathSlot(c *C) {
	// Test good slot examples
	for _, slot := range []*interfaces.Slot{s.testSlot1, s.testSlot2, s.testSlot3, s.testSlot4} {
		err := s.iface.SanitizeSlot(slot)
		c.Assert(err, IsNil)
	}

	// Slots without the "path" attribute are rejected.
	err := s.iface.SanitizeSlot(s.missingPathSlot)
	c.Assert(err, ErrorMatches, `serial-port slot does not have "path" attribute`)

	// Slots with incorrect value of the "path" attribute are rejected.
	for _, slot := range []*interfaces.Slot{s.badPathSlot1, s.badPathSlot2, s.badPathSlot3} {
		err := s.iface.SanitizeSlot(slot)
		c.Assert(err, ErrorMatches, "serial-port path attribute must be a valid device node")
	}

	// It is impossible to use "bool-file" interface to sanitize slots with other interfaces.
	c.Assert(func() { s.iface.SanitizeSlot(s.badInterfaceSlot) }, PanicMatches, `slot is not of interface "serial-port"`)
}

func (s *SerialPortInterfaceSuite) TestSanitizeCoreSlot(c *C) {
	err := s.iface.SanitizeSlot(s.osSlot)
	c.Assert(err, IsNil)
}

func (s *SerialPortInterfaceSuite) TestSanitizeGoodPlugs(c *C) {
	for _, plug := range []*interfaces.Plug{s.udevPlug1, s.pathPlug1, s.pathPlug2, s.pathPlug3, s.pathPlug4} {
		err := s.iface.SanitizePlug(plug)
		c.Assert(err, IsNil)
	}
}

func (s *SerialPortInterfaceSuite) TestSanitizeBadPlugs(c *C) {
	err := s.iface.SanitizePlug(s.udevBadPlug1)
	c.Assert(err, ErrorMatches, `serial-port plug found one attribute but it was not "path"`)

	err = s.iface.SanitizePlug(s.udevBadPlug2)
	c.Assert(err, ErrorMatches, `serial-port plug found one attribute but it was not "path"`)

	err = s.iface.SanitizePlug(s.udevBadPlug3)
	c.Assert(err, ErrorMatches, `serial-port product-id attribute not valid: 1`)

	err = s.iface.SanitizePlug(s.udevBadPlug4)
	c.Assert(err, ErrorMatches, `serial-port vendor-id attribute not valid: adc`)

	// It is impossible to use "bool-file" interface to sanitize plugs of different interface.
	c.Assert(func() { s.iface.SanitizePlug(s.badInterfacePlug) }, PanicMatches, `plug is not of interface "serial-port"`)
}

func (s *SerialPortInterfaceSuite) TestConnectedPathPlugSnippetUnusedSecuritySystems(c *C) {
	// No extra seccomp permissions for plug
	snippet, err := s.iface.ConnectedPlugSnippet(s.pathPlug1, s.testSlot1, interfaces.SecuritySecComp)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
	// No extra dbus permissions for plug
	snippet, err = s.iface.ConnectedPlugSnippet(s.pathPlug1, s.testSlot1, interfaces.SecurityDBus)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
	// No extra udev permissions for plug
	snippet, err = s.iface.ConnectedPlugSnippet(s.pathPlug1, s.testSlot1, interfaces.SecurityUDev)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
	// No extra mount permissions for plug
	snippet, err = s.iface.ConnectedPlugSnippet(s.pathPlug1, s.testSlot1, interfaces.SecurityMount)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
	// Other security types are not recognized
	snippet, err = s.iface.ConnectedPlugSnippet(s.pathPlug1, s.testSlot1, "foo")
	c.Assert(err, ErrorMatches, `unknown security system`)
	c.Assert(snippet, IsNil)
}

func (s *SerialPortInterfaceSuite) TestConnectedUdevPlugSnippetGivesExtraPermissions(c *C) {
	expectedPlugSnippet1 := []byte(`/dev/tty* rw,
`)
	snippet, err := s.iface.ConnectedPlugSnippet(s.udevPlug1, s.osSlot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, DeepEquals, expectedPlugSnippet1, Commentf("\nexpected:\n%s\nfound:\n%s", expectedPlugSnippet1, snippet))
}

func (s *SerialPortInterfaceSuite) TestPermanentPlugSnippetUnusedSecuritySystems(c *C) {
	// No extra apparmor permissions for plug
	snippet, err := s.iface.PermanentPlugSnippet(s.pathPlug1, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
	// No extra seccomp permissions for plug
	snippet, err = s.iface.PermanentPlugSnippet(s.pathPlug1, interfaces.SecuritySecComp)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
	// No extra dbus permissions for plug
	snippet, err = s.iface.PermanentPlugSnippet(s.pathPlug1, interfaces.SecurityDBus)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
	// No extra udev permissions for plug
	snippet, err = s.iface.PermanentPlugSnippet(s.pathPlug1, interfaces.SecurityUDev)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
	// No extra mount permissions for plug
	snippet, err = s.iface.PermanentPlugSnippet(s.pathPlug1, interfaces.SecurityMount)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
	// Other security types are not recognized
	snippet, err = s.iface.PermanentPlugSnippet(s.pathPlug1, "foo")
	c.Assert(err, ErrorMatches, `unknown security system`)
	c.Assert(snippet, IsNil)
}

func (s *SerialPortInterfaceSuite) TestConnectedEmptyPlugSnippetGivesExtraPermissions(c *C) {
	// slot snippet 1
	expectedPlugSnippet1 := []byte(`/dev/ttyS0 rwk,
`)
	snippet, err := s.iface.ConnectedPlugSnippet(s.pathPlug1, s.testSlot1, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, DeepEquals, expectedPlugSnippet1, Commentf("\nexpected:\n%s\nfound:\n%s", expectedPlugSnippet1, snippet))
	// slot snippet 2
	expectedPlugSnippet2 := []byte(`/dev/ttyAMA2 rwk,
`)
	snippet, err = s.iface.ConnectedPlugSnippet(s.pathPlug2, s.testSlot2, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, DeepEquals, expectedPlugSnippet2, Commentf("\nexpected:\n%s\nfound:\n%s", expectedPlugSnippet2, snippet))
	// slot snippet 3
	expectedPlugSnippet3 := []byte(`/dev/ttyUSB927 rwk,
`)
	snippet, err = s.iface.ConnectedPlugSnippet(s.pathPlug3, s.testSlot3, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, DeepEquals, expectedPlugSnippet3, Commentf("\nexpected:\n%s\nfound:\n%s", expectedPlugSnippet3, snippet))
	// slot snippet 4
	expectedPlugSnippet4 := []byte(`/dev/ttyS42 rwk,
`)
	snippet, err = s.iface.ConnectedPlugSnippet(s.pathPlug4, s.testSlot4, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, DeepEquals, expectedPlugSnippet4, Commentf("\nexpected:\n%s\nfound:\n%s", expectedPlugSnippet4, snippet))
}

func (s *SerialPortInterfaceSuite) TestConnectedSlotSnippetUnusedSecuritySystems(c *C) {
	// No extra apparmor permissions for slot
	snippet, err := s.iface.ConnectedSlotSnippet(s.pathPlug1, s.testSlot1, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
	// No extra seccomp permissions for slot
	snippet, err = s.iface.ConnectedSlotSnippet(s.pathPlug1, s.testSlot1, interfaces.SecuritySecComp)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
	// No extra dbus permissions for slot
	snippet, err = s.iface.ConnectedSlotSnippet(s.pathPlug1, s.testSlot1, interfaces.SecurityDBus)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
	// No extra udev permissions for slot
	snippet, err = s.iface.ConnectedSlotSnippet(s.pathPlug1, s.testSlot1, interfaces.SecurityUDev)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
	// No extra mount permissions for slot
	snippet, err = s.iface.ConnectedSlotSnippet(s.pathPlug1, s.testSlot1, interfaces.SecurityMount)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
	// Other security types are not recognized
	snippet, err = s.iface.ConnectedSlotSnippet(s.pathPlug1, s.testSlot1, "foo")
	c.Assert(err, ErrorMatches, `unknown security system`)
	c.Assert(snippet, IsNil)
}

func (s *SerialPortInterfaceSuite) TestPermanentSlotSnippetUnusedSecuritySystems(c *C) {
	for _, slot := range []*interfaces.Slot{s.testSlot1, s.testSlot2, s.testSlot3, s.testSlot4} {
		// No extra apparmor permissions for slot
		snippet, err := s.iface.PermanentSlotSnippet(slot, interfaces.SecurityAppArmor)
		c.Assert(err, IsNil)
		c.Assert(snippet, IsNil)
		// No extra seccomp permissions for slot
		snippet, err = s.iface.PermanentSlotSnippet(slot, interfaces.SecuritySecComp)
		c.Assert(err, IsNil)
		c.Assert(snippet, IsNil)
		// No extra dbus permissions for slot
		snippet, err = s.iface.PermanentSlotSnippet(slot, interfaces.SecurityDBus)
		c.Assert(err, IsNil)
		c.Assert(snippet, IsNil)
		// No extra udev permissions for slot
		snippet, err = s.iface.PermanentSlotSnippet(slot, interfaces.SecurityUDev)
		c.Assert(err, IsNil)
		c.Assert(snippet, IsNil)
		// No extra mount permissions for slot
		snippet, err = s.iface.PermanentSlotSnippet(slot, interfaces.SecurityMount)
		c.Assert(err, IsNil)
		c.Assert(snippet, IsNil)
		// Other security types are not recognized
		snippet, err = s.iface.PermanentSlotSnippet(slot, "foo")
		c.Assert(err, ErrorMatches, `unknown security system`)
		c.Assert(snippet, IsNil)
	}
}

func (s *SerialPortInterfaceSuite) TestAutoConnect(c *C) {
	c.Check(s.iface.AutoConnect(), Equals, false)
}
