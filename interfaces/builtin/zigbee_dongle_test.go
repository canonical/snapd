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

type ZigbeeDongleInterfaceSuite struct {
	testutil.BaseTest
	iface            interfaces.Interface
	zigbeeAccessSlot *interfaces.Slot
	badInterfaceSlot *interfaces.Slot
	plug             *interfaces.Plug
	badInterfacePlug *interfaces.Plug
}

var _ = Suite(&ZigbeeDongleInterfaceSuite{
	iface: &builtin.ZigbeeDongleInterface{},
})

func (s *ZigbeeDongleInterfaceSuite) SetUpTest(c *C) {
	info, err := snap.InfoFromSnapYaml([]byte(`
name: ubuntu-core
slots:
    zigbee-access:
        interface: zigbee-dongle
    bad-interface: other-interface
plugs:
    plug: zigbee-dongle
    bad-interface: other-interface
`))
	c.Assert(err, IsNil)
	s.zigbeeAccessSlot = &interfaces.Slot{SlotInfo: info.Slots["zigbee-access"]}
	s.badInterfaceSlot = &interfaces.Slot{SlotInfo: info.Slots["bad-interface"]}
	s.plug = &interfaces.Plug{PlugInfo: info.Plugs["plug"]}
	s.badInterfacePlug = &interfaces.Plug{PlugInfo: info.Plugs["bad-interface"]}
}

func (s *ZigbeeDongleInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "zigbee-dongle")
}

func (s *ZigbeeDongleInterfaceSuite) TestPermanentSlotSnippetUdevPermissions(c *C) {
	expectedSlotSnippet := []byte(`
IMPORT{builtin}="usb_id"
SUBSYSTEM=="tty", SUBSYSTEMS=="usb", ATTRS{idProduct}=="0003", ATTRS{idVendor}=="10c4", SYMLINK+="zigbee/$env{ID_SERIAL}"
`)
	snippet, err := s.iface.PermanentSlotSnippet(s.zigbeeAccessSlot, interfaces.SecurityUDev)
	c.Assert(err, IsNil)
	c.Assert(snippet, DeepEquals, expectedSlotSnippet)
}

func (s *ZigbeeDongleInterfaceSuite) TestPermanentSlotSnippetUnusedSecuritySystems(c *C) {
	// No extra apparmor permissions for slot
	snippet, err := s.iface.PermanentSlotSnippet(s.zigbeeAccessSlot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
	// No extra seccomp permissions for slot
	snippet, err = s.iface.PermanentSlotSnippet(s.zigbeeAccessSlot, interfaces.SecuritySecComp)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
	// No extra dbus permissions for slot
	snippet, err = s.iface.PermanentSlotSnippet(s.zigbeeAccessSlot, interfaces.SecurityDBus)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
	// Other security types are not recognized
	snippet, err = s.iface.PermanentSlotSnippet(s.zigbeeAccessSlot, "foo")
	c.Assert(err, ErrorMatches, `unknown security system`)
	c.Assert(snippet, IsNil)
}

func (s *ZigbeeDongleInterfaceSuite) TestConnectedSlotSnippetUnusedSecuritySystems(c *C) {
	// No extra apparmor permissions for slot
	snippet, err := s.iface.ConnectedSlotSnippet(s.plug, s.zigbeeAccessSlot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
	// No extra seccomp permissions for slot
	snippet, err = s.iface.ConnectedSlotSnippet(s.plug, s.zigbeeAccessSlot, interfaces.SecuritySecComp)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
	// No extra dbus permissions for slot
	snippet, err = s.iface.ConnectedSlotSnippet(s.plug, s.zigbeeAccessSlot, interfaces.SecurityDBus)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
	// No extra udev permissions for slot
	snippet, err = s.iface.ConnectedSlotSnippet(s.plug, s.zigbeeAccessSlot, interfaces.SecurityUDev)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
	// Other security types are not recognized
	snippet, err = s.iface.ConnectedSlotSnippet(s.plug, s.zigbeeAccessSlot, "foo")
	c.Assert(err, ErrorMatches, `unknown security system`)
	c.Assert(snippet, IsNil)
}

func (s *ZigbeeDongleInterfaceSuite) TestPermanentPlugSnippetUnusedSecuritySystems(c *C) {
	// No extra apparmor permissions for plug
	snippet, err := s.iface.PermanentPlugSnippet(s.plug, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
	// No extra seccomp permissions for plug
	snippet, err = s.iface.PermanentPlugSnippet(s.plug, interfaces.SecuritySecComp)
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
	// Other security types are not recognized
	snippet, err = s.iface.PermanentPlugSnippet(s.plug, "foo")
	c.Assert(err, ErrorMatches, `unknown security system`)
	c.Assert(snippet, IsNil)
}

func (s *ZigbeeDongleInterfaceSuite) TestAutoConnect(c *C) {
	c.Check(s.iface.AutoConnect(), Equals, false)
}
