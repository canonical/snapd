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

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
)

type EDSInterfaceSuite struct {
	iface interfaces.Interface

	calendarPlug           *interfaces.Plug
	contactPlug            *interfaces.Plug
	calendarAndContactPlug *interfaces.Plug
	missingServicePlug     *interfaces.Plug
	badServicePlug         *interfaces.Plug
	calendarSyncPlug       *interfaces.Plug
	contactSyncPlug        *interfaces.Plug
	allPlug                *interfaces.Plug

	slot             *interfaces.Slot
	badInterfaceSlot *interfaces.Slot
}

var _ = Suite(&EDSInterfaceSuite{
	iface: &builtin.EDSInterface{},
})

func (s *EDSInterfaceSuite) SetUpTest(c *C) {
	info := snaptest.MockInfo(c, `
name: ubuntu-core
plugs:
    eds-calendar:
        interface: eds
        services: [calendar]
    eds-contact:
        interface: eds
        services: [contact]
    eds-calendar-contact:
        interface: eds
        services: [calendar, contact]
    eds-missing-service:
        interface: eds
    eds-bad-service:
        interface: eds
        services: [badService]
    eds-contact-sync:
        interface: eds
        services: [contact-sync]
    eds-calendar-sync:
        interface: eds
        services: [calendar-sync]
    eds-all:
        interface: eds
        services: [calendar, contact, calendar-sync, contact-sync]
slots:
    eds-slot: eds
`, &snap.SideInfo{})
	s.calendarPlug = &interfaces.Plug{PlugInfo: info.Plugs["eds-calendar"]}
	s.contactPlug = &interfaces.Plug{PlugInfo: info.Plugs["eds-contact"]}
	s.calendarAndContactPlug = &interfaces.Plug{PlugInfo: info.Plugs["eds-calendar-contact"]}
	s.missingServicePlug = &interfaces.Plug{PlugInfo: info.Plugs["eds-missing-service"]}
	s.calendarSyncPlug = &interfaces.Plug{PlugInfo: info.Plugs["eds-calendar-sync"]}
	s.contactSyncPlug = &interfaces.Plug{PlugInfo: info.Plugs["eds-contact-sync"]}
	s.allPlug = &interfaces.Plug{PlugInfo: info.Plugs["eds-all"]}
	s.badServicePlug = &interfaces.Plug{PlugInfo: info.Plugs["eds-bad-service"]}
	s.slot = &interfaces.Slot{SlotInfo: info.Slots["eds-slot"]}
}

func (s *EDSInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "eds")
}

func (s *EDSInterfaceSuite) TestSanitizeSlot(c *C) {
	err := s.iface.SanitizeSlot(s.slot)
	c.Assert(err, IsNil)
}

func (s *EDSInterfaceSuite) TestSanitizePlug(c *C) {
	// valid plugs
	err := s.iface.SanitizePlug(s.calendarPlug)
	c.Assert(err, IsNil)
	err = s.iface.SanitizePlug(s.contactPlug)
	c.Assert(err, IsNil)
	err = s.iface.SanitizePlug(s.calendarAndContactPlug)
	c.Assert(err, IsNil)
	err = s.iface.SanitizePlug(s.calendarSyncPlug)
	c.Assert(err, IsNil)
	err = s.iface.SanitizePlug(s.contactSyncPlug)
	c.Assert(err, IsNil)
	err = s.iface.SanitizePlug(s.allPlug)
	c.Assert(err, IsNil)

	// Plugs without the "services" attribute are rejected.
	err = s.iface.SanitizePlug(s.missingServicePlug)
	c.Assert(err, ErrorMatches,
		"eds must contain the services attribute")

	// Plugs with incorrect value of the "services" attribute are rejected.
	err = s.iface.SanitizePlug(s.badServicePlug)
	c.Assert(err, ErrorMatches,
		"invalid 'service' value")
}

