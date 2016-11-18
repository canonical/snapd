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
	iface       interfaces.Interface
	sessionPlug *interfaces.Plug
	systemPlug  *interfaces.Plug
	sessionSlot *interfaces.Slot
	systemSlot  *interfaces.Slot
}

var _ = Suite(&DbusInterfaceSuite{
	iface: &builtin.DbusInterface{},
})

func (s *DbusInterfaceSuite) SetUpTest(c *C) {
	info, err := snap.InfoFromSnapYaml([]byte(`
name: test-dbus
slots:
  test-session-slot:
    interface: dbus
    bus: session
    name: org.test-session-slot
  test-system-slot:
    interface: dbus
    bus: system
    name: org.test-system-slot

plugs:
  test-session-plug:
    interface: dbus
    bus: session
    name: org.test-session-plug
  test-system-plug:
    interface: dbus
    bus: system
    name: org.test-system-plug

apps:
  test-session-provider:
    slots:
    - test-session-slot
  test-system-provider:
    slots:
    - test-system-slot
  test-session-consumer:
    plugs:
    - test-session-plug
  test-system-consumer:
    plugs:
    - test-system-plug
`))
	c.Assert(err, IsNil)
	s.sessionSlot = &interfaces.Slot{SlotInfo: info.Slots["test-session-slot"]}
	s.systemSlot = &interfaces.Slot{SlotInfo: info.Slots["test-system-slot"]}
	s.sessionPlug = &interfaces.Plug{PlugInfo: info.Plugs["test-session-plug"]}
	s.systemPlug = &interfaces.Plug{PlugInfo: info.Plugs["test-system-plug"]}
}

func (s *DbusInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "dbus")
}

func (s *DbusInterfaceSuite) TestUsedSecuritySystems(c *C) {
	snippet, err := s.iface.PermanentSlotSnippet(s.sessionSlot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, Not(IsNil))

	snippet, err = s.iface.PermanentSlotSnippet(s.sessionSlot, interfaces.SecuritySecComp)
	c.Assert(err, IsNil)
	c.Assert(snippet, Not(IsNil))
}

func (s *DbusInterfaceSuite) TestValidSessionBusName(c *C) {
	var mockSnapYaml = []byte(`name: dbus-snap
version: 1.0
slots:
 dbus-slot:
  interface: dbus
  bus: session
  name: org.dbus-snap.session-1
`)

	info, err := snap.InfoFromSnapYaml(mockSnapYaml)
	c.Assert(err, IsNil)

	slot := &interfaces.Slot{SlotInfo: info.Slots["dbus-slot"]}
	err = s.iface.SanitizeSlot(slot)
	c.Assert(err, IsNil)
}

func (s *DbusInterfaceSuite) TestValidSystemBusName(c *C) {
	var mockSnapYaml = []byte(`name: dbus-snap
version: 1.0
slots:
 dbus-slot:
  interface: dbus
  bus: system
  name: org.dbus-snap.system-1
`)

	info, err := snap.InfoFromSnapYaml(mockSnapYaml)
	c.Assert(err, IsNil)

	slot := &interfaces.Slot{SlotInfo: info.Slots["dbus-slot"]}
	err = s.iface.SanitizeSlot(slot)
	c.Assert(err, IsNil)
}

func (s *DbusInterfaceSuite) TestValidFullBusName(c *C) {
	var mockSnapYaml = []byte(`name: dbus-snap
version: 1.0
slots:
 dbus-slot:
  interface: dbus
  bus: system
  name: org.dbus-snap.foo.bar.baz.n0rf_qux
`)

	info, err := snap.InfoFromSnapYaml(mockSnapYaml)
	c.Assert(err, IsNil)

	slot := &interfaces.Slot{SlotInfo: info.Slots["dbus-slot"]}
	err = s.iface.SanitizeSlot(slot)
	c.Assert(err, IsNil)
}

func (s *DbusInterfaceSuite) TestNonexistentBusName(c *C) {
	var mockSnapYaml = []byte(`name: dbus-snap
version: 1.0
slots:
 dbus-slot:
  interface: dbus
  bus: nonexistent
  name: org.dbus-snap
`)

	info, err := snap.InfoFromSnapYaml(mockSnapYaml)
	c.Assert(err, IsNil)

	slot := &interfaces.Slot{SlotInfo: info.Slots["dbus-slot"]}
	err = s.iface.SanitizeSlot(slot)
	c.Assert(err, Not(IsNil))
	c.Assert(err, ErrorMatches, "bus 'nonexistent' must be one of 'session' or 'system'")
}

