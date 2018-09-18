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
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/dbus"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type DbusInterfaceSuite struct {
	testutil.BaseTest
	iface interfaces.Interface

	snapInfo *snap.Info

	sessionPlugInfo          *snap.PlugInfo
	sessionPlug              *interfaces.ConnectedPlug
	systemPlugInfo           *snap.PlugInfo
	systemPlug               *interfaces.ConnectedPlug
	connectedSessionPlugInfo *snap.PlugInfo
	connectedSessionPlug     *interfaces.ConnectedPlug
	connectedSystemPlugInfo  *snap.PlugInfo
	connectedSystemPlug      *interfaces.ConnectedPlug

	sessionSlotInfo          *snap.SlotInfo
	sessionSlot              *interfaces.ConnectedSlot
	systemSlotInfo           *snap.SlotInfo
	systemSlot               *interfaces.ConnectedSlot
	connectedSessionSlotInfo *snap.SlotInfo
	connectedSessionSlot     *interfaces.ConnectedSlot
	connectedSystemSlotInfo  *snap.SlotInfo
	connectedSystemSlot      *interfaces.ConnectedSlot
}

var _ = Suite(&DbusInterfaceSuite{
	iface: builtin.MustInterface("dbus"),
})

func (s *DbusInterfaceSuite) SetUpSuite(c *C) {
	s.snapInfo = snaptest.MockInfo(c, `
name: test-dbus
version: 0
slots:
  test-session-slot:
    interface: dbus
    bus: session
    name: org.test-session-slot
  test-system-slot:
    interface: dbus
    bus: system
    name: org.test-system-slot
  test-system-connected-slot:
    interface: dbus
    bus: system
    name: org.test-system-connected
  test-session-connected-slot:
    interface: dbus
    bus: session
    name: org.test-session-connected

plugs:
  test-session-plug:
    interface: dbus
    bus: session
    name: org.test-session-plug
  test-system-plug:
    interface: dbus
    bus: system
    name: org.test-system-plug
  test-system-connected-plug:
    interface: dbus
    bus: system
    name: org.test-system-connected
  test-session-connected-plug:
    interface: dbus
    bus: session
    name: org.test-session-connected

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
`, nil)
}

func (s *DbusInterfaceSuite) SetUpTest(c *C) {
	s.sessionSlotInfo = s.snapInfo.Slots["test-session-slot"]
	s.sessionSlot = interfaces.NewConnectedSlot(s.sessionSlotInfo, nil)
	s.systemSlotInfo = s.snapInfo.Slots["test-system-slot"]
	s.systemSlot = interfaces.NewConnectedSlot(s.systemSlotInfo, nil)
	s.connectedSessionSlotInfo = s.snapInfo.Slots["test-session-connected-slot"]
	s.connectedSessionSlot = interfaces.NewConnectedSlot(s.connectedSessionSlotInfo, nil)
	s.connectedSystemSlotInfo = s.snapInfo.Slots["test-system-connected-slot"]
	s.connectedSystemSlot = interfaces.NewConnectedSlot(s.connectedSystemSlotInfo, nil)

	s.sessionPlugInfo = s.snapInfo.Plugs["test-session-plug"]
	s.sessionPlug = interfaces.NewConnectedPlug(s.sessionPlugInfo, nil)
	s.systemPlugInfo = s.snapInfo.Plugs["test-system-plug"]
	s.systemPlug = interfaces.NewConnectedPlug(s.systemPlugInfo, nil)
	s.connectedSessionPlugInfo = s.snapInfo.Plugs["test-session-connected-plug"]
	s.connectedSessionPlug = interfaces.NewConnectedPlug(s.connectedSessionPlugInfo, nil)
	s.connectedSystemPlugInfo = s.snapInfo.Plugs["test-system-connected-plug"]
	s.connectedSystemPlug = interfaces.NewConnectedPlug(s.connectedSystemPlugInfo, nil)
}

func (s *DbusInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "dbus")
}

