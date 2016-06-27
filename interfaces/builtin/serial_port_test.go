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
	plug             *interfaces.Plug
	badInterfacePlug *interfaces.Plug
}

var _ = Suite(&SerialPortInterfaceSuite{
	iface: &builtin.SerialPortInterface{},
})

func (s *SerialPortInterfaceSuite) SetUpTest(c *C) {
	info, err := snap.InfoFromSnapYaml([]byte(`
name: ubuntu-core
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
    plug: serial-port
    bad-interface: other-interface
`))
	c.Assert(err, IsNil)
	s.testSlot1 = &interfaces.Slot{SlotInfo: info.Slots["test-port-1"]}
	s.testSlot2 = &interfaces.Slot{SlotInfo: info.Slots["test-port-2"]}
	s.testSlot3 = &interfaces.Slot{SlotInfo: info.Slots["test-port-3"]}
	s.testSlot4 = &interfaces.Slot{SlotInfo: info.Slots["test-port-4"]}
	s.missingPathSlot = &interfaces.Slot{SlotInfo: info.Slots["missing-path"]}
	s.badPathSlot1 = &interfaces.Slot{SlotInfo: info.Slots["bad-path-1"]}
	s.badPathSlot2 = &interfaces.Slot{SlotInfo: info.Slots["bad-path-2"]}
	s.badPathSlot3 = &interfaces.Slot{SlotInfo: info.Slots["bad-path-3"]}
	s.badInterfaceSlot = &interfaces.Slot{SlotInfo: info.Slots["bad-interface"]}
	s.plug = &interfaces.Plug{PlugInfo: info.Plugs["plug"]}
	s.badInterfacePlug = &interfaces.Plug{PlugInfo: info.Plugs["bad-interface"]}
}

func (s *SerialPortInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "serial-port")
}

func (s *SerialPortInterfaceSuite) TestSanitizeSlot(c *C) {
	// Test good slot examples
	for _, slot := range []*interfaces.Slot{s.testSlot1, s.testSlot2, s.testSlot3, s.testSlot4} {
		err := s.iface.SanitizeSlot(slot)
		c.Assert(err, IsNil)
	}
	// Slots without the "path" attribute are rejected.
	err := s.iface.SanitizeSlot(s.missingPathSlot)
	c.Assert(err, ErrorMatches,
		"serial-port slot must have a path attribute")
	// Slots with incorrect value of the "path" attribute are rejected.
	for _, slot := range []*interfaces.Slot{s.badPathSlot1, s.badPathSlot2, s.badPathSlot3} {
		err := s.iface.SanitizeSlot(slot)
		c.Assert(err, ErrorMatches,
			"serial-port path attribute must be a valid device node")
	}
	// It is impossible to use "bool-file" interface to sanitize slots with other interfaces.
	c.Assert(func() { s.iface.SanitizeSlot(s.badInterfaceSlot) }, PanicMatches,
		`slot is not of interface "serial-port"`)
}

func (s *SerialPortInterfaceSuite) TestSanitizePlug(c *C) {
	err := s.iface.SanitizePlug(s.plug)
	c.Assert(err, IsNil)
	// It is impossible to use "bool-file" interface to sanitize plugs of different interface.
	c.Assert(func() { s.iface.SanitizePlug(s.badInterfacePlug) }, PanicMatches,
		`plug is not of interface "serial-port"`)
}

func (s *SerialPortInterfaceSuite) TestConnectedPlugSnippetPanicsOnUnsanitizedSlots(c *C) {
	// Unsanitized slots should never be used and cause a panic.
	c.Assert(func() {
		s.iface.ConnectedPlugSnippet(s.plug, s.missingPathSlot, interfaces.SecurityAppArmor)
	}, PanicMatches, "slot is not sanitized")
}

func (s *SerialPortInterfaceSuite) TestConnectedPlugSnippetUnusedSecuritySystems(c *C) {
	for _, slot := range []*interfaces.Slot{s.testSlot1, s.testSlot2, s.testSlot3, s.testSlot4} {
		// No extra seccomp permissions for plug
		snippet, err := s.iface.ConnectedPlugSnippet(s.plug, slot, interfaces.SecuritySecComp)
		c.Assert(err, IsNil)
		c.Assert(snippet, IsNil)
		// No extra dbus permissions for plug
		snippet, err = s.iface.ConnectedPlugSnippet(s.plug, slot, interfaces.SecurityDBus)
		c.Assert(err, IsNil)
		c.Assert(snippet, IsNil)
		// No extra udev permissions for plug
		snippet, err = s.iface.ConnectedPlugSnippet(s.plug, slot, interfaces.SecurityUDev)
		c.Assert(err, IsNil)
		c.Assert(snippet, IsNil)
		// No extra udev permissions for plug
		snippet, err = s.iface.ConnectedPlugSnippet(s.plug, slot, interfaces.SecurityUDev)
		c.Assert(err, IsNil)
		c.Assert(snippet, IsNil)
		// Other security types are not recognized
		snippet, err = s.iface.ConnectedPlugSnippet(s.plug, slot, "foo")
		c.Assert(err, ErrorMatches, `unknown security system`)
		c.Assert(snippet, IsNil)
	}
}

