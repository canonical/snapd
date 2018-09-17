// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2017 Canonical Ltd
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

package ifacetest_test

import (
	"fmt"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/dbus"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/snap"
)

type TestInterfaceSuite struct {
	iface    interfaces.Interface
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
}

var _ = Suite(&TestInterfaceSuite{
	iface: &ifacetest.TestInterface{
		InterfaceName: "test",
		InterfaceStaticInfo: interfaces.StaticInfo{
			Summary: "summary",
		},
	},
	plugInfo: &snap.PlugInfo{
		Snap:      &snap.Info{SuggestedName: "snap"},
		Name:      "name",
		Interface: "test",
	},
	slotInfo: &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "snap"},
		Name:      "name",
		Interface: "test",
	},
})

// TestInterface has a working Name() function
func (s *TestInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "test")
	s.plug = interfaces.NewConnectedPlug(s.plugInfo, nil, nil)
	s.slot = interfaces.NewConnectedSlot(s.slotInfo, nil, nil)
}

func (s *TestInterfaceSuite) TestStaticInfo(c *C) {
	c.Assert(interfaces.StaticInfoOf(s.iface), Equals, interfaces.StaticInfo{
		Summary: "summary",
	})
}

// TestInterface has provisions to customize validation
func (s *TestInterfaceSuite) TestBeforeConnectPlugError(c *C) {
	iface := &ifacetest.TestInterface{
		InterfaceName: "test",
		BeforeConnectPlugCallback: func(plug *interfaces.ConnectedPlug) error {
			return fmt.Errorf("plug validation failed")
		},
	}
	err := iface.BeforeConnectPlug(s.plug)
	c.Assert(err, ErrorMatches, "plug validation failed")
}

func (s *TestInterfaceSuite) TestBeforeConnectSlotError(c *C) {
	iface := &ifacetest.TestInterface{
		InterfaceName: "test",
		BeforeConnectSlotCallback: func(slot *interfaces.ConnectedSlot) error {
			return fmt.Errorf("slot validation failed")
		},
	}
	err := iface.BeforeConnectSlot(s.slot)
	c.Assert(err, ErrorMatches, "slot validation failed")
}

// TestInterface doesn't do any sanitization by default
func (s *TestInterfaceSuite) TestSanitizePlugOK(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

// TestInterface has provisions to customize sanitization
func (s *TestInterfaceSuite) TestSanitizePlugError(c *C) {
	iface := &ifacetest.TestInterface{
		InterfaceName: "test",
		BeforePreparePlugCallback: func(plug *snap.PlugInfo) error {
			return fmt.Errorf("sanitize plug failed")
		},
	}
	c.Assert(interfaces.BeforePreparePlug(iface, s.plugInfo), ErrorMatches, "sanitize plug failed")
}

// TestInterface doesn't do any sanitization by default
func (s *TestInterfaceSuite) TestSanitizeSlotOK(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

// TestInterface has provisions to customize sanitization
func (s *TestInterfaceSuite) TestSanitizeSlotError(c *C) {
	iface := &ifacetest.TestInterface{
		InterfaceName: "test",
		BeforePrepareSlotCallback: func(slot *snap.SlotInfo) error {
			return fmt.Errorf("sanitize slot failed")
		},
	}
	c.Assert(interfaces.BeforePrepareSlot(iface, s.slotInfo), ErrorMatches, "sanitize slot failed")
}

// TestInterface hands out empty plug security snippets
func (s *TestInterfaceSuite) TestPlugSnippet(c *C) {
	iface := s.iface.(*ifacetest.TestInterface)

	apparmorSpec := &apparmor.Specification{}
	c.Assert(iface.AppArmorConnectedPlug(apparmorSpec, s.plug, s.slot), IsNil)
	c.Assert(apparmorSpec.Snippets(), HasLen, 0)

	seccompSpec := &seccomp.Specification{}
	c.Assert(iface.SecCompConnectedPlug(seccompSpec, s.plug, s.slot), IsNil)
	c.Assert(seccompSpec.Snippets(), HasLen, 0)

	dbusSpec := &dbus.Specification{}
	c.Assert(iface.DBusConnectedPlug(dbusSpec, s.plug, s.slot), IsNil)
	c.Assert(dbusSpec.Snippets(), HasLen, 0)
}

// TestInterface hands out empty slot security snippets
func (s *TestInterfaceSuite) TestSlotSnippet(c *C) {
	iface := s.iface.(*ifacetest.TestInterface)

	apparmorSpec := &apparmor.Specification{}
	c.Assert(iface.AppArmorConnectedSlot(apparmorSpec, s.plug, s.slot), IsNil)
	c.Assert(apparmorSpec.Snippets(), HasLen, 0)

	seccompSpec := &seccomp.Specification{}
	c.Assert(iface.SecCompConnectedSlot(seccompSpec, s.plug, s.slot), IsNil)
	c.Assert(seccompSpec.Snippets(), HasLen, 0)

	dbusSpec := &dbus.Specification{}
	c.Assert(iface.DBusConnectedSlot(dbusSpec, s.plug, s.slot), IsNil)
	c.Assert(dbusSpec.Snippets(), HasLen, 0)
}

func (s *TestInterfaceSuite) TestAutoConnect(c *C) {
	c.Check(s.iface.AutoConnect(nil, nil), Equals, true)

	iface := &ifacetest.TestInterface{AutoConnectCallback: func(*snap.PlugInfo, *snap.SlotInfo) bool { return false }}

	c.Check(iface.AutoConnect(nil, nil), Equals, false)
}
