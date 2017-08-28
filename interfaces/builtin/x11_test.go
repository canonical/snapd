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

type X11InterfaceSuite struct {
	iface interfaces.Interface
	slot  *interfaces.Slot
	plug  *interfaces.Plug
}

const x11MockPlugSnapInfoYaml = `name: other
version: 1.0
apps:
 app2:
  command: foo
  plugs: [x11]
`

var _ = Suite(&X11InterfaceSuite{
	iface: builtin.MustInterface("x11"),
})

func (s *X11InterfaceSuite) SetUpTest(c *C) {
	s.slot = &interfaces.Slot{
		SlotInfo: &snap.SlotInfo{
			PlugSlotData: snap.PlugSlotData{
				Snap:      &snap.Info{SuggestedName: "core", Type: snap.TypeOS},
				Name:      "x11",
				Interface: "x11",
			},
		},
	}
	plugSnap := snaptest.MockInfo(c, x11MockPlugSnapInfoYaml, nil)
	s.plug = &interfaces.Plug{PlugInfo: plugSnap.Plugs["x11"]}
}

func (s *X11InterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "x11")
}

func (s *X11InterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(s.slot.Sanitize(s.iface), IsNil)
	slot := &interfaces.Slot{SlotInfo: &snap.SlotInfo{
		PlugSlotData: snap.PlugSlotData{
			Snap:      &snap.Info{SuggestedName: "some-snap"},
			Name:      "x11",
			Interface: "x11",
		}}}
	c.Assert(slot.Sanitize(s.iface), ErrorMatches,
		"x11 slots are reserved for the core snap")
}

func (s *X11InterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(s.plug.Sanitize(s.iface), IsNil)
}

func (s *X11InterfaceSuite) TestUsedSecuritySystems(c *C) {
	// connected plugs have a non-nil security snippet for apparmor
	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, nil, s.slot, nil)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app2"})
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app2"), testutil.Contains, `fontconfig`)
}

// The shutdown system call is allowed
func (s *X11InterfaceSuite) TestLP1574526(c *C) {
	seccompSpec := &seccomp.Specification{}
	err := seccompSpec.AddConnectedPlug(s.iface, s.plug, nil, s.slot, nil)
	c.Assert(err, IsNil)
	c.Assert(seccompSpec.SecurityTags(), DeepEquals, []string{"snap.other.app2"})
	c.Check(seccompSpec.SnippetForTag("snap.other.app2"), testutil.Contains, "shutdown\n")
}

func (s *X11InterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
