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

type NetworkSetupObserveInterfaceSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

var _ = Suite(&NetworkSetupObserveInterfaceSuite{
	iface: builtin.MustInterface("network-setup-observe"),
})

func (s *NetworkSetupObserveInterfaceSuite) SetUpTest(c *C) {
	const mockPlugSnapInfoYaml = `name: other
version: 1.0
apps:
 app:
  command: foo
  plugs: [network-setup-observe]
`
	s.slotInfo = &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "core", Type: snap.TypeOS},
		Name:      "network-setup-observe",
		Interface: "network-setup-observe",
	}
	s.slot = interfaces.NewConnectedSlot(s.slotInfo, nil)

	snapInfo := snaptest.MockInfo(c, mockPlugSnapInfoYaml, nil)
	s.plugInfo = snapInfo.Plugs["network-setup-observe"]
	s.plug = interfaces.NewConnectedPlug(s.plugInfo, nil)
}

func (s *NetworkSetupObserveInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "network-setup-observe")
}

func (s *NetworkSetupObserveInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
	slot := &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "some-snap"},
		Name:      "network-setup-observe",
		Interface: "network-setup-observe",
	}
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		"network-setup-observe slots are reserved for the core snap")
}

func (s *NetworkSetupObserveInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *NetworkSetupObserveInterfaceSuite) TestUsedSecuritySystems(c *C) {
	// connected plugs have a non-nil security snippet for apparmor
	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app"), testutil.Contains, "/etc/netplan")
}

func (s *NetworkSetupObserveInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