func (s *EDSInterfaceSuite) TestPermanentPlugSecurityDoesNotContainSlotSecurity(c *C) {
	var err error
	var slotSnippet, plugSnippet []byte
	plugSnippet, err = s.iface.PermanentPlugSnippet(s.calendarPlug, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	slotSnippet, err = s.iface.PermanentSlotSnippet(s.slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	// Ensure that we don't accidentally give plug-side permissions to slot-side.
	c.Assert(bytes.Contains(plugSnippet, slotSnippet), Equals, false)
}

func (s *EDSInterfaceSuite) TestConnectedPlugSnippetPanicksOnUnsanitizedSlots(c *C) {
	// Unsanitized slots should never be used and cause a panic.
	c.Assert(func() {
		s.iface.ConnectedPlugSnippet(s.missingServicePlug, s.slot, interfaces.SecurityAppArmor)
	}, PanicMatches, "slot is not sanitized")
}

func (s *EDSInterfaceSuite) TestConnectedPlugSnippet(c *C) {
	var calendarSnippet = []byte("path=/org/gnome/evolution/dataserver/CalendarFactory")
	var contactSnippet = []byte("path=/org/gnome/evolution/dataserver/AddressBookFactory")
	var calendarSyncSnippet = []byte("path=/com/canonical/SyncMonitor{,/**}")
	var contactSyncSnippet = []byte("path=/synchronizer{,/**}")

	// No contacts permissions for calendar plug
	snippet, err := s.iface.ConnectedPlugSnippet(s.calendarPlug, s.slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(bytes.Contains(snippet, calendarSnippet), Equals, true)
	c.Assert(bytes.Contains(snippet, contactSnippet), Equals, false)
	c.Assert(bytes.Contains(snippet, calendarSyncSnippet), Equals, false)
	c.Assert(bytes.Contains(snippet, contactSyncSnippet), Equals, false)

	// No calendar permissions for contact plug
	snippet, err = s.iface.ConnectedPlugSnippet(s.contactPlug, s.slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(bytes.Contains(snippet, calendarSnippet), Equals, false)
	c.Assert(bytes.Contains(snippet, contactSnippet), Equals, true)
	c.Assert(bytes.Contains(snippet, calendarSyncSnippet), Equals, false)
	c.Assert(bytes.Contains(snippet, contactSyncSnippet), Equals, false)

	// Both permissions
	snippet, err = s.iface.ConnectedPlugSnippet(s.calendarAndContactPlug, s.slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(bytes.Contains(snippet, calendarSnippet), Equals, true)
	c.Assert(bytes.Contains(snippet, contactSnippet), Equals, true)
	c.Assert(bytes.Contains(snippet, calendarSyncSnippet), Equals, false)
	c.Assert(bytes.Contains(snippet, contactSyncSnippet), Equals, false)

	// only calendar sync
	snippet, err = s.iface.ConnectedPlugSnippet(s.calendarSyncPlug, s.slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(bytes.Contains(snippet, calendarSnippet), Equals, false)
	c.Assert(bytes.Contains(snippet, contactSnippet), Equals, false)
	c.Assert(bytes.Contains(snippet, calendarSyncSnippet), Equals, true)
	c.Assert(bytes.Contains(snippet, contactSyncSnippet), Equals, false)

	// only contact sync
	snippet, err = s.iface.ConnectedPlugSnippet(s.contactSyncPlug, s.slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(bytes.Contains(snippet, calendarSnippet), Equals, false)
	c.Assert(bytes.Contains(snippet, contactSnippet), Equals, false)
	c.Assert(bytes.Contains(snippet, calendarSyncSnippet), Equals, false)
	c.Assert(bytes.Contains(snippet, contactSyncSnippet), Equals, true)

	// all
	snippet, err = s.iface.ConnectedPlugSnippet(s.allPlug, s.slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(bytes.Contains(snippet, calendarSnippet), Equals, true)
	c.Assert(bytes.Contains(snippet, contactSnippet), Equals, true)
	c.Assert(bytes.Contains(snippet, calendarSyncSnippet), Equals, true)
	c.Assert(bytes.Contains(snippet, contactSyncSnippet), Equals, true)
}

func (s *EDSInterfaceSuite) TestLegacyAutoConnect(c *C) {
	c.Check(s.iface.LegacyAutoConnect(), Equals, false)
}
