// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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
	"github.com/snapcore/snapd/testutil"
)

type AuditdSupportInterfaceSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

const auditdSupportMockPlugSnapInfoYaml = `name: other
version: 1.0
apps:
 app2:
  command: foo
  plugs: [auditd-support]
`

const auditdSupportMockSlotSnapInfoYaml = `name: core
version: 1.0
type: os
slots:
 auditd-support:
  interface: auditd-support
`

var _ = Suite(&AuditdSupportInterfaceSuite{
	iface: builtin.MustInterface("auditd-support"),
})

func (s *AuditdSupportInterfaceSuite) SetUpTest(c *C) {
	s.slot, s.slotInfo = MockConnectedSlot(c, auditdSupportMockSlotSnapInfoYaml, nil, "auditd-support")
	s.plug, s.plugInfo = MockConnectedPlug(c, auditdSupportMockPlugSnapInfoYaml, nil, "auditd-support")
}

func (s *AuditdSupportInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "auditd-support")
}

func (s *AuditdSupportInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *AuditdSupportInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *AuditdSupportInterfaceSuite) TestAppArmorSpec(c *C) {
	spec := apparmor.NewSpecification(s.plug.AppSet())
	err := spec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.other.app2"})
	c.Check(spec.SnippetForTag("snap.other.app2"), testutil.Contains, "capability audit_control,\n")
	c.Check(spec.SnippetForTag("snap.other.app2"), testutil.Contains, "@{PROC}/*/{loginuid,sessionid} r,\n")
	c.Check(spec.SnippetForTag("snap.other.app2"), testutil.Contains, "/{,var/}run/auditd.{pid,state} rw,\n")
	c.Check(spec.SnippetForTag("snap.other.app2"), testutil.Contains, "@{PROC}/@{pid}/oom_score_adj rw,\n")
}

func (s *AuditdSupportInterfaceSuite) TestSecCompSpec(c *C) {
	spec := seccomp.NewSpecification(s.plug.AppSet())
	err := spec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(spec.Snippets(), HasLen, 1)
	c.Check(spec.SnippetForTag("snap.other.app2"), testutil.Contains, "bind")
	c.Check(spec.SnippetForTag("snap.other.app2"), testutil.Contains, "socket AF_NETLINK")
	c.Check(spec.SnippetForTag("snap.other.app2"), testutil.Contains, "setpriority")
}

func (s *AuditdSupportInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
