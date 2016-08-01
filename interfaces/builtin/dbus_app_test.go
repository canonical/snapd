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

type DbusAppInterfaceSuite struct {
	testutil.BaseTest
	iface           interfaces.Interface
	slot            *interfaces.Slot
	plug            *interfaces.Plug
	testSessionSlot *interfaces.Slot
	testSystemSlot  *interfaces.Slot
}

var _ = Suite(&DbusAppInterfaceSuite{
	iface: &builtin.DbusAppInterface{},
})

func (s *DbusAppInterfaceSuite) SetUpTest(c *C) {
	info, err := snap.InfoFromSnapYaml([]byte(`
name: test-dbus-app
slots:
  test-slot:
    interface: dbus-app
    session:
    - org.test-slot
  test-session:
    interface: dbus-app
    session:
    - org.test-session1
    - org.test-session2
  test-system:
    interface: dbus-app
    system:
    - org.test-system
plugs:
  test-plug:
    interface: dbus-app
    session:
    - org.test-slot

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
  test-consumer:
    plugs:
    - test-plug
`))
	c.Assert(err, IsNil)
	s.plug = &interfaces.Plug{PlugInfo: info.Plugs["test-plug"]}
	s.slot = &interfaces.Slot{SlotInfo: info.Slots["test-slot"]}
	s.testSessionSlot = &interfaces.Slot{SlotInfo: info.Slots["test-session"]}
	s.testSystemSlot = &interfaces.Slot{SlotInfo: info.Slots["test-system"]}
}

func (s *DbusAppInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "dbus-app")
}

func (s *DbusAppInterfaceSuite) TestUnusedSecuritySystems(c *C) {
	systems := [...]interfaces.SecuritySystem{interfaces.SecurityDBus,
		interfaces.SecurityAppArmor, interfaces.SecuritySecComp,
		interfaces.SecurityUDev, interfaces.SecurityMount}
	for _, system := range systems {
		snippet, err := s.iface.ConnectedSlotSnippet(s.plug, s.slot, system)
		c.Assert(err, IsNil)
		c.Assert(snippet, IsNil)

		snippet, err = s.iface.PermanentPlugSnippet(s.plug, system)
		c.Assert(err, IsNil)
		c.Assert(snippet, IsNil)

		snippet, err = s.iface.ConnectedPlugSnippet(s.plug, s.slot, system)
		c.Assert(err, IsNil)
		c.Assert(snippet, IsNil)
	}

	snippet, err := s.iface.PermanentSlotSnippet(s.slot, interfaces.SecurityDBus)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)

	snippet, err = s.iface.PermanentSlotSnippet(s.slot, interfaces.SecurityUDev)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)

	snippet, err = s.iface.PermanentSlotSnippet(s.slot, interfaces.SecurityMount)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
}

func (s *DbusAppInterfaceSuite) TestUsedSecuritySystems(c *C) {
	systems := [...]interfaces.SecuritySystem{interfaces.SecurityAppArmor,
		interfaces.SecuritySecComp}
	for _, system := range systems {
		snippet, err := s.iface.PermanentSlotSnippet(s.slot, system)
		c.Assert(err, IsNil)
		c.Assert(snippet, Not(IsNil))
	}
}

func (s *DbusAppInterfaceSuite) TestUnexpectedSecuritySystems(c *C) {
	snippet, err := s.iface.PermanentPlugSnippet(s.plug, "foo")
	c.Assert(err, Equals, interfaces.ErrUnknownSecurity)
	c.Assert(snippet, IsNil)
	snippet, err = s.iface.ConnectedPlugSnippet(s.plug, s.slot, "foo")
	c.Assert(err, Equals, interfaces.ErrUnknownSecurity)
	c.Assert(snippet, IsNil)
	snippet, err = s.iface.PermanentSlotSnippet(s.slot, "foo")
	c.Assert(err, Equals, interfaces.ErrUnknownSecurity)
	c.Assert(snippet, IsNil)
	snippet, err = s.iface.ConnectedSlotSnippet(s.plug, s.slot, "foo")
	c.Assert(err, Equals, interfaces.ErrUnknownSecurity)
	c.Assert(snippet, IsNil)
}

