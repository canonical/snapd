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
	gpioPlug          *interfaces.Plug
	ledPlug           *interfaces.Plug
	badPathPlug       *interfaces.Plug
	parentDirPathPlug *interfaces.Plug
	missingPathPlug   *interfaces.Plug
	badInterfacePlug  *interfaces.Plug
	slot              *interfaces.Slot
	badInterfaceSlot  *interfaces.Slot
}

var _ = Suite(&BoolFileInterfaceSuite{
	iface: &builtin.BoolFileInterface{},
	gpioPlug: &interfaces.Plug{
		Interface: "bool-file",
		Attrs: map[string]interface{}{
			"path": "/sys/class/gpio/gpio13/value",
		},
	},
	ledPlug: &interfaces.Plug{
		Interface: "bool-file",
		Attrs: map[string]interface{}{
			"path": "/sys/class/leds/input27::capslock/brightness",
		},
	},
	missingPathPlug: &interfaces.Plug{
		Interface: "bool-file",
	},
	badPathPlug: &interfaces.Plug{
		Interface: "bool-file",
		Attrs:     map[string]interface{}{"path": "path"},
	},
	parentDirPathPlug: &interfaces.Plug{
		Interface: "bool-file",
		Attrs: map[string]interface{}{
			"path": "/sys/class/gpio/../value",
		},
	},
	badInterfacePlug: &interfaces.Plug{
		Interface: "other-interface",
	},
	slot: &interfaces.Slot{
		Interface: "bool-file",
	},
	badInterfaceSlot: &interfaces.Slot{
		Interface: "other-interface",
	},
})

func (s *BoolFileInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "bool-file")
}

func (s *BoolFileInterfaceSuite) TestSanitizePlug(c *C) {
	// Both LED and GPIO plugs are accepted
	err := s.iface.SanitizePlug(s.ledPlug)
	c.Assert(err, IsNil)
	err = s.iface.SanitizePlug(s.gpioPlug)
	c.Assert(err, IsNil)
	// Plugs without the "path" attribute are rejected.
	err = s.iface.SanitizePlug(s.missingPathPlug)
	c.Assert(err, ErrorMatches,
		"bool-file must contain the path attribute")
	// Plugs without the "path" attribute are rejected.
	err = s.iface.SanitizePlug(s.parentDirPathPlug)
	c.Assert(err, ErrorMatches,
		"bool-file can only point at LED brightness or GPIO value")
	// Plugs with incorrect value of the "path" attribute are rejected.
	err = s.iface.SanitizePlug(s.badPathPlug)
	c.Assert(err, ErrorMatches,
		"bool-file can only point at LED brightness or GPIO value")
	// It is impossible to use "bool-file" interface to sanitize plugs with other interfaces.
	c.Assert(func() { s.iface.SanitizePlug(s.badInterfacePlug) }, PanicMatches,
		`plug is not of interface "bool-file"`)
}

func (s *BoolFileInterfaceSuite) TestSanitizeSlot(c *C) {
	err := s.iface.SanitizeSlot(s.slot)
	c.Assert(err, IsNil)
	// It is impossible to use "bool-file" interface to sanitize slots of different interface.
	c.Assert(func() { s.iface.SanitizeSlot(s.badInterfaceSlot) }, PanicMatches,
		`slot is not of interface "bool-file"`)
}

func (s *BoolFileInterfaceSuite) TestPlugSecuritySnippetHandlesSymlinkErrors(c *C) {
	// Symbolic link traversal is handled correctly
	builtin.MockEvalSymlinks(&s.BaseTest, func(path string) (string, error) {
		return "", fmt.Errorf("broken symbolic link")
	})
	snippet, err := s.iface.PlugSecuritySnippet(s.gpioPlug, s.slot, interfaces.SecurityAppArmor)
	c.Assert(err, ErrorMatches, "cannot compute slot security snippet: broken symbolic link")
	c.Assert(snippet, IsNil)
}

func (s *BoolFileInterfaceSuite) TestPlugSecuritySnippetDereferencesSymlinks(c *C) {
	// Use a fake (successful) dereferencing function for the remainder of the test.
	builtin.MockEvalSymlinks(&s.BaseTest, func(path string) (string, error) {
		return "(dereferenced)" + path, nil
	})
	// Extra apparmor permission to access GPIO value
	// The path uses dereferenced symbolic links.
	snippet, err := s.iface.PlugSecuritySnippet(s.gpioPlug, s.slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, DeepEquals, []byte(
		"(dereferenced)/sys/class/gpio/gpio13/value rwk,\n"))
	// Extra apparmor permission to access LED brightness.
	// The path uses dereferenced symbolic links.
	snippet, err = s.iface.PlugSecuritySnippet(s.ledPlug, s.slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, DeepEquals, []byte(
		"(dereferenced)/sys/class/leds/input27::capslock/brightness rwk,\n"))
}

