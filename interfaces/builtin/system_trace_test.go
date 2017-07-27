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
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type SystemTraceInterfaceSuite struct {
	iface interfaces.Interface
	slot  *interfaces.Slot
	plug  *interfaces.Plug
}

var _ = Suite(&SystemTraceInterfaceSuite{
	iface: builtin.MustInterface("system-trace"),
})

func (s *SystemTraceInterfaceSuite) SetUpTest(c *C) {
	const mockPlugSnapInfo = `name: other
version: 1.0
apps:
 app:
  command: foo
  plugs: [system-trace]
`
	s.slot = &interfaces.Slot{
		SlotInfo: &snap.SlotInfo{
			Snap:      &snap.Info{SuggestedName: "core", Type: snap.TypeOS},
			Name:      "system-trace",
			Interface: "system-trace",
		},
	}
	plugSnap := snaptest.MockInfo(c, mockPlugSnapInfo, nil)
	s.plug = &interfaces.Plug{PlugInfo: plugSnap.Plugs["system-trace"]}
}

func (s *SystemTraceInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "system-trace")
}

func (s *SystemTraceInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(s.slot.Sanitize(s.iface), IsNil)
	slot := &interfaces.Slot{SlotInfo: &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "some-snap"},
		Name:      "system-trace",
		Interface: "system-trace",
	}}
	c.Assert(slot.Sanitize(s.iface), ErrorMatches, "system-trace slots are reserved for the operating system snap")
}

func (s *SystemTraceInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(s.plug.Sanitize(s.iface), IsNil)
}

func (s *SystemTraceInterfaceSuite) TestUsedSecuritySystems(c *C) {
	// connected plugs have a non-nil security snippet for apparmor
	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, nil, s.slot, nil)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app"})
	c.Check(apparmorSpec.SnippetForTag("snap.other.app"), testutil.Contains, "/sys/kernel/debug/tracing/ r,")
}

func (s *SystemTraceInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
