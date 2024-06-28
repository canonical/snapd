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
	"github.com/snapcore/snapd/testutil"
)

type NetlinkConnectorInterfaceSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

const netlinkConnectorMockPlugSnapInfoYaml = `name: other
version: 1.0
apps:
 app2:
  command: foo
  plugs: [netlink-connector]
`

const netlinkConnectorMockSlotSnapInfoYaml = `name: core
version: 1.0
type: os
slots:
 netlink-connector:
  interface: netlink-connector
`

var _ = Suite(&NetlinkConnectorInterfaceSuite{
	iface: builtin.MustInterface("netlink-connector"),
})

func (s *NetlinkConnectorInterfaceSuite) SetUpTest(c *C) {
	s.slot, s.slotInfo = MockConnectedSlot(c, netlinkConnectorMockSlotSnapInfoYaml, nil, "netlink-connector")
	s.plug, s.plugInfo = MockConnectedPlug(c, netlinkConnectorMockPlugSnapInfoYaml, nil, "netlink-connector")
}

func (s *NetlinkConnectorInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "netlink-connector")
}

func (s *NetlinkConnectorInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *NetlinkConnectorInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *NetlinkConnectorInterfaceSuite) TestUsedSecuritySystems(c *C) {
	seccompSpec := seccomp.NewSpecification(s.plug.AppSet())
	err := seccompSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(seccompSpec.SecurityTags(), DeepEquals, []string{"snap.other.app2"})
	c.Check(seccompSpec.SnippetForTag("snap.other.app2"), testutil.Contains, "socket AF_NETLINK - NETLINK_CONNECTOR\n")
}

func (s *NetlinkConnectorInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