func (s *DbusInterfaceSuite) TestValidSessionBusName(c *C) {
	var mockSnapYaml = `name: dbus-snap
version: 1.0
slots:
 dbus-slot:
  interface: dbus
  bus: session
  name: org.dbus-snap.session-a
`

	info := snaptest.MockInfo(c, mockSnapYaml, nil)

	slot := info.Slots["dbus-slot"]
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), IsNil)
}

func (s *DbusInterfaceSuite) TestValidSystemBusName(c *C) {
	var mockSnapYaml = `name: dbus-snap
version: 1.0
slots:
 dbus-slot:
  interface: dbus
  bus: system
  name: org.dbus-snap.system-a
`

	info := snaptest.MockInfo(c, mockSnapYaml, nil)

	slot := info.Slots["dbus-slot"]
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), IsNil)
}

func (s *DbusInterfaceSuite) TestValidFullBusName(c *C) {
	var mockSnapYaml = `name: dbus-snap
version: 1.0
slots:
 dbus-slot:
  interface: dbus
  bus: system
  name: org.dbus-snap.foo.bar.baz.n0rf_qux
`

	info := snaptest.MockInfo(c, mockSnapYaml, nil)

	slot := info.Slots["dbus-slot"]
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), IsNil)
}

func (s *DbusInterfaceSuite) TestNonexistentBusName(c *C) {
	var mockSnapYaml = `name: dbus-snap
version: 1.0
slots:
 dbus-slot:
  interface: dbus
  bus: nonexistent
  name: org.dbus-snap
`

	info := snaptest.MockInfo(c, mockSnapYaml, nil)

	slot := info.Slots["dbus-slot"]
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches, "bus 'nonexistent' must be one of 'session' or 'system'")
}

// If this test is failing, be sure to verify the AppArmor rules for binding to
// a well-known name to avoid overlaps.
func (s *DbusInterfaceSuite) TestInvalidBusNameEndsWithDashInt(c *C) {
	var mockSnapYaml = `name: dbus-snap
version: 1.0
slots:
 dbus-slot:
  interface: dbus
  bus: session
  name: org.dbus-snap.session-12345
`

	info := snaptest.MockInfo(c, mockSnapYaml, nil)

	slot := info.Slots["dbus-slot"]
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches, "DBus bus name must not end with -NUMBER")
}

func (s *DbusInterfaceSuite) TestSanitizeSlotSystem(c *C) {
	var mockSnapYaml = `name: dbus-snap
version: 1.0
slots:
 dbus-slot:
  interface: dbus
  bus: system
  name: org.dbus-snap.system
`

	info := snaptest.MockInfo(c, mockSnapYaml, nil)

	slot := info.Slots["dbus-slot"]
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), IsNil)
}

func (s *DbusInterfaceSuite) TestSanitizeSlotSession(c *C) {
	var mockSnapYaml = `name: dbus-snap
version: 1.0
slots:
 dbus-slot:
  interface: dbus
  bus: session
  name: org.dbus-snap.session
`

	info := snaptest.MockInfo(c, mockSnapYaml, nil)

	slot := info.Slots["dbus-slot"]
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), IsNil)
}

func (s *DbusInterfaceSuite) TestSanitizePlugSystem(c *C) {
	var mockSnapYaml = `name: dbus-snap
version: 1.0
plugs:
 dbus-plug:
  interface: dbus
  bus: system
  name: org.dbus-snap.system
`

	info := snaptest.MockInfo(c, mockSnapYaml, nil)

	plug := info.Plugs["dbus-plug"]
	c.Assert(interfaces.BeforePreparePlug(s.iface, plug), IsNil)
}

func (s *DbusInterfaceSuite) TestSanitizePlugSession(c *C) {
	var mockSnapYaml = `name: dbus-snap
version: 1.0
plugs:
 dbus-plug:
  interface: dbus
  bus: session
  name: org.dbus-snap.session
`

	info := snaptest.MockInfo(c, mockSnapYaml, nil)

	plug := info.Plugs["dbus-plug"]
	c.Assert(interfaces.BeforePreparePlug(s.iface, plug), IsNil)
}