func (s *DbusAppInterfaceSuite) TestGetBusNamesSession(c *C) {
	var mockSnapYaml = []byte(`name: dbus-app-snap
version: 1.0
slots:
 dbus-app-slot:
  interface: dbus-app
  session:
  - org.dbus-app-snap.session-1
  - org.dbus-app-snap.session-2
`)

	info, err := snap.InfoFromSnapYaml(mockSnapYaml)
	c.Assert(err, IsNil)

	slot := &interfaces.Slot{SlotInfo: info.Slots["dbus-app-slot"]}
	iface := &builtin.DbusAppInterface{}
	names, err := iface.GetBusNames(slot.Attrs)
	c.Assert(err, IsNil)
	c.Assert(names, Not(IsNil))
}

func (s *DbusAppInterfaceSuite) TestGetBusNamesSystem(c *C) {
	var mockSnapYaml = []byte(`name: dbus-app-snap
version: 1.0
slots:
 dbus-app-slot:
  interface: dbus-app
  system:
  - org.dbus-app-snap.system-1
  - org.dbus-app-snap.system-2
`)

	info, err := snap.InfoFromSnapYaml(mockSnapYaml)
	c.Assert(err, IsNil)

	slot := &interfaces.Slot{SlotInfo: info.Slots["dbus-app-slot"]}
	iface := &builtin.DbusAppInterface{}
	names, err := iface.GetBusNames(slot.Attrs)
	c.Assert(err, IsNil)
	c.Assert(names, Not(IsNil))
}

func (s *DbusAppInterfaceSuite) TestGetBusNamesFull(c *C) {
	var mockSnapYaml = []byte(`name: dbus-app-snap
version: 1.0
slots:
 dbus-app-slot:
  interface: dbus-app
  system:
  - org.dbus-app-snap.foo.bar.baz.n0rf_qux
`)

	info, err := snap.InfoFromSnapYaml(mockSnapYaml)
	c.Assert(err, IsNil)

	slot := &interfaces.Slot{SlotInfo: info.Slots["dbus-app-slot"]}
	iface := &builtin.DbusAppInterface{}
	names, err := iface.GetBusNames(slot.Attrs)
	c.Assert(err, IsNil)
	c.Assert(names, Not(IsNil))
}

func (s *DbusAppInterfaceSuite) TestGetBusNamesNonexistentBus(c *C) {
	var mockSnapYaml = []byte(`name: dbus-app-snap
version: 1.0
slots:
 dbus-app-slot:
  interface: dbus-app
  nonexistent:
  - org.dbus-app-snap
`)

	info, err := snap.InfoFromSnapYaml(mockSnapYaml)
	c.Assert(err, IsNil)

	slot := &interfaces.Slot{SlotInfo: info.Slots["dbus-app-slot"]}
	iface := &builtin.DbusAppInterface{}
	names, err := iface.GetBusNames(slot.Attrs)
	c.Assert(err, ErrorMatches, "bus must be one of 'session' or 'system'")
	c.Assert(names, IsNil)
}

func (s *DbusAppInterfaceSuite) TestGetBusNamesMissingName(c *C) {
	var mockSnapYaml = []byte(`name: dbus-app-snap
version: 1.0
slots:
 dbus-app-slot:
  interface: dbus-app
  session: null
`)

	info, err := snap.InfoFromSnapYaml(mockSnapYaml)
	c.Assert(err, IsNil)

	slot := &interfaces.Slot{SlotInfo: info.Slots["dbus-app-slot"]}
	iface := &builtin.DbusAppInterface{}
	names, err := iface.GetBusNames(slot.Attrs)
	c.Assert(err, ErrorMatches, "bus attribute is not a list")
	c.Assert(names, IsNil)
}

