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
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type AccountControlSuite struct {
	iface interfaces.Interface
	slot  *interfaces.Slot
	plug  *interfaces.Plug
}

var _ = Suite(&AccountControlSuite{})

const accountCtlMockPlugSnapInfo = `name: other
version: 1.0
plugs:
 account-control:
  interface: account-control
apps:
 app2:
  command: foo
  plugs:
   - account-control
`

func (s *AccountControlSuite) SetUpTest(c *C) {
	s.iface = builtin.NewAccountControlInterface()
	s.slot = &interfaces.Slot{
		SlotInfo: &snap.SlotInfo{
			Snap:      &snap.Info{SuggestedName: "core", Type: snap.TypeOS},
			Name:      "account-control",
			Interface: "account-control",
			Apps: map[string]*snap.AppInfo{
				"app1": {
					Snap: &snap.Info{
						SuggestedName: "core",
					},
					Name: "app1"}},
		},
	}

	plugSnap := snaptest.MockInfo(c, accountCtlMockPlugSnapInfo, nil)
	s.plug = &interfaces.Plug{PlugInfo: plugSnap.Plugs["account-control"]}
}

func (s *AccountControlSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "account-control")
}

func (s *AccountControlSuite) TestSanitizeSlot(c *C) {
	err := s.iface.SanitizeSlot(s.slot)
	c.Assert(err, IsNil)
	err = s.iface.SanitizeSlot(&interfaces.Slot{SlotInfo: &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "some-snap"},
		Name:      "account-control",
		Interface: "account-control",
	}})
	c.Assert(err, ErrorMatches, "account-control slots are reserved for the operating system snap")
}

func (s *AccountControlSuite) TestSanitizePlug(c *C) {
	err := s.iface.SanitizePlug(s.plug)
	c.Assert(err, IsNil)
}

func (s *AccountControlSuite) TestSanitizeIncorrectInterface(c *C) {
	c.Assert(func() { s.iface.SanitizeSlot(&interfaces.Slot{SlotInfo: &snap.SlotInfo{Interface: "other"}}) },
		PanicMatches, `slot is not of interface "account-control"`)
	c.Assert(func() { s.iface.SanitizePlug(&interfaces.Plug{PlugInfo: &snap.PlugInfo{Interface: "other"}}) },
		PanicMatches, `plug is not of interface "account-control"`)
}

func (s *AccountControlSuite) TestUsedSecuritySystems(c *C) {
	// connected plugs have a non-nil security snippet for apparmor
	snippet, err := s.iface.ConnectedPlugSnippet(s.plug, s.slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, Not(IsNil))
	c.Assert(string(snippet), testutil.Contains, "/{,usr/}sbin/chpasswd")

	seccompSpec := &seccomp.Specification{}
	err = seccompSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	snippets := seccompSpec.Snippets()
	c.Assert(len(snippets), Equals, 1)
	c.Assert(len(snippets["snap.other.app2"]), Equals, 1)
	c.Check(string(snippets["snap.other.app2"][0]), testutil.Contains, "\nfchown - 0 42\n")
}
