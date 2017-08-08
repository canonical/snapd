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
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type DesktopInterfaceSuite struct {
	iface interfaces.Interface
	slot  *interfaces.Slot
	plug  *interfaces.Plug
}

const desktopMockPlugSnapInfoYaml = `name: other
version: 1.0
apps:
 app2:
  command: foo
  plugs: [desktop]
`

var _ = Suite(&DesktopInterfaceSuite{
	iface: builtin.MustInterface("desktop"),
})

func (s *DesktopInterfaceSuite) SetUpTest(c *C) {
	s.slot = &interfaces.Slot{
		SlotInfo: &snap.SlotInfo{
			Snap:      &snap.Info{SuggestedName: "core", Type: snap.TypeOS},
			Name:      "desktop",
			Interface: "desktop",
		},
	}
	plugSnap := snaptest.MockInfo(c, desktopMockPlugSnapInfoYaml, nil)
	s.plug = &interfaces.Plug{PlugInfo: plugSnap.Plugs["desktop"]}
}

func (s *DesktopInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "desktop")
}

func (s *DesktopInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(s.slot.Sanitize(s.iface), IsNil)
	slot := &interfaces.Slot{SlotInfo: &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "some-snap"},
		Name:      "desktop",
		Interface: "desktop",
	}}
	c.Assert(slot.Sanitize(s.iface), ErrorMatches,
		"desktop slots are reserved for the core snap")
}

func (s *DesktopInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(s.plug.Sanitize(s.iface), IsNil)
}

func (s *DesktopInterfaceSuite) TestUsedSecuritySystems(c *C) {
	// connected plugs have a non-nil security snippet for apparmor
	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, nil, s.slot, nil)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app2"})
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app2"), testutil.Contains, `# Description: Can access modern Desktop Environments`)
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app2"), testutil.Contains, `#include <abstractions/fonts>`)
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app2"), testutil.Contains, `/etc/gtk-3.0/settings.ini r,`)
}

func (s *DesktopInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