func (s *DbusAppInterfaceSuite) TestGetBusNamesEmptyName(c *C) {
	var mockSnapYaml = []byte(`name: dbus-app-snap
version: 1.0
slots:
 dbus-app-slot:
  interface: dbus-app
  session:
  - ""
`)

	info, err := snap.InfoFromSnapYaml(mockSnapYaml)
	c.Assert(err, IsNil)

	slot := &interfaces.Slot{SlotInfo: info.Slots["dbus-app-slot"]}
	iface := &builtin.DbusAppInterface{}
	names, err := iface.GetBusNames(slot.Attrs)
	c.Assert(err, ErrorMatches, "bus name must be set")
	c.Assert(names, IsNil)
}

func (s *DbusAppInterfaceSuite) TestGetBusNamesNameTooLong(c *C) {
	long_name := make([]byte, 256)
	for i := range long_name {
		long_name[i] = 'b'
	}
	// make it look otherwise valid (a.bbbb...)
	long_name[0] = 'a'
	long_name[1] = '.'

	var mockSnapYaml = []byte(`name: dbus-app-snap
version: 1.0
slots:
 dbus-app-slot:
  interface: dbus-app
  session:
  - `)
	mockSnapYaml = append(mockSnapYaml, long_name...)
	mockSnapYaml = append(mockSnapYaml, "\n"...)

	info, err := snap.InfoFromSnapYaml(mockSnapYaml)
	c.Assert(err, IsNil)

	slot := &interfaces.Slot{SlotInfo: info.Slots["dbus-app-slot"]}
	iface := &builtin.DbusAppInterface{}
	names, err := iface.GetBusNames(slot.Attrs)
	c.Assert(err, ErrorMatches, "bus name is too long \\(must be <= 255\\)")
	c.Assert(names, IsNil)
}

func (s *DbusAppInterfaceSuite) TestGetBusNamesNameStartsWithColon(c *C) {
	var mockSnapYaml = []byte(`name: dbus-app-snap
version: 1.0
slots:
 dbus-app-slot:
  interface: dbus-app
  session:
  - :dbus-app-snap.bar
`)

	info, err := snap.InfoFromSnapYaml(mockSnapYaml)
	c.Assert(err, IsNil)

	slot := &interfaces.Slot{SlotInfo: info.Slots["dbus-app-slot"]}
	iface := &builtin.DbusAppInterface{}
	names, err := iface.GetBusNames(slot.Attrs)
	c.Assert(err, ErrorMatches, "invalid bus name: \":dbus-app-snap.bar\"")
	c.Assert(names, IsNil)
}

func (s *DbusAppInterfaceSuite) TestGetBusNamesNameStartsWithNum(c *C) {
	var mockSnapYaml = []byte(`name: dbus-app-snap
version: 1.0
slots:
 dbus-app-slot:
  interface: dbus-app
  session:
  - 0dbus-app-snap.bar
`)

	info, err := snap.InfoFromSnapYaml(mockSnapYaml)
	c.Assert(err, IsNil)

	slot := &interfaces.Slot{SlotInfo: info.Slots["dbus-app-slot"]}
	iface := &builtin.DbusAppInterface{}
	names, err := iface.GetBusNames(slot.Attrs)
	c.Assert(err, ErrorMatches, "invalid bus name: \"0dbus-app-snap.bar\"")
	c.Assert(names, IsNil)
}

func (s *DbusAppInterfaceSuite) TestGetBusNamesNameMissingDot(c *C) {
	var mockSnapYaml = []byte(`name: dbus-app-snap
version: 1.0
slots:
 dbus-app-slot:
  interface: dbus-app
  session:
  - dbus-app-snap
`)

	info, err := snap.InfoFromSnapYaml(mockSnapYaml)
	c.Assert(err, IsNil)

	slot := &interfaces.Slot{SlotInfo: info.Slots["dbus-app-slot"]}
	iface := &builtin.DbusAppInterface{}
	names, err := iface.GetBusNames(slot.Attrs)
	c.Assert(err, ErrorMatches, "invalid bus name: \"dbus-app-snap\"")
	c.Assert(names, IsNil)
}

