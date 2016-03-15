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
	"bytes"
	"fmt"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/interfaces"
	"github.com/ubuntu-core/snappy/interfaces/builtin"
	"github.com/ubuntu-core/snappy/testutil"
)

func Test(t *testing.T) {
	TestingT(t)
}

type BoolFileInterfaceSuite struct {
	testutil.BaseTest
	iface             interfaces.Interface
	gpioSlot          *interfaces.Slot
	ledSlot           *interfaces.Slot
	badPathSlot       *interfaces.Slot
	parentDirPathSlot *interfaces.Slot
	missingPathSlot   *interfaces.Slot
	badInterfaceSlot  *interfaces.Slot
	plug              *interfaces.Plug
	badInterfacePlug  *interfaces.Plug
}

var _ = Suite(&BoolFileInterfaceSuite{
	iface: &builtin.BoolFileInterface{},
	gpioSlot: &interfaces.Slot{
		Interface: "bool-file",
		Attrs: map[string]interface{}{
			"path": "/sys/class/gpio/gpio13/value",
		},
	},
	ledSlot: &interfaces.Slot{
		Interface: "bool-file",
		Attrs: map[string]interface{}{
			"path": "/sys/class/leds/input27::capslock/brightness",
		},
	},
	missingPathSlot: &interfaces.Slot{
		Interface: "bool-file",
	},
	badPathSlot: &interfaces.Slot{
		Interface: "bool-file",
		Attrs:     map[string]interface{}{"path": "path"},
	},
	parentDirPathSlot: &interfaces.Slot{
		Interface: "bool-file",
		Attrs: map[string]interface{}{
			"path": "/sys/class/gpio/../value",
		},
	},
	badInterfaceSlot: &interfaces.Slot{
		Interface: "other-interface",
	},
	plug: &interfaces.Plug{
		Interface: "bool-file",
	},
	badInterfacePlug: &interfaces.Plug{
		Interface: "other-interface",
	},
})

func (s *BoolFileInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "bool-file")
}

func (s *BoolFileInterfaceSuite) TestSanitizeSlot(c *C) {
	// Both LED and GPIO slots are accepted
	err := s.iface.SanitizeSlot(s.ledSlot)
	c.Assert(err, IsNil)
	err = s.iface.SanitizeSlot(s.gpioSlot)
	c.Assert(err, IsNil)
	// Slots without the "path" attribute are rejected.
	err = s.iface.SanitizeSlot(s.missingPathSlot)
	c.Assert(err, ErrorMatches,
		"bool-file must contain the path attribute")
	// Slots without the "path" attribute are rejected.
	err = s.iface.SanitizeSlot(s.parentDirPathSlot)
	c.Assert(err, ErrorMatches,
		"bool-file can only point at LED brightness or GPIO value")
	// Slots with incorrect value of the "path" attribute are rejected.
	err = s.iface.SanitizeSlot(s.badPathSlot)
	c.Assert(err, ErrorMatches,
		"bool-file can only point at LED brightness or GPIO value")
	// It is impossible to use "bool-file" interface to sanitize slots with other interfaces.
	c.Assert(func() { s.iface.SanitizeSlot(s.badInterfaceSlot) }, PanicMatches,
		`slot is not of interface "bool-file"`)
}

func (s *BoolFileInterfaceSuite) TestSanitizePlug(c *C) {
	err := s.iface.SanitizePlug(s.plug)
	c.Assert(err, IsNil)
	// It is impossible to use "bool-file" interface to sanitize plugs of different interface.
	c.Assert(func() { s.iface.SanitizePlug(s.badInterfacePlug) }, PanicMatches,
		`plug is not of interface "bool-file"`)
}

func (s *BoolFileInterfaceSuite) TestPlugSnippetHandlesSymlinkErrors(c *C) {
	// Symbolic link traversal is handled correctly
	builtin.MockEvalSymlinks(&s.BaseTest, func(path string) (string, error) {
		return "", fmt.Errorf("broken symbolic link")
	})
	snippet, err := s.iface.ConnectedPlugSnippet(s.plug, s.gpioSlot, interfaces.SecurityAppArmor)
	c.Assert(err, ErrorMatches, "cannot compute plug security snippet: broken symbolic link")
	c.Assert(snippet, IsNil)
}