func (s *DbusInterfaceSuite) TestSanitizePlugSystem(c *C) {
	var mockSnapYaml = []byte(`name: dbus-snap
version: 1.0
plugs:
 dbus-plug:
  interface: dbus
  bus: system
  name: org.dbus-snap.system
`)

	info, err := snap.InfoFromSnapYaml(mockSnapYaml)
	c.Assert(err, IsNil)

	plug := &interfaces.Plug{PlugInfo: info.Plugs["dbus-plug"]}
	err = s.iface.SanitizePlug(plug)
	c.Assert(err, IsNil)
}

func (s *DbusInterfaceSuite) TestSanitizePlugSession(c *C) {
	var mockSnapYaml = []byte(`name: dbus-snap
version: 1.0
plugs:
 dbus-plug:
  interface: dbus
  bus: session
  name: org.dbus-snap.session
`)

	info, err := snap.InfoFromSnapYaml(mockSnapYaml)
	c.Assert(err, IsNil)

	plug := &interfaces.Plug{PlugInfo: info.Plugs["dbus-plug"]}
	err = s.iface.SanitizePlug(plug)
	c.Assert(err, IsNil)
}

func (s *DbusInterfaceSuite) TestSanitizeSlotSystem(c *C) {
	var mockSnapYaml = []byte(`name: dbus-snap
version: 1.0
slots:
 dbus-slot:
  interface: dbus
  bus: system
  name: org.dbus-snap.system
`)

	info, err := snap.InfoFromSnapYaml(mockSnapYaml)
	c.Assert(err, IsNil)

	slot := &interfaces.Slot{SlotInfo: info.Slots["dbus-slot"]}
	err = s.iface.SanitizeSlot(slot)
	c.Assert(err, IsNil)
}

func (s *DbusInterfaceSuite) TestSanitizeSlotSession(c *C) {
	var mockSnapYaml = []byte(`name: dbus-snap
version: 1.0
slots:
 dbus-slot:
  interface: dbus
  bus: session
  name: org.dbus-snap.session
`)

	info, err := snap.InfoFromSnapYaml(mockSnapYaml)
	c.Assert(err, IsNil)

	slot := &interfaces.Slot{SlotInfo: info.Slots["dbus-slot"]}
	err = s.iface.SanitizeSlot(slot)
	c.Assert(err, IsNil)
}

func (s *DbusInterfaceSuite) TestPermanentSlotAppArmorSession(c *C) {
	snippet, err := s.iface.PermanentSlotSnippet(s.sessionSlot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, Not(IsNil))

	// verify abstraction rule
	c.Check(string(snippet), testutil.Contains, "#include <abstractions/dbus-session-strict>\n")

	// verify shared permanent slot policy
	c.Check(string(snippet), testutil.Contains, "dbus (send)\n    bus=system\n    path=/org/freedesktop/DBus\n    interface=org.freedesktop.DBus\n    member=\"{Request,Release}Name\"\n    peer=(name=org.freedesktop.DBus, label=unconfined),\n")

	// verify individual bind rules
	c.Check(string(snippet), testutil.Contains, "dbus (bind)\n    bus=session\n    name=org.test-session-slot,\n")

	// verify individual path in rules
	c.Check(string(snippet), testutil.Contains, "path=\"/org/test-session-slot{,/**}\"\n")
}

func (s *DbusInterfaceSuite) TestPermanentSlotAppArmorSessionNative(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()
	iface := &builtin.DbusInterface{}
	snippet, err := iface.PermanentSlotSnippet(s.sessionSlot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, Not(IsNil))

	// verify classic rule not present
	c.Check(string(snippet), Not(testutil.Contains), "# allow unconfined clients talk to org.test-session-slot on classic\n")
}

func (s *DbusInterfaceSuite) TestPermanentSlotAppArmorSessionClassic(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()
	iface := &builtin.DbusInterface{}
	snippet, err := iface.PermanentSlotSnippet(s.sessionSlot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, Not(IsNil))

	// verify classic rule
	c.Check(string(snippet), testutil.Contains, "# allow unconfined clients talk to org.test-session-slot on classic\n")
}

func (s *DbusInterfaceSuite) TestPermanentSlotAppArmorSystem(c *C) {
	snippet, err := s.iface.PermanentSlotSnippet(s.systemSlot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, Not(IsNil))

	// verify abstraction rule
	c.Check(string(snippet), testutil.Contains, "#include <abstractions/dbus-strict>\n")

	// verify bind rule
	c.Check(string(snippet), testutil.Contains, "dbus (bind)\n    bus=system\n    name=org.test-system-slot,\n")

	// verify path in rule
	c.Check(string(snippet), testutil.Contains, "path=\"/org/test-system-slot{,/**}\"\n")
}

func (s *DbusInterfaceSuite) TestPermanentSlotSeccomp(c *C) {
	snippet, err := s.iface.PermanentSlotSnippet(s.sessionSlot, interfaces.SecuritySecComp)
	c.Assert(err, IsNil)
	c.Assert(snippet, Not(IsNil))

	c.Check(string(snippet), testutil.Contains, "getsockname\n")
}