func (s *DbusAppInterfaceSuite) TestGetBusNamesNameMissingElement(c *C) {
	var mockSnapYaml = []byte(`name: dbus-app-snap
version: 1.0
slots:
 dbus-app-slot:
  interface: dbus-app
  session:
  - dbus-app-snap.
`)

	info, err := snap.InfoFromSnapYaml(mockSnapYaml)
	c.Assert(err, IsNil)

	slot := &interfaces.Slot{SlotInfo: info.Slots["dbus-app-slot"]}
	iface := &builtin.DbusAppInterface{}
	names, err := iface.GetBusNames(slot.Attrs)
	c.Assert(err, ErrorMatches, "invalid bus name: \"dbus-app-snap\\.\"")
	c.Assert(names, IsNil)
}

// most of SanitizePlug and SanitizeSlot is GetBusNames(), so just do a cursory
// test for these
func (s *DbusAppInterfaceSuite) TestSanitizeSlotSystem(c *C) {
	var mockSnapYaml = []byte(`name: dbus-app-snap
version: 1.0
slots:
 dbus-app-slot:
  interface: dbus-app
  session:
  - org.dbus-app-snap.system-1
  - org.dbus-app-snap.system-2
`)

	info, err := snap.InfoFromSnapYaml(mockSnapYaml)
	c.Assert(err, IsNil)

	slot := &interfaces.Slot{SlotInfo: info.Slots["dbus-app-slot"]}
	err = s.iface.SanitizeSlot(slot)
	c.Assert(err, IsNil)
}

/*
func (s *DbusAppInterfaceSuite) TestSanitizePlugSession(c *C) {
	var mockSnapYaml = []byte(`name: dbus-app-snap
version: 1.0
plugs:
 dbus-app-plug:
  interface: dbus-app
  session:
  - org.dbus-app-snap.session-1
  - org.dbus-app-snap.session-2
`)

	info, err := snap.InfoFromSnapYaml(mockSnapYaml)
	c.Assert(err, IsNil)

	plug := &interfaces.Plug{PlugInfo: info.Plugs["dbus-app-plug"]}
	err = s.iface.SanitizePlug(plug)
	c.Assert(err, IsNil)
}
*/

func (s *DbusAppInterfaceSuite) TestPermanentSlotAppArmorSession(c *C) {
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

func (s *DbusAppInterfaceSuite) TestPermanentSlotAppArmorSessionNative(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()
	iface := &builtin.DbusAppInterface{}
	snippet, err := iface.PermanentSlotSnippet(s.testSessionSlot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, Not(IsNil))

	// verify classic rule not present
	c.Check(string(snippet), Not(testutil.Contains), "# allow unconfined clients talk to org.test-session1 on classic\n")
}

func (s *DbusAppInterfaceSuite) TestPermanentSlotAppArmorSessionClassic(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()
	iface := &builtin.DbusAppInterface{}
	snippet, err := iface.PermanentSlotSnippet(s.testSessionSlot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, Not(IsNil))

	// verify classic rule
	c.Check(string(snippet), testutil.Contains, "# allow unconfined clients talk to org.test-session1 on classic\n")
}

func (s *DbusAppInterfaceSuite) TestPermanentSlotAppArmorSystem(c *C) {
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

func (s *DbusAppInterfaceSuite) TestPermanentSlotSeccomp(c *C) {
	snippet, err := s.iface.PermanentSlotSnippet(s.testSessionSlot, interfaces.SecuritySecComp)
	c.Assert(err, IsNil)
	c.Assert(snippet, Not(IsNil))

	c.Check(string(snippet), testutil.Contains, "getsockname\n")
}

func (s *DbusAppInterfaceSuite) TestAutoConnect(c *C) {
	iface := &builtin.DbusAppInterface{}
	c.Check(iface.AutoConnect(), Equals, false)
}
