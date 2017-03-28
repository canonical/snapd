// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type NetworkStatusSuite struct {
	iface interfaces.Interface
	slot  *interfaces.Slot
	plug  *interfaces.Plug
}

var _ = Suite(&NetworkStatusSuite{iface: &builtin.NetworkStatusInterface{}})

func (s *NetworkStatusSuite) SetUpSuite(c *C) {
	const serverYaml = `name: server
version: 1.0
apps:
  app:
    command: foo
    slots: [network-status]
`
	serverInfo := snaptest.MockInfo(c, serverYaml, nil)
	s.slot = &interfaces.Slot{SlotInfo: serverInfo.Slots["network-status"]}

	const clientYaml = `name: client
version: 1.0
apps:
  app:
    command: foo
    plugs: [network-status]
`
	clientInfo := snaptest.MockInfo(c, clientYaml, nil)
	s.plug = &interfaces.Plug{PlugInfo: clientInfo.Plugs["network-status"]}
}

func (s *NetworkStatusSuite) TestName(c *C) {
	c.Check(s.iface.Name(), Equals, "network-status")
}

func (s *NetworkStatusSuite) TestSanitizeIncorrectInterface(c *C) {
	c.Check(func() { s.iface.SanitizeSlot(&interfaces.Slot{SlotInfo: &snap.SlotInfo{Interface: "other"}}) },
		PanicMatches, `slot is not of interface "network-status"`)
	c.Check(func() { s.iface.SanitizePlug(&interfaces.Plug{PlugInfo: &snap.PlugInfo{Interface: "other"}}) },
		PanicMatches, `plug is not of interface "network-status"`)
}

func (s *NetworkStatusSuite) TestAppArmorSpec(c *C) {
	// connected slots have a non-nil security snippet for apparmor
	spec := &apparmor.Specification{}
	err := spec.AddConnectedSlot(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.server.app"})
	c.Assert(spec.SnippetForTag("snap.server.app"), testutil.Contains, "interface=org.freedesktop.DBus.*")
	c.Assert(spec.SnippetForTag("snap.server.app"), testutil.Contains, `peer=(label="snap.client.app")`)

	// slots have a permanent non-nil security snippet for apparmor
	spec = &apparmor.Specification{}
	err = spec.AddPermanentSlot(s.iface, s.slot)
	c.Assert(err, IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.server.app"})
	c.Assert(spec.SnippetForTag("snap.server.app"), testutil.Contains, "dbus (bind)")
	c.Assert(spec.SnippetForTag("snap.server.app"), testutil.Contains, `name="com.ubuntu.connectivity1.NetworkingStatus"`)

	// connected plugs have a non-nil security snippet for apparmor
	spec = &apparmor.Specification{}
	err = spec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.client.app"})
	c.Assert(spec.SnippetForTag("snap.client.app"), testutil.Contains, `peer=(label="snap.server.app"`)
	c.Assert(spec.SnippetForTag("snap.client.app"), testutil.Contains, "interface=com.ubuntu.connectivity1.NetworkingStatus{,/**}")
}

func (s *NetworkStatusSuite) TestDBusSpec(c *C) {
	// slots have a permanent non-nil security snippet for dbus
	spec := &dbus.Specification{}
	err := spec.AddPermanentSlot(s.iface, s.slot)
	c.Assert(err, IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.server.app"})
	c.Assert(spec.SnippetForTag("snap.server.app"), testutil.Contains, `<policy user="root">`)
	c.Assert(spec.SnippetForTag("snap.server.app"), testutil.Contains, `<allow send_destination="com.ubuntu.connectivity1.NetworkingStatus"/>`)
}
