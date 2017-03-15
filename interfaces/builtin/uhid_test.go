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
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type UhidInterfaceSuite struct {
	iface interfaces.Interface

	slot *interfaces.Slot
	plug *interfaces.Plug
}

var _ = Suite(&UhidInterfaceSuite{
	iface: &builtin.UhidInterface{},
})

func (s *UhidInterfaceSuite) SetUpTest(c *C) {
	// Mocking
	osSnapInfo := snaptest.MockInfo(c, `
name: ubuntu-core
type: os
slots:
  test-slot-1:
    interface: uhid
    path: /dev/uhid
`, nil)
	s.slot = &interfaces.Slot{SlotInfo: osSnapInfo.Slots["test-slot-1"]}

	// Snap Consumers
	consumingSnapInfo := snaptest.MockInfo(c, `
name: client-snap
apps:
  app-accessing-slot-1:
    command: foo
    plugs: [uhid]
`, nil)
	s.plug = &interfaces.Plug{PlugInfo: consumingSnapInfo.Plugs["uhid"]}
}

func (s *UhidInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "uhid")
}

func (s *UhidInterfaceSuite) TestSanitizeSlot(c *C) {
	err := s.iface.SanitizeSlot(s.slot)
	c.Assert(err, IsNil)
}

func (s *UhidInterfaceSuite) TestSanitizePlug(c *C) {
	err := s.iface.SanitizePlug(s.plug)
	c.Assert(err, IsNil)
}

func (s *UhidInterfaceSuite) TestConnectedPlugAppArmorSnippets(c *C) {
	// connected plugs have a non-nil security snippet for apparmor
	snippet, err := s.iface.ConnectedPlugSnippet(s.plug, s.slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, Not(IsNil))
	c.Check(string(snippet), testutil.Contains, "/dev/uhid rw,\n")
}

func (s *UhidInterfaceSuite) TestConnectedPlugUdevSnippets(c *C) {

	expectedSnippet1 := []byte(`KERNEL=="uhid", TAG+="snap_client-snap_app-accessing-slot-1"
`)

	snippet, err := s.iface.ConnectedPlugSnippet(s.plug, s.slot, interfaces.SecurityUDev)
	c.Assert(err, IsNil)
	c.Assert(snippet, DeepEquals, expectedSnippet1, Commentf("\nexpected:\n%s\nfound:\n%s", expectedSnippet1, snippet))
}

func (s *UhidInterfaceSuite) TestAutoConnect(c *C) {
	c.Check(s.iface.AutoConnect(nil, nil), Equals, true)
}
