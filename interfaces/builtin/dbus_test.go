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
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type DbusInterfaceSuite struct {
	testutil.BaseTest
	iface           interfaces.Interface
	slot            *interfaces.Slot
	plug            *interfaces.Plug
	testSessionSlot *interfaces.Slot
	testSystemSlot  *interfaces.Slot
}

var _ = Suite(&DbusInterfaceSuite{
	iface: &builtin.DbusInterface{},
})

func (s *DbusInterfaceSuite) SetUpTest(c *C) {
	info, err := snap.InfoFromSnapYaml([]byte(`
name: test-dbus
slots:
  test-slot:
    interface: dbus
    session:
    - org.test-slot
  test-session:
    interface: dbus
    session:
    - org.test-session1
    - org.test-session2
  test-system:
    interface: dbus
    system:
    - org.test-system

apps:
  test-provider:
    slots:
    - test-slot
  test-session-provider:
    slots:
    - test-session
  test-system-provider:
    slots:
    - test-system
`))
	c.Assert(err, IsNil)
	s.plug = &interfaces.Plug{PlugInfo: info.Plugs["test-plug"]}
	s.slot = &interfaces.Slot{SlotInfo: info.Slots["test-slot"]}
	s.testSessionSlot = &interfaces.Slot{SlotInfo: info.Slots["test-session"]}
	s.testSystemSlot = &interfaces.Slot{SlotInfo: info.Slots["test-system"]}
}

func (s *DbusInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "dbus")
}

func (s *DbusInterfaceSuite) TestUsedSecuritySystems(c *C) {
	systems := [...]interfaces.SecuritySystem{interfaces.SecurityAppArmor,
		interfaces.SecuritySecComp}
	for _, system := range systems {
		snippet, err := s.iface.PermanentSlotSnippet(s.slot, system)
		c.Assert(err, IsNil)
		c.Assert(snippet, Not(IsNil))
	}
}

func (s *DbusInterfaceSuite) TestGetBusNamesSession(c *C) {
	var mockSnapYaml = []byte(`name: dbus-snap
version: 1.0
slots:
 dbus-slot:
  interface: dbus
  session:
  - org.dbus-snap.session-1
  - org.dbus-snap.session-2
`)

	info, err := snap.InfoFromSnapYaml(mockSnapYaml)
	c.Assert(err, IsNil)

	slot := &interfaces.Slot{SlotInfo: info.Slots["dbus-slot"]}
	err = s.iface.SanitizeSlot(slot)
	c.Assert(err, IsNil)
}

func (s *DbusInterfaceSuite) TestGetBusNamesSystem(c *C) {
	var mockSnapYaml = []byte(`name: dbus-snap
version: 1.0
slots:
 dbus-slot:
  interface: dbus
  system:
  - org.dbus-snap.system-1
  - org.dbus-snap.system-2
`)

	info, err := snap.InfoFromSnapYaml(mockSnapYaml)
	c.Assert(err, IsNil)

	slot := &interfaces.Slot{SlotInfo: info.Slots["dbus-slot"]}
	err = s.iface.SanitizeSlot(slot)
	c.Assert(err, IsNil)
}

func (s *DbusInterfaceSuite) TestGetBusNamesFull(c *C) {
	var mockSnapYaml = []byte(`name: dbus-snap
version: 1.0
slots:
 dbus-slot:
  interface: dbus
  system:
  - org.dbus-snap.foo.bar.baz.n0rf_qux
`)

	info, err := snap.InfoFromSnapYaml(mockSnapYaml)
	c.Assert(err, IsNil)

	slot := &interfaces.Slot{SlotInfo: info.Slots["dbus-slot"]}
	err = s.iface.SanitizeSlot(slot)
	c.Assert(err, IsNil)
}

func (s *DbusInterfaceSuite) TestGetBusNamesNonexistentBus(c *C) {
	var mockSnapYaml = []byte(`name: dbus-snap
version: 1.0
slots:
 dbus-slot:
  interface: dbus
  nonexistent:
  - org.dbus-snap
`)

	info, err := snap.InfoFromSnapYaml(mockSnapYaml)
	c.Assert(err, IsNil)

	slot := &interfaces.Slot{SlotInfo: info.Slots["dbus-slot"]}
	err = s.iface.SanitizeSlot(slot)
	c.Assert(err, Not(IsNil))
	c.Assert(err, ErrorMatches, "bus must be one of 'session' or 'system'")
}