func (s *BoolFileInterfaceSuite) TestSlotSecurityDoesNotContainPlugSecurity(c *C) {
	// Use a fake (successful) dereferencing function for the remainder of the test.
	builtin.MockEvalSymlinks(&s.BaseTest, func(path string) (string, error) {
		return path, nil
	})
	var err error
	var plugSnippet, slotSnippet []byte
	slotSnippet, err = s.iface.PlugSecuritySnippet(s.gpioPlug, s.slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	plugSnippet, err = s.iface.SlotSecuritySnippet(s.gpioPlug, s.slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	// Ensure that we don't accidentally give plug-side permissions to slot-side.
	c.Assert(bytes.Contains(slotSnippet, plugSnippet), Equals, false)
}

func (s *BoolFileInterfaceSuite) TestPlugSecuritySnippetPanicksOnUnsanitizedPlugs(c *C) {
	// Unsanitized plugs should never be used and cause a panic.
	c.Assert(func() {
		s.iface.PlugSecuritySnippet(s.missingPathPlug, s.slot, interfaces.SecurityAppArmor)
	}, PanicMatches, "plug is not sanitized")
}

func (s *BoolFileInterfaceSuite) TestPlugSecuritySnippetUnusedSecuritySystems(c *C) {
	for _, plug := range []*interfaces.Plug{s.ledPlug, s.gpioPlug} {
		// No extra seccomp permissions for slot
		snippet, err := s.iface.PlugSecuritySnippet(plug, s.slot, interfaces.SecuritySecComp)
		c.Assert(err, IsNil)
		c.Assert(snippet, IsNil)
		// No extra dbus permissions for slot
		snippet, err = s.iface.PlugSecuritySnippet(plug, s.slot, interfaces.SecurityDBus)
		c.Assert(err, IsNil)
		c.Assert(snippet, IsNil)
		// No extra udev permissions for slot
		snippet, err = s.iface.PlugSecuritySnippet(plug, s.slot, interfaces.SecurityUDev)
		c.Assert(err, IsNil)
		c.Assert(snippet, IsNil)
		// No extra udev permissions for slot
		snippet, err = s.iface.PlugSecuritySnippet(plug, s.slot, interfaces.SecurityUDev)
		c.Assert(err, IsNil)
		c.Assert(snippet, IsNil)
		// Other security types are not recognized
		snippet, err = s.iface.PlugSecuritySnippet(plug, s.slot, "foo")
		c.Assert(err, ErrorMatches, `unknown security system`)
		c.Assert(snippet, IsNil)
	}
}

func (s *BoolFileInterfaceSuite) TestSlotSecuritySnippetGivesExtraPermissionsToConfigureGPIOs(c *C) {
	// Extra apparmor permission to provide GPIOs
	expectedGPIOSnippet := []byte(`
/sys/class/gpio/export rw,
/sys/class/gpio/unexport rw,
/sys/class/gpio/gpio[0-9]+/direction rw,
`)
	snippet, err := s.iface.SlotSecuritySnippet(s.gpioPlug, s.slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, DeepEquals, expectedGPIOSnippet)
}

func (s *BoolFileInterfaceSuite) TestSlotSecuritySnippetGivesNoExtraPermissionsToConfigureLEDs(c *C) {
	// No extra apparmor permission to provide LEDs
	snippet, err := s.iface.SlotSecuritySnippet(s.ledPlug, s.slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
}

func (s *BoolFileInterfaceSuite) TestSlotSecuritySnippetPanicksOnUnsanitizedPlugs(c *C) {
	// Unsanitized plugs should never be used and cause a panic.
	c.Assert(func() {
		s.iface.SlotSecuritySnippet(s.missingPathPlug, s.slot, interfaces.SecurityAppArmor)
	}, PanicMatches, "plug is not sanitized")
}

func (s *BoolFileInterfaceSuite) TestSlotSecuritySnippetUnusedSecuritySystems(c *C) {
	for _, plug := range []*interfaces.Plug{s.ledPlug, s.gpioPlug} {
		// No extra seccomp permissions for plug
		snippet, err := s.iface.SlotSecuritySnippet(plug, s.slot, interfaces.SecuritySecComp)
		c.Assert(err, IsNil)
		c.Assert(snippet, IsNil)
		// No extra dbus permissions for plug
		snippet, err = s.iface.SlotSecuritySnippet(plug, s.slot, interfaces.SecurityDBus)
		c.Assert(err, IsNil)
		c.Assert(snippet, IsNil)
		// No extra udev permissions for plug
		snippet, err = s.iface.SlotSecuritySnippet(plug, s.slot, interfaces.SecurityUDev)
		c.Assert(err, IsNil)
		c.Assert(snippet, IsNil)
		// Other security types are not recognized
		snippet, err = s.iface.SlotSecuritySnippet(plug, s.slot, "foo")
		c.Assert(err, ErrorMatches, `unknown security system`)
		c.Assert(snippet, IsNil)
	}
}
