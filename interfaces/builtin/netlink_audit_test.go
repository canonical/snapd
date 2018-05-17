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
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
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

var _ = Suite(&NetlinkAuditInterfaceSuite{
	iface: builtin.MustInterface("netlink-audit"),
})

func (s *NetlinkAuditInterfaceSuite) SetUpTest(c *C) {
	s.slotInfo = &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "core", Type: snap.TypeOS},
		Name:      "netlink-audit",
		Interface: "netlink-audit",
	}
	s.slot = interfaces.NewConnectedSlot(s.slotInfo, nil)
	plugSnap := snaptest.MockInfo(c, netlinkAuditMockPlugSnapInfoYaml, nil)
	s.plugInfo = plugSnap.Plugs["netlink-audit"]
	s.plug = interfaces.NewConnectedPlug(s.plugInfo, nil)
}

func (s *NetlinkAuditInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "netlink-audit")
}

func (s *NetlinkAuditInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
	slot := &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "some-snap"},
		Name:      "netlink-audit",
		Interface: "netlink-audit",
	}
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		"netlink-audit slots are reserved for the core snap")
}

func (s *NetlinkAuditInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *NetlinkAuditInterfaceSuite) TestAppArmorSpec(c *C) {
	spec := &apparmor.Specification{}
	err := spec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.other.app2"})
	c.Check(spec.SnippetForTag("snap.other.app2"), testutil.Contains, "capability audit_write,\n")
}

func (s *NetlinkAuditInterfaceSuite) TestSecCompSpec(c *C) {
	spec := &seccomp.Specification{}
	err := spec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.other.app2"})
	c.Check(spec.SnippetForTag("snap.other.app2"), testutil.Contains, "socket AF_NETLINK - NETLINK_AUDIT\n")
}

func (s *NetlinkAuditInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