func (s *DbusInterfaceSuite) TestSanitizeSlotSystem(c *C) {
	var mockSnapYaml = []byte(`name: dbus-snap
version: 1.0
slots:
 dbus-slot:
  interface: dbus
  session:
  - org.dbus-snap.system-1
  - org.dbus-snap.system-2
`)

	info, err := snap.InfoFromSnapYaml(mockSnapYaml)
	c.Assert(err, IsNil)

	slot := &interfaces.Slot{SlotInfo: info.Slots["dbus-slot"]}
	err = s.iface.SanitizeSlot(slot)
	c.Assert(err, IsNil)
}

func (s *DbusInterfaceSuite) TestPermanentSlotAppArmorSession(c *C) {
	snippet, err := s.iface.PermanentSlotSnippet(s.testSessionSlot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, Not(IsNil))

	// verify abstraction rule
	c.Check(string(snippet), testutil.Contains, "#include <abstractions/dbus-session-strict>\n")

	// verify shared permanent slot policy
	c.Check(string(snippet), testutil.Contains, "dbus (send)\n    bus=system\n    path=/org/freedesktop/DBus\n    interface=org.freedesktop.DBus\n    member=\"{Request,Release}Name\"\n    peer=(name=org.freedesktop.DBus, label=unconfined),\n")

	// verify individual bind rules
	c.Check(string(snippet), testutil.Contains, "dbus (bind)\n    bus=session\n    name=org.test-session1,\n")
	c.Check(string(snippet), testutil.Contains, "dbus (bind)\n    bus=session\n    name=org.test-session2,\n")

	// verify individual path in rules
	c.Check(string(snippet), testutil.Contains, "path=\"/org/test-session1{,/**}\"\n")
	c.Check(string(snippet), testutil.Contains, "path=\"/org/test-session2{,/**}\"\n")
}

func (s *DbusInterfaceSuite) TestPermanentSlotAppArmorSessionNative(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()
	iface := &builtin.DbusInterface{}
	snippet, err := iface.PermanentSlotSnippet(s.testSessionSlot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, Not(IsNil))

	// verify classic rule not present
	c.Check(string(snippet), Not(testutil.Contains), "# allow unconfined clients talk to org.test-session1 on classic\n")
}

func (s *DbusInterfaceSuite) TestPermanentSlotAppArmorSessionClassic(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()
	iface := &builtin.DbusInterface{}
	snippet, err := iface.PermanentSlotSnippet(s.testSessionSlot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, Not(IsNil))

	// verify classic rule
	c.Check(string(snippet), testutil.Contains, "# allow unconfined clients talk to org.test-session1 on classic\n")
}

func (s *DbusInterfaceSuite) TestPermanentSlotAppArmorSystem(c *C) {
	snippet, err := s.iface.PermanentSlotSnippet(s.testSystemSlot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, Not(IsNil))

	// verify abstraction rule
	c.Check(string(snippet), testutil.Contains, "#include <abstractions/dbus-strict>\n")

	// verify bind rule
	c.Check(string(snippet), testutil.Contains, "dbus (bind)\n    bus=system\n    name=org.test-system,\n")

	// verify path in rule
	c.Check(string(snippet), testutil.Contains, "path=\"/org/test-system{,/**}\"\n")
}

func (s *DbusInterfaceSuite) TestPermanentSlotSeccomp(c *C) {
	snippet, err := s.iface.PermanentSlotSnippet(s.testSessionSlot, interfaces.SecuritySecComp)
	c.Assert(err, IsNil)
	c.Assert(snippet, Not(IsNil))

	c.Check(string(snippet), testutil.Contains, "getsockname\n")
}

func (s *DbusInterfaceSuite) TestLegacyAutoConnect(c *C) {
	iface := &builtin.DbusInterface{}
	c.Check(iface.LegacyAutoConnect(), Equals, false)
}
