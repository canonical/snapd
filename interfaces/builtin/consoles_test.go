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
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type ConsolesInterfaceSuite struct {
	iface interfaces.Interface
	slot  *interfaces.Slot
	plug  *interfaces.Plug
}

var _ = Suite(&ConsolesInterfaceSuite{
	iface: &builtin.ConsolesInterface{},
})

func (s *ConsolesInterfaceSuite) SetUpTest(c *C) {
	provider := snaptest.MockInfo(c, `
name: core
type: os
slots:
  consoles:
`, nil)
	s.slot = &interfaces.Slot{SlotInfo: provider.Slots["consoles"]}

	consumer := snaptest.MockInfo(c, `
name: consumer
apps:
  app:
    command: foo
    plugs: [consoles]
`, nil)
	s.plug = &interfaces.Plug{PlugInfo: consumer.Plugs["consoles"]}
}

func (s *ConsolesInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "consoles")
}

func (s *ConsolesInterfaceSuite) TestSanitizeSlot(c *C) {
	err := s.iface.SanitizeSlot(s.slot)
	c.Assert(err, IsNil)
	err = s.iface.SanitizeSlot(&interfaces.Slot{SlotInfo: &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "some-snap"},
		Name:      "consoles",
		Interface: "consoles",
	}})
	c.Assert(err, ErrorMatches, "consoles slots are reserved for the operating system snap")
}

func (s *ConsolesInterfaceSuite) TestSanitizePlug(c *C) {
	err := s.iface.SanitizePlug(s.plug)
	c.Assert(err, IsNil)
}

func (s *ConsolesInterfaceSuite) TestSanitizeIncorrectInterface(c *C) {
	c.Assert(func() { s.iface.SanitizeSlot(&interfaces.Slot{SlotInfo: &snap.SlotInfo{Interface: "other"}}) },
		PanicMatches, `slot is not of interface "consoles"`)
	c.Assert(func() { s.iface.SanitizePlug(&interfaces.Plug{PlugInfo: &snap.PlugInfo{Interface: "other"}}) },
		PanicMatches, `plug is not of interface "consoles"`)
}

func (s *ConsolesInterfaceSuite) TestAppArmorConnectedPlug(c *C) {
	spec := &apparmor.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, nil, s.slot, nil), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	expected := `
# Description: Allow access to the current system console.

/dev/tty0 rw,
/sys/devices/virtual/tty/tty0 rw,
/dev/console rw,
/sys/devices/virtual/tty/console rw,
`
	c.Assert(spec.SnippetForTag("snap.consumer.app"), Equals, expected)
}

func (s *ConsolesInterfaceSuite) TestUDevConnectedPlug(c *C) {
	spec := &udev.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, nil, s.slot, nil), IsNil)
	c.Assert(spec.Snippets(), HasLen, 1)
	expected := []string{`
SUBSYSTEM="tty", KERNEL=="tty0", TAG+="snap_consumer_app"
SUBSYSTEM="tty", KERNEL=="console", TAG+="snap_consumer_app"
`}
	c.Assert(spec.Snippets(), DeepEquals, expected)
}

func (s *ConsolesInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
