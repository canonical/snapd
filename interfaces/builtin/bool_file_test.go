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
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
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
})

func (s *BoolFileInterfaceSuite) SetUpTest(c *C) {
	plugSnapinfo := snaptest.MockInfo(c, `
name: other
plugs:
 plug: bool-file
apps:
 app:
  command: foo
`, nil)
	info := snaptest.MockInfo(c, `
name: ubuntu-core
slots:
    gpio:
        interface: bool-file
        path: /sys/class/gpio/gpio13/value
    led:
        interface: bool-file
        path: "/sys/class/leds/input27::capslock/brightness"
    missing-path: bool-file
    bad-path:
        interface: bool-file
        path: path
    parent-dir-path:
        interface: bool-file
        path: "/sys/class/gpio/../value"
    bad-interface-slot: other-interface
plugs:
    plug: bool-file
    bad-interface-plug: other-interface
`, &snap.SideInfo{})
	s.gpioSlot = &interfaces.Slot{SlotInfo: info.Slots["gpio"]}
	s.ledSlot = &interfaces.Slot{SlotInfo: info.Slots["led"]}
	s.missingPathSlot = &interfaces.Slot{SlotInfo: info.Slots["missing-path"]}
	s.badPathSlot = &interfaces.Slot{SlotInfo: info.Slots["bad-path"]}
	s.parentDirPathSlot = &interfaces.Slot{SlotInfo: info.Slots["parent-dir-path"]}
	s.badInterfaceSlot = &interfaces.Slot{SlotInfo: info.Slots["bad-interface-slot"]}
	s.plug = &interfaces.Plug{PlugInfo: plugSnapinfo.Plugs["plug"]}
	s.badInterfacePlug = &interfaces.Plug{PlugInfo: info.Plugs["bad-interface-plug"]}
}

// TODO: add test for permanent slot when we have hook support.

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

	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.gpioSlot)
	c.Assert(err, ErrorMatches, "cannot compute plug security snippet: broken symbolic link")
	c.Assert(apparmorSpec.SecurityTags(), HasLen, 0)
}

func (s *BoolFileInterfaceSuite) TestPlugSnippetDereferencesSymlinks(c *C) {
	// Use a fake (successful) dereferencing function for the remainder of the test.
	builtin.MockEvalSymlinks(&s.BaseTest, func(path string) (string, error) {
		return "(dereferenced)" + path, nil
	})
	// Extra apparmor permission to access GPIO value
	// The path uses dereferenced symbolic links.
	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.gpioSlot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app"), Equals, "(dereferenced)/sys/class/gpio/gpio13/value rwk,")
	// Extra apparmor permission to access LED brightness.
	// The path uses dereferenced symbolic links.
	apparmorSpec = &apparmor.Specification{}
	err = apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.ledSlot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app"), Equals, "(dereferenced)/sys/class/leds/input27::capslock/brightness rwk,")
}

func (s *BoolFileInterfaceSuite) TestConnectedPlugSnippetPanicksOnUnsanitizedSlots(c *C) {
	// Unsanitized slots should never be used and cause a panic.
	c.Assert(func() {
		apparmorSpec := &apparmor.Specification{}
		apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.missingPathSlot)
	}, PanicMatches, "slot is not sanitized")
}

func (s *BoolFileInterfaceSuite) TestConnectedPlugSnippetUnusedSecuritySystems(c *C) {
	for _, slot := range []*interfaces.Slot{s.ledSlot, s.gpioSlot} {
		// No extra seccomp permissions for plug
		seccompSpec := &seccomp.Specification{}
		err := seccompSpec.AddConnectedPlug(s.iface, s.plug, slot)
		c.Assert(err, IsNil)
		c.Assert(seccompSpec.Snippets(), HasLen, 0)
		// No extra dbus permissions for plug
		snippet, err := s.iface.ConnectedPlugSnippet(s.plug, slot, interfaces.SecurityDBus)
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
	}
}

func (s *BoolFileInterfaceSuite) TestPermanentPlugSnippetUnusedSecuritySystems(c *C) {
	// No extra seccomp permissions for plug
	seccompSpec := &seccomp.Specification{}
	err := seccompSpec.AddPermanentPlug(s.iface, s.plug)
	c.Assert(err, IsNil)
	c.Assert(seccompSpec.Snippets(), HasLen, 0)
	// No extra dbus permissions for plug
	snippet, err := s.iface.PermanentPlugSnippet(s.plug, interfaces.SecurityDBus)
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
}
