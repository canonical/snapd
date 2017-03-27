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
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
)

type RandomInterfaceSuite struct {
	iface interfaces.Interface
	slot  *interfaces.Slot
	plug  *interfaces.Plug
}

var _ = Suite(&RandomInterfaceSuite{
	iface: &builtin.RandomInterface{},
})

func (s *RandomInterfaceSuite) SetUpTest(c *C) {
	// Mock for OS Snap
	osSnapInfo := snaptest.MockInfo(c, `
name: ubuntu-core
type: os
slots:
  test-random:
    interface: random
`, nil)
	s.slot = &interfaces.Slot{SlotInfo: osSnapInfo.Slots["test-random"]}

	// Snap Consumers
	consumingSnapInfo := snaptest.MockInfo(c, `
name: client-snap
apps:
  app-accessing-random:
    command: foo
    plugs: [random]
`, nil)
	s.plug = &interfaces.Plug{PlugInfo: consumingSnapInfo.Plugs["random"]}
}

func (s *RandomInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "random")
}

func (s *RandomInterfaceSuite) TestSanitizeSlot(c *C) {
	err := s.iface.SanitizeSlot(s.slot)
	c.Assert(err, IsNil)
	err = s.iface.SanitizeSlot(&interfaces.Slot{SlotInfo: &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "some-snap"},
		Name:      "random",
		Interface: "random",
	}})
	c.Assert(err, ErrorMatches, "random slots only allowed on core snap")
}

func (s *RandomInterfaceSuite) TestSanitizePlug(c *C) {
	err := s.iface.SanitizePlug(s.plug)
	c.Assert(err, IsNil)
}

func (s *RandomInterfaceSuite) TestSanitizeIncorrectInterface(c *C) {
	c.Assert(func() { s.iface.SanitizeSlot(&interfaces.Slot{SlotInfo: &snap.SlotInfo{Interface: "other"}}) },
		PanicMatches, `slot is not of interface "random"`)
	c.Assert(func() { s.iface.SanitizePlug(&interfaces.Plug{PlugInfo: &snap.PlugInfo{Interface: "other"}}) },
		PanicMatches, `plug is not of interface "random"`)
}

func (s *RandomInterfaceSuite) TestUsedSecuritySystems(c *C) {
	expectedSnippet1 := `
# Description: Allow access to the hardware random number generator device - /dev/hwrng

/dev/hwrng rw,
/sys/devices/virtual/misc/hw_random rw,
`
	expectedSnippet2 := `KERNEL=="hwrng", TAG+="snap_client-snap_app-accessing-random"`

	// connected plugs have a non-nil security snippet for apparmor
	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.client-snap.app-accessing-random"})
	aasnippet := apparmorSpec.SnippetForTag("snap.client-snap.app-accessing-random")
	c.Assert(aasnippet, Equals, expectedSnippet1, Commentf("\nexpected:\n%s\nfound:\n%s", expectedSnippet1, aasnippet))

	udevSpec := &udev.Specification{}
	c.Assert(udevSpec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(udevSpec.Snippets(), HasLen, 1)
	snippet := udevSpec.Snippets()[0]
	c.Assert(snippet, Equals, expectedSnippet2)
}
