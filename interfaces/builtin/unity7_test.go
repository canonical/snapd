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
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type Unity7InterfaceSuite struct {
	iface interfaces.Interface
	slot  *interfaces.Slot
	plug  *interfaces.Plug
}

var _ = Suite(&Unity7InterfaceSuite{})

const unity7mockPlugSnapInfoYaml = `name: other-snap
version: 1.0
apps:
 app2:
  command: foo
  plugs: [unity7]
`

func (s *Unity7InterfaceSuite) SetUpTest(c *C) {
	s.iface = &builtin.Unity7Interface{}
	s.slot = &interfaces.Slot{
		SlotInfo: &snap.SlotInfo{
			Snap:      &snap.Info{SuggestedName: "core", Type: snap.TypeOS},
			Name:      "unity7",
			Interface: "unity7",
		},
	}
	plugSnap := snaptest.MockInfo(c, unity7mockPlugSnapInfoYaml, nil)
	s.plug = &interfaces.Plug{PlugInfo: plugSnap.Plugs["unity7"]}
}

func (s *Unity7InterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "unity7")
}

func (s *Unity7InterfaceSuite) TestSanitizeSlot(c *C) {
	err := s.iface.SanitizeSlot(s.slot)
	c.Assert(err, IsNil)
	err = s.iface.SanitizeSlot(&interfaces.Slot{SlotInfo: &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "some-snap"},
		Name:      "unity7",
		Interface: "unity7",
	}})
	c.Assert(err, ErrorMatches, "unity7 slots are reserved for the operating system snap")
}

func (s *Unity7InterfaceSuite) TestSanitizePlug(c *C) {
	err := s.iface.SanitizePlug(s.plug)
	c.Assert(err, IsNil)
}

func (s *Unity7InterfaceSuite) TestSanitizeIncorrectInterface(c *C) {
	c.Assert(func() { s.iface.SanitizeSlot(&interfaces.Slot{SlotInfo: &snap.SlotInfo{Interface: "other-snap"}}) },
		PanicMatches, `slot is not of interface "unity7"`)
	c.Assert(func() { s.iface.SanitizePlug(&interfaces.Plug{PlugInfo: &snap.PlugInfo{Interface: "other-snap"}}) },
		PanicMatches, `plug is not of interface "unity7"`)
}

func (s *Unity7InterfaceSuite) TestUsedSecuritySystems(c *C) {
	// connected plugs have a non-nil security snippet for apparmor
	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, nil, s.slot, nil)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other-snap.app2"})
	c.Assert(apparmorSpec.SnippetForTag("snap.other-snap.app2"), testutil.Contains, `/usr/share/pixmaps`)
	c.Assert(apparmorSpec.SnippetForTag("snap.other-snap.app2"), testutil.Contains, `path=/com/canonical/indicator/messages/other_snap_*_desktop`)

	// connected plugs have a non-nil security snippet for seccomp
	seccompSpec := &seccomp.Specification{}
	err = seccompSpec.AddConnectedPlug(s.iface, s.plug, nil, s.slot, nil)
	c.Assert(err, IsNil)
	c.Assert(seccompSpec.SecurityTags(), DeepEquals, []string{"snap.other-snap.app2"})
	c.Check(seccompSpec.SnippetForTag("snap.other-snap.app2"), testutil.Contains, "shutdown\n")
}