func (s *DbusInterfaceSuite) TestPermanentSlotAppArmorSession(c *C) {
	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddPermanentSlot(s.iface, s.sessionSlotInfo)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.test-dbus.test-session-provider"})
	snippet := apparmorSpec.SnippetForTag("snap.test-dbus.test-session-provider")

	// verify abstraction rule
	c.Check(snippet, testutil.Contains, "#include <abstractions/dbus-session-strict>\n")

	// verify shared permanent slot policy
	c.Check(snippet, testutil.Contains, "dbus (send)\n    bus=session\n    path=/org/freedesktop/DBus\n    interface=org.freedesktop.DBus\n    member=\"{Request,Release}Name\"\n    peer=(name=org.freedesktop.DBus, label=unconfined),\n")

	// verify individual bind rules
	c.Check(snippet, testutil.Contains, "dbus (bind)\n    bus=session\n    name=org.test-session-slot,\n")

	// verify individual path in rules
	c.Check(snippet, testutil.Contains, "path=\"/org/test-session-slot{,/**}\"\n")

	// verify interface in rule
	c.Check(snippet, testutil.Contains, "interface=\"org.test-session-slot{,.*}\"\n")
}

func (s *DbusInterfaceSuite) TestPermanentSlotAppArmorSessionNative(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddPermanentSlot(s.iface, s.sessionSlotInfo)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.test-dbus.test-session-provider"})

	// verify classic rule not present
	c.Check(apparmorSpec.SnippetForTag("snap.test-dbus.test-session-provider"), Not(testutil.Contains), "# allow us to respond to unconfined clients via \"org.test-session-slot{,.*}\"\n")
}

func (s *DbusInterfaceSuite) TestPermanentSlotAppArmorSessionClassic(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddPermanentSlot(s.iface, s.sessionSlotInfo)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.test-dbus.test-session-provider"})

	// verify classic rule
	c.Check(apparmorSpec.SnippetForTag("snap.test-dbus.test-session-provider"), testutil.Contains, "# allow us to respond to unconfined clients via \"org.test-session-slot{,.*}\"\n")
}

func (s *DbusInterfaceSuite) TestPermanentSlotAppArmorSystem(c *C) {
	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddPermanentSlot(s.iface, s.systemSlotInfo)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.test-dbus.test-system-provider"})
	snippet := apparmorSpec.SnippetForTag("snap.test-dbus.test-system-provider")

	// verify abstraction rule
	c.Check(snippet, testutil.Contains, "#include <abstractions/dbus-strict>\n")

	// verify bind rule
	c.Check(snippet, testutil.Contains, "dbus (bind)\n    bus=system\n    name=org.test-system-slot,\n")

	// verify path in rule
	c.Check(snippet, testutil.Contains, "path=\"/org/test-system-slot{,/**}\"\n")

	// verify interface in rule
	c.Check(snippet, testutil.Contains, "interface=\"org.test-system-slot{,.*}\"\n")

	// verify dbus-daemon introspection rule
	c.Check(snippet, testutil.Contains, "dbus (send)\n    bus=system\n    interface=org.freedesktop.DBus.Introspectable\n    member=Introspect\n    peer=(name=org.freedesktop.DBus, label=unconfined),\n")
}

func (s *DbusInterfaceSuite) TestPermanentSlotDBusSession(c *C) {
	dbusSpec := &dbus.Specification{}
	err := dbusSpec.AddPermanentSlot(s.iface, s.sessionSlotInfo)
	c.Assert(err, IsNil)
	c.Assert(dbusSpec.SecurityTags(), HasLen, 0)
}

func (s *DbusInterfaceSuite) TestPermanentSlotDBusSystem(c *C) {
	dbusSpec := &dbus.Specification{}
	err := dbusSpec.AddPermanentSlot(s.iface, s.systemSlotInfo)
	c.Assert(err, IsNil)
	c.Assert(dbusSpec.SecurityTags(), DeepEquals, []string{"snap.test-dbus.test-system-provider"})
	snippet := dbusSpec.SnippetForTag("snap.test-dbus.test-system-provider")
	c.Check(snippet, testutil.Contains, "<policy user=\"root\">\n    <allow own=\"org.test-system-slot\"/>")
	c.Check(snippet, testutil.Contains, "<policy context=\"default\">\n    <allow send_destination=\"org.test-system-slot\"/>")
}

