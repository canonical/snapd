// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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

type SnapdObserveInterfaceSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

var _ = Suite(&SnapdObserveInterfaceSuite{
	iface: builtin.MustInterface("snapd-observe"),
})

func (s *SnapdObserveInterfaceSuite) SetUpTest(c *C) {
	consumingSnapInfo := snaptest.MockInfo(c, `
name: other
version: 0
apps:
 app:
    command: foo
    plugs: [snapd-observe]
`, nil)
	s.slotInfo = &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "core", SnapType: snap.TypeOS},
		Name:      "snapd-observe",
		Interface: "snapd-observe",
	}
	s.slot = interfaces.NewConnectedSlot(s.slotInfo, nil, nil)
	s.plugInfo = consumingSnapInfo.Plugs["snapd-observe"]
	s.plug = interfaces.NewConnectedPlug(s.plugInfo, nil, nil)
}

func (s *SnapdObserveInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "snapd-observe")
}

func (s *SnapdObserveInterfaceSuite) TestUsedSecuritySystems(c *C) {
	// connected plugs have a non-nil security snippet for apparmor
	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app"), testutil.Contains, `/run/snapd-ro.socket rw,`)
}

func (s *SnapdObserveInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
