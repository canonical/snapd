// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017-2018 Canonical Ltd
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
	apparmor_sandbox "github.com/snapcore/snapd/sandbox/apparmor"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type NetlinkAuditInterfaceSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

const netlinkAuditMockPlugSnapInfoYaml = `name: other
version: 1.0
apps:
 app2:
  command: foo
  plugs: [netlink-audit]
`

const netlinkAuditMockSlotSnapInfoYaml = `name: core
version: 1.0
type: os
slots:
 netlink-audit:
  interface: netlink-audit
`

var _ = Suite(&NetlinkAuditInterfaceSuite{
	iface: builtin.MustInterface("netlink-audit"),
})

func (s *NetlinkAuditInterfaceSuite) SetUpTest(c *C) {
	s.slot, s.slotInfo = MockConnectedSlot(c, netlinkAuditMockSlotSnapInfoYaml, nil, "netlink-audit")
	s.plug, s.plugInfo = MockConnectedPlug(c, netlinkAuditMockPlugSnapInfoYaml, nil, "netlink-audit")
}

func (s *NetlinkAuditInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "netlink-audit")
}

func (s *NetlinkAuditInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *NetlinkAuditInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *NetlinkAuditInterfaceSuite) TestSanitizePlugConnectionMissingAppArmorSandboxFeatures(c *C) {
	r := apparmor_sandbox.MockLevel(apparmor_sandbox.Full)
	defer r()
	r = apparmor_sandbox.MockFeatures(nil, nil, nil, nil)
	defer r()
	err := interfaces.BeforeConnectPlug(s.iface, s.plug)
	c.Assert(err, ErrorMatches, "cannot connect plug on system without audit_read support")
}

func (s *NetlinkAuditInterfaceSuite) TestSanitizePlugConnectionMissingNoAppArmor(c *C) {
	r := apparmor_sandbox.MockLevel(apparmor_sandbox.Unsupported)
	defer r()
	err := interfaces.BeforeConnectPlug(s.iface, s.plug)
	c.Assert(err, IsNil)
}

func (s *NetlinkAuditInterfaceSuite) TestAppArmorSpec(c *C) {
	spec := apparmor.NewSpecification(s.plug.AppSet())
	err := spec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.other.app2"})
	c.Check(spec.SnippetForTag("snap.other.app2"), testutil.Contains, "capability audit_write,\n")
}

func (s *NetlinkAuditInterfaceSuite) TestSecCompSpec(c *C) {
	spec := seccomp.NewSpecification(s.plug.AppSet())
	err := spec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.other.app2"})
	c.Check(spec.SnippetForTag("snap.other.app2"), testutil.Contains, "socket AF_NETLINK - NETLINK_AUDIT\n")
}

func (s *NetlinkAuditInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