func (s *DbusInterfaceSuite) TestPermanentSlotSecCompSystem(c *C) {
	seccompSpec := &seccomp.Specification{}
	err := seccompSpec.AddPermanentSlot(s.iface, s.systemSlotInfo)
	c.Assert(err, IsNil)
	c.Assert(seccompSpec.SecurityTags(), DeepEquals, []string{"snap.test-dbus.test-system-provider"})
	snippet := seccompSpec.SnippetForTag("snap.test-dbus.test-system-provider")
	c.Check(snippet, testutil.Contains, "listen\naccept\naccept4\n")
}

func (s *DbusInterfaceSuite) TestPermanentSlotSecCompSession(c *C) {
	seccompSpec := &seccomp.Specification{}
	err := seccompSpec.AddPermanentSlot(s.iface, s.sessionSlotInfo)
	c.Assert(err, IsNil)
	c.Assert(seccompSpec.SecurityTags(), DeepEquals, []string{"snap.test-dbus.test-session-provider"})
	snippet := seccompSpec.SnippetForTag("snap.test-dbus.test-session-provider")
	c.Check(snippet, testutil.Contains, "listen\naccept\naccept4\n")
}

func (s *DbusInterfaceSuite) TestConnectedSlotAppArmorSession(c *C) {
	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedSlot(s.iface, s.connectedSessionPlug, s.connectedSessionSlot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.test-dbus.test-session-consumer", "snap.test-dbus.test-session-provider", "snap.test-dbus.test-system-consumer", "snap.test-dbus.test-system-provider"})
	snippet := apparmorSpec.SnippetForTag("snap.test-dbus.test-session-provider")

	// verify introspectable rule
	c.Check(snippet, testutil.Contains, "dbus (receive)\n    bus=session\n    interface=org.freedesktop.DBus.Introspectable\n    member=Introspect\n    peer=(label=\"snap.test-dbus.*\"),\n")

	// verify bind rule not present
	c.Check(snippet, Not(testutil.Contains), "dbus (bind)")

	// verify individual path in rules
	c.Check(snippet, testutil.Contains, "path=\"/org/test-session-connected{,/**}\"\n")

	// verify interface in rule
	c.Check(snippet, testutil.Contains, "interface=\"org.test-session-connected{,.*}\"\n")
}

func (s *DbusInterfaceSuite) TestConnectedSlotAppArmorSystem(c *C) {
	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedSlot(s.iface, s.connectedSystemPlug, s.connectedSystemSlot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.test-dbus.test-session-consumer", "snap.test-dbus.test-session-provider", "snap.test-dbus.test-system-consumer", "snap.test-dbus.test-system-provider"})
	snippet := apparmorSpec.SnippetForTag("snap.test-dbus.test-session-provider")

	// verify introspectable rule
	c.Check(snippet, testutil.Contains, "dbus (receive)\n    bus=system\n    interface=org.freedesktop.DBus.Introspectable\n    member=Introspect\n    peer=(label=\"snap.test-dbus.*\"),\n")

	// verify bind rule not present
	c.Check(snippet, Not(testutil.Contains), "dbus (bind)")

	// verify individual path in rules
	c.Check(snippet, testutil.Contains, "path=\"/org/test-system-connected{,/**}\"\n")

	// verify interface in rule
	c.Check(snippet, testutil.Contains, "interface=\"org.test-system-connected{,.*}\"\n")
}

func (s *DbusInterfaceSuite) TestConnectedPlugAppArmorSession(c *C) {
	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, s.connectedSessionPlug, s.connectedSessionSlot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.test-dbus.test-session-consumer", "snap.test-dbus.test-session-provider", "snap.test-dbus.test-system-consumer", "snap.test-dbus.test-system-provider"})
	snippet := apparmorSpec.SnippetForTag("snap.test-dbus.test-session-consumer")

	// verify introspectable rule
	c.Check(string(snippet), testutil.Contains, "dbus (send)\n    bus=session\n    interface=org.freedesktop.DBus.Introspectable\n    member=Introspect\n    peer=(label=\"snap.test-dbus.*\"),\n")

	// verify bind rule not present
	c.Check(string(snippet), Not(testutil.Contains), "dbus (bind)")

	// verify well-known connection in rule
	c.Check(string(snippet), testutil.Contains, "peer=(name=org.test-session-connected, label=")

	// verify interface in rule

	// verify individual path in rules
	c.Check(string(snippet), testutil.Contains, "path=\"/org/test-session-connected{,/**}\"\n")

	// verify interface in rule
	c.Check(string(snippet), testutil.Contains, "interface=\"org.test-session-connected{,.*}\"\n")
}

func (s *DbusInterfaceSuite) TestConnectedPlugAppArmorSystem(c *C) {
	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, s.connectedSystemPlug, s.connectedSystemSlot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.test-dbus.test-session-consumer", "snap.test-dbus.test-session-provider", "snap.test-dbus.test-system-consumer", "snap.test-dbus.test-system-provider"})
	snippet := apparmorSpec.SnippetForTag("snap.test-dbus.test-session-consumer")

	// verify introspectable rule
	c.Check(string(snippet), testutil.Contains, "dbus (send)\n    bus=system\n    interface=org.freedesktop.DBus.Introspectable\n    member=Introspect\n    peer=(label=\"snap.test-dbus.*\"),\n")

	// verify bind rule not present
	c.Check(string(snippet), Not(testutil.Contains), "dbus (bind)")

	// verify well-known connection in rule
	c.Check(string(snippet), testutil.Contains, "peer=(name=org.test-system-connected, label=")

	// verify individual path in rules
	c.Check(string(snippet), testutil.Contains, "path=\"/org/test-system-connected{,/**}\"\n")

	// verify interface in rule
	c.Check(string(snippet), testutil.Contains, "interface=\"org.test-system-connected{,.*}\"\n")
}

func (s *DbusInterfaceSuite) TestConnectionFirst(c *C) {
	const plugYaml = `name: plugger
version: 1.0
plugs:
 this:
  interface: dbus
  bus: session
  name: org.slotter.session
apps:
 app:
  command: foo
`
	const slotYaml = `name: slotter
version: 1.0
slots:
 this:
  interface: dbus
  bus: session
  name: org.slotter.session
 that:
  interface: dbus
  bus: system
  name: org.slotter.other-session
`

	plugInfo := snaptest.MockInfo(c, plugYaml, nil)
	matchingPlug := interfaces.NewConnectedPlug(plugInfo.Plugs["this"], nil)

	slotInfo := snaptest.MockInfo(c, slotYaml, nil)
	matchingSlot := interfaces.NewConnectedSlot(slotInfo.Slots["this"], nil)
	nonmatchingSlot := interfaces.NewConnectedSlot(slotInfo.Slots["that"], nil)

	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, matchingPlug, matchingSlot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.plugger.app"})
	snippet := apparmorSpec.SnippetForTag("snap.plugger.app")

	c.Check(snippet, testutil.Contains, "org.slotter.session")
	c.Check(snippet, testutil.Contains, "bus=session")
	c.Check(snippet, Not(testutil.Contains), "org.slotter.other-session")
	c.Check(snippet, Not(testutil.Contains), "bus=system")

	apparmorSpec = &apparmor.Specification{}
	err = apparmorSpec.AddConnectedPlug(s.iface, matchingPlug, nonmatchingSlot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), HasLen, 0)
}

func (s *DbusInterfaceSuite) TestConnectionSecond(c *C) {
	const plugYaml = `name: plugger
version: 1.0
plugs:
 that:
  interface: dbus
  bus: system
  name: org.slotter.other-session
apps:
 app:
  command: foo
`
	const slotYaml = `name: slotter
version: 1.0
slots:
 this:
  interface: dbus
  bus: session
  name: org.slotter.session
 that:
  interface: dbus
  bus: system
  name: org.slotter.other-session
`

	plugInfo := snaptest.MockInfo(c, plugYaml, nil)
	matchingPlug := interfaces.NewConnectedPlug(plugInfo.Plugs["that"], nil)

	slotInfo := snaptest.MockInfo(c, slotYaml, nil)
	matchingSlot := interfaces.NewConnectedSlot(slotInfo.Slots["that"], nil)
	nonmatchingSlot := interfaces.NewConnectedSlot(slotInfo.Slots["this"], nil)

	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, matchingPlug, matchingSlot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.plugger.app"})
	snippet := apparmorSpec.SnippetForTag("snap.plugger.app")

	c.Check(snippet, testutil.Contains, "org.slotter.other-session")
	c.Check(snippet, testutil.Contains, "bus=system")
	c.Check(snippet, Not(testutil.Contains), "org.slotter.session")
	c.Check(snippet, Not(testutil.Contains), "bus=session")

	apparmorSpec = &apparmor.Specification{}
	err = apparmorSpec.AddConnectedPlug(s.iface, matchingPlug, nonmatchingSlot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), HasLen, 0)
}