func (s *BoolFileInterfaceSuite) TestPlugSnippetDereferencesSymlinks(c *C) {
	// Use a fake (successful) dereferencing function for the remainder of the test.
	builtin.MockEvalSymlinks(&s.BaseTest, func(path string) (string, error) {
		return "(dereferenced)" + path, nil
	})
	// Extra apparmor permission to access GPIO value
	// The path uses dereferenced symbolic links.
	snippet, err := s.iface.ConnectedPlugSnippet(s.plug, s.gpioSlot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, DeepEquals, []byte(
		"(dereferenced)/sys/class/gpio/gpio13/value rwk,\n"))
	// Extra apparmor permission to access LED brightness.
	// The path uses dereferenced symbolic links.
	snippet, err = s.iface.ConnectedPlugSnippet(s.plug, s.ledSlot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, DeepEquals, []byte(
		"(dereferenced)/sys/class/leds/input27::capslock/brightness rwk,\n"))
}

func (s *BoolFileInterfaceSuite) TestPermanentPlugSecurityDoesNotContainSlotSecurity(c *C) {
	// Use a fake (successful) dereferencing function for the remainder of the test.
	builtin.MockEvalSymlinks(&s.BaseTest, func(path string) (string, error) {
		return path, nil
	})
	var err error
	var slotSnippet, plugSnippet []byte
	plugSnippet, err = s.iface.PermanentPlugSnippet(s.plug, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	slotSnippet, err = s.iface.PermanentSlotSnippet(s.gpioSlot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	// Ensure that we don't accidentally give plug-side permissions to slot-side.
	c.Assert(bytes.Contains(plugSnippet, slotSnippet), Equals, false)
}

func (s *BoolFileInterfaceSuite) TestConnectedPlugSnippetPanicksOnUnsanitizedSlots(c *C) {
	// Unsanitized slots should never be used and cause a panic.
	c.Assert(func() {
		s.iface.ConnectedPlugSnippet(s.plug, s.missingPathSlot, interfaces.SecurityAppArmor)
	}, PanicMatches, "slot is not sanitized")
}

func (s *BoolFileInterfaceSuite) TestConnectedPlugSnippetUnusedSecuritySystems(c *C) {
	for _, slot := range []*interfaces.Slot{s.ledSlot, s.gpioSlot} {
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

func (s *BoolFileInterfaceSuite) TestPermanentPlugSnippetUnusedSecuritySystems(c *C) {
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

func (s *BoolFileInterfaceSuite) TestPermanentSlotSnippetGivesExtraPermissionsToConfigureGPIOs(c *C) {
	// Extra apparmor permission to provide GPIOs
	expectedGPIOSnippet := []byte(`
/sys/class/gpio/export rw,
/sys/class/gpio/unexport rw,
/sys/class/gpio/gpio[0-9]+/direction rw,
`)
	snippet, err := s.iface.PermanentSlotSnippet(s.gpioSlot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, DeepEquals, expectedGPIOSnippet)
}

func (s *BoolFileInterfaceSuite) TestPermanentSlotSnippetGivesNoExtraPermissionsToConfigureLEDs(c *C) {
	// No extra apparmor permission to provide LEDs
	snippet, err := s.iface.PermanentSlotSnippet(s.ledSlot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
}

func (s *BoolFileInterfaceSuite) TestPermanentSlotSnippetPanicksOnUnsanitizedSlots(c *C) {
	// Unsanitized slots should never be used and cause a panic.
	c.Assert(func() {
		s.iface.PermanentSlotSnippet(s.missingPathSlot, interfaces.SecurityAppArmor)
	}, PanicMatches, "slot is not sanitized")
}

func (s *BoolFileInterfaceSuite) TestConnectedSlotSnippetUnusedSecuritySystems(c *C) {
	for _, slot := range []*interfaces.Slot{s.ledSlot, s.gpioSlot} {
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

func (s *BoolFileInterfaceSuite) TestPermanentSlotSnippetUnusedSecuritySystems(c *C) {
	for _, slot := range []*interfaces.Slot{s.ledSlot, s.gpioSlot} {
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