func (s *SerialPortInterfaceSuite) TestPermanentPlugSnippetUnusedSecuritySystems(c *C) {
	// No extra seccomp permissions for plug
	snippet, err := s.iface.PermanentPlugSnippet(s.plug, interfaces.SecuritySecComp)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
	// No extra dbus permissions for plug
	snippet, err = s.iface.PermanentPlugSnippet(s.plug, interfaces.SecurityDBus)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
	// No extra udev permissions for plug
	snippet, err = s.iface.PermanentPlugSnippet(s.plug, interfaces.SecurityUDev)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
	// No extra udev permissions for plug
	snippet, err = s.iface.PermanentPlugSnippet(s.plug, interfaces.SecurityUDev)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
	// Other security types are not recognized
	snippet, err = s.iface.PermanentPlugSnippet(s.plug, "foo")
	c.Assert(err, ErrorMatches, `unknown security system`)
	c.Assert(snippet, IsNil)
}

func (s *SerialPortInterfaceSuite) TestPermanentSlotSnippetGivesExtraPermissions(c *C) {
	// slot snippet 1
	expectedSlotSnippet1 := []byte(`
/dev/ttyS0 rwk,
`)
	snippet, err := s.iface.PermanentSlotSnippet(s.testSlot1, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, DeepEquals, expectedSlotSnippet1)
	// slot snippet 2
	expectedSlotSnippet2 := []byte(`
/dev/ttyAMA2 rwk,
`)
	snippet, err = s.iface.PermanentSlotSnippet(s.testSlot2, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, DeepEquals, expectedSlotSnippet2)
	// slot snippet 3
	expectedSlotSnippet3 := []byte(`
/dev/ttyUSB927 rwk,
`)
	snippet, err = s.iface.PermanentSlotSnippet(s.testSlot3, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, DeepEquals, expectedSlotSnippet3)
	// slot snippet 4
	expectedSlotSnippet4 := []byte(`
/dev/ttyS42 rwk,
`)
	snippet, err = s.iface.PermanentSlotSnippet(s.testSlot4, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, DeepEquals, expectedSlotSnippet4)
}

func (s *SerialPortInterfaceSuite) TestPermanentSlotSnippetPanicsOnUnsanitizedSlots(c *C) {
	// Unsanitized slots should never be used and cause a panic.
	c.Assert(func() {
		s.iface.PermanentSlotSnippet(s.missingPathSlot, interfaces.SecurityAppArmor)
	}, PanicMatches, "slot is not sanitized")
}

func (s *SerialPortInterfaceSuite) TestConnectedSlotSnippetUnusedSecuritySystems(c *C) {
	for _, slot := range []*interfaces.Slot{s.testSlot1, s.testSlot2, s.testSlot3, s.testSlot4} {
		// No extra seccomp permissions for slot
		snippet, err := s.iface.ConnectedSlotSnippet(s.plug, slot, interfaces.SecuritySecComp)
		c.Assert(err, IsNil)
		c.Assert(snippet, IsNil)
		// No extra dbus permissions for slot
		snippet, err = s.iface.ConnectedSlotSnippet(s.plug, slot, interfaces.SecurityDBus)
		c.Assert(err, IsNil)
		c.Assert(snippet, IsNil)
		// No extra udev permissions for slot
		snippet, err = s.iface.ConnectedSlotSnippet(s.plug, slot, interfaces.SecurityUDev)
		c.Assert(err, IsNil)
		c.Assert(snippet, IsNil)
		// Other security types are not recognized
		snippet, err = s.iface.ConnectedSlotSnippet(s.plug, slot, "foo")
		c.Assert(err, ErrorMatches, `unknown security system`)
		c.Assert(snippet, IsNil)
	}
}

func (s *SerialPortInterfaceSuite) TestPermanentSlotSnippetUnusedSecuritySystems(c *C) {
	for _, slot := range []*interfaces.Slot{s.testSlot1, s.testSlot2, s.testSlot3, s.testSlot4} {
		// No extra seccomp permissions for slot
		snippet, err := s.iface.PermanentSlotSnippet(slot, interfaces.SecuritySecComp)
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
		// Other security types are not recognized
		snippet, err = s.iface.PermanentSlotSnippet(slot, "foo")
		c.Assert(err, ErrorMatches, `unknown security system`)
		c.Assert(snippet, IsNil)
	}
}

func (s *SerialPortInterfaceSuite) TestAutoConnect(c *C) {
	c.Check(s.iface.AutoConnect(), Equals, false)
}