func (s *DbusInterfaceSuite) TestConnectionBoth(c *C) {
	const plugYaml = `name: plugger
version: 1.0
plugs:
 that:
  interface: dbus
  bus: system
  name: org.slotter.other-session
 this:
  interface: dbus
  bus: session
  name: org.slotter.session
apps:
 app:
  command: foo
`
	const slotYaml = `name: slotter
version: 1.0
slots:
 this:
  interface: dbus
  bus: session
  name: org.slotter.session
 that:
  interface: dbus
  bus: system
  name: org.slotter.other-session
`

	plugInfo := snaptest.MockInfo(c, plugYaml, nil)
	matchingPlug1 := interfaces.NewConnectedPlug(plugInfo.Plugs["this"], nil)
	matchingPlug2 := interfaces.NewConnectedPlug(plugInfo.Plugs["that"], nil)

	slotInfo := snaptest.MockInfo(c, slotYaml, nil)
	matchingSlot1 := interfaces.NewConnectedSlot(slotInfo.Slots["this"], nil)
	matchingSlot2 := interfaces.NewConnectedSlot(slotInfo.Slots["that"], nil)

	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, matchingPlug1, matchingSlot1)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.plugger.app"})
	snippet := apparmorSpec.SnippetForTag("snap.plugger.app")
	c.Check(snippet, testutil.Contains, "org.slotter.session")
	c.Check(snippet, testutil.Contains, "bus=session")

	apparmorSpec = &apparmor.Specification{}
	err = apparmorSpec.AddConnectedPlug(s.iface, matchingPlug2, matchingSlot2)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.plugger.app"})
	snippet = apparmorSpec.SnippetForTag("snap.plugger.app")
	c.Check(snippet, testutil.Contains, "org.slotter.other-session")
	c.Check(snippet, testutil.Contains, "bus=system")
}

