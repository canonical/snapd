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
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
)

type IioPortsControlInterfaceSuite struct {
	iface interfaces.Interface
	slot  *interfaces.Slot
	plug  *interfaces.Plug
}

var _ = Suite(&IioPortsControlInterfaceSuite{
	iface: &builtin.IioPortsControlInterface{},
})

func (s *IioPortsControlInterfaceSuite) SetUpTest(c *C) {
	// Mock for OS Snap
	osSnapInfo := snaptest.MockInfo(c, `
name: ubuntu-core
type: os
slots:
  test-io-ports:
    interface: io-ports-control
`, nil)
	s.slot = &interfaces.Slot{SlotInfo: osSnapInfo.Slots["test-io-ports"]}

	// Snap Consumers
	consumingSnapInfo := snaptest.MockInfo(c, `
name: client-snap
plugs:
  plug-for-io-ports:
    interface: io-ports-control
apps:
  app-accessing-io-ports:
    command: foo
    plugs: [plug-for-io-ports]
`, nil)
	s.plug = &interfaces.Plug{PlugInfo: consumingSnapInfo.Plugs["plug-for-io-ports"]}
}

func (s *IioPortsControlInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "io-ports-control")
}

func (s *IioPortsControlInterfaceSuite) TestSanitizeSlot(c *C) {
	err := s.iface.SanitizeSlot(s.slot)
	c.Assert(err, IsNil)
	err = s.iface.SanitizeSlot(&interfaces.Slot{SlotInfo: &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "some-snap"},
		Name:      "io-ports-control",
		Interface: "io-ports-control",
	}})
	c.Assert(err, ErrorMatches, "io-ports-control slots only allowed on core snap")
}

func (s *IioPortsControlInterfaceSuite) TestSanitizePlug(c *C) {
	err := s.iface.SanitizePlug(s.plug)
	c.Assert(err, IsNil)
}

func (s *IioPortsControlInterfaceSuite) TestSanitizeIncorrectInterface(c *C) {
	c.Assert(func() { s.iface.SanitizeSlot(&interfaces.Slot{SlotInfo: &snap.SlotInfo{Interface: "other"}}) },
		PanicMatches, `slot is not of interface "io-ports-control"`)
	c.Assert(func() { s.iface.SanitizePlug(&interfaces.Plug{PlugInfo: &snap.PlugInfo{Interface: "other"}}) },
		PanicMatches, `plug is not of interface "io-ports-control"`)
}

func (s *IioPortsControlInterfaceSuite) TestUsedSecuritySystems(c *C) {
	expectedSnippet1 := []byte(`
# Description: Allow write access to all I/O ports.
# See 'man 4 mem' for details.

capability sys_rawio, # required by iopl

/dev/ports rw,
`)

	expectedSnippet2 := []byte(`
# Description: Allow changes to the I/O port permissions and
# privilege level of the calling process.  In addition to granting
# unrestricted I/O port access, running at a higher I/O privilege
# level also allows the process to disable interrupts.  This will
# probably crash the system, and is not recommended.
ioperm
iopl
`)

	expectedSnippet3 := []byte(`KERNEL=="ports", TAG+="snap_client-snap_app-accessing-io-ports"
`)

	// connected plugs have a non-nil security snippet for apparmor
	snippet, err := s.iface.ConnectedPlugSnippet(s.plug, nil, s.slot, nil, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, DeepEquals, expectedSnippet1, Commentf("\nexpected:\n%s\nfound:\n%s", expectedSnippet1, snippet))

	snippet, err = s.iface.ConnectedPlugSnippet(s.plug, nil, s.slot, nil, interfaces.SecuritySecComp)
	c.Assert(err, IsNil)
	c.Assert(snippet, DeepEquals, expectedSnippet2, Commentf("\nexpected:\n%s\nfound:\n%s", expectedSnippet2, snippet))

	snippet, err = s.iface.ConnectedPlugSnippet(s.plug, nil, s.slot, nil, interfaces.SecurityUDev)
	c.Assert(err, IsNil)
	c.Assert(snippet, DeepEquals, expectedSnippet3, Commentf("\nexpected:\n%s\nfound:\n%s", expectedSnippet3, snippet))
}
