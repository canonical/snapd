// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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
	"github.com/snapcore/snapd/interfaces/kmod"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type iscsiInitiatorSupportInterfaceSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

const iscsiInitiatorSupportMockPlugSnapInfoYaml = `name: other
version: 1.0
apps:
 app:
  command: foo
  plugs: [iscsi-initiator-support]
`

const iscsiInitiatorSupportCoreYaml = `name: core
version: 0
type: os
slots:
  iscsi-initiator-support:
`

var _ = Suite(&iscsiInitiatorSupportInterfaceSuite{
	iface: builtin.MustInterface("iscsi-initiator-support"),
})

func (s *iscsiInitiatorSupportInterfaceSuite) SetUpTest(c *C) {
	s.slot, s.slotInfo = MockConnectedSlot(c, iscsiInitiatorSupportCoreYaml, nil, "iscsi-initiator-support")
	s.plug, s.plugInfo = MockConnectedPlug(c, iscsiInitiatorSupportMockPlugSnapInfoYaml, nil, "iscsi-initiator-support")
}

func (s *iscsiInitiatorSupportInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "iscsi-initiator-support")
}

func (s *iscsiInitiatorSupportInterfaceSuite) TestUsedSecuritySystems(c *C) {
	// connected plugs have a non-nil security snippet for apparmor
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	apparmorSpec := apparmor.NewSpecification(appSet)
	c.Assert(apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(apparmorSpec.SecurityTags(), HasLen, 1)
}

func (s *iscsiInitiatorSupportInterfaceSuite) TestConnectedPlugSnippet(c *C) {
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	apparmorSpec := apparmor.NewSpecification(appSet)
	c.Assert(apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app"), testutil.Contains, "/etc/iscsi/initiatorname.iscsi r,")
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app"), testutil.Contains, "/etc/iscsi/nodes/ rwk,")
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app"), testutil.Contains, "/sys/kernel/config/target/ rw,")
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app"), testutil.Contains, "unix (send, receive, connect) type=stream peer=(addr=\"@ISCSIADM_ABSTRACT_NAMESPACE\"),")
}

func (s *iscsiInitiatorSupportInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *iscsiInitiatorSupportInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *iscsiInitiatorSupportInterfaceSuite) TestKModConnectedPlug(c *C) {
	spec := &kmod.Specification{}
	err := spec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(spec.Modules(), DeepEquals, map[string]bool{
		"iscsi_tcp":       true,
		"target_core_mod": true,
	})
}

func (s *iscsiInitiatorSupportInterfaceSuite) TestUDevConnectedPlug(c *C) {
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	spec := udev.NewSpecification(appSet)
	// no udev specs because the interface controls it's own device cgroups
	err = spec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(spec.Snippets(), HasLen, 0)
}

func (s *iscsiInitiatorSupportInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