func (s *DbusInterfaceSuite) TestConnectionMismatchBus(c *C) {
	const plugYaml = `name: plugger
version: 1.0
plugs:
 this:
  interface: dbus
  bus: session
  name: org.slotter.session
`
	const slotYaml = `name: slotter
version: 1.0
slots:
 this:
  interface: dbus
  bus: system
  name: org.slotter.session
`

	plugInfo := snaptest.MockInfo(c, plugYaml, nil)
	plug := interfaces.NewConnectedPlug(plugInfo.Plugs["this"], nil)

	slotInfo := snaptest.MockInfo(c, slotYaml, nil)
	slot := interfaces.NewConnectedSlot(slotInfo.Slots["this"], nil)

	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, plug, slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), HasLen, 0)
}

func (s *DbusInterfaceSuite) TestConnectionMismatchName(c *C) {
	const plugYaml = `name: plugger
version: 1.0
plugs:
 this:
  interface: dbus
  bus: session
  name: org.slotter.session
`
	const slotYaml = `name: slotter
version: 1.0
slots:
 this:
  interface: dbus
  bus: session
  name: org.slotter.nomatch
`

	plugInfo := snaptest.MockInfo(c, plugYaml, nil)
	plug := interfaces.NewConnectedPlug(plugInfo.Plugs["this"], nil)

	slotInfo := snaptest.MockInfo(c, slotYaml, nil)
	slot := interfaces.NewConnectedSlot(slotInfo.Slots["this"], nil)

	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, plug, slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), HasLen, 0)
}

func (s *DbusInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
