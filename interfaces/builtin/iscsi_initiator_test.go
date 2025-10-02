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

type iscsiInitiatorInterfaceSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

const iscsiInitiatorMockPlugSnapInfoYaml = `name: other
version: 1.0
apps:
 app:
  command: foo
  plugs: [iscsi-initiator]
`

const iscsiInitiatorCoreYaml = `name: core
version: 0
type: os
slots:
  iscsi-initiator:
`

var _ = Suite(&iscsiInitiatorInterfaceSuite{
	iface: builtin.MustInterface("iscsi-initiator"),
})

func (s *iscsiInitiatorInterfaceSuite) SetUpTest(c *C) {
	s.slot, s.slotInfo = MockConnectedSlot(c, iscsiInitiatorCoreYaml, nil, "iscsi-initiator")
	s.plug, s.plugInfo = MockConnectedPlug(c, iscsiInitiatorMockPlugSnapInfoYaml, nil, "iscsi-initiator")
}

func (s *iscsiInitiatorInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "iscsi-initiator")
}

func (s *iscsiInitiatorInterfaceSuite) TestUsedSecuritySystems(c *C) {
	// connected plugs have a non-nil security snippet for apparmor
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	apparmorSpec := apparmor.NewSpecification(appSet)
	c.Assert(apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(apparmorSpec.SecurityTags(), HasLen, 1)
}

func (s *iscsiInitiatorInterfaceSuite) TestConnectedPlugSnippet(c *C) {
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

func (s *iscsiInitiatorInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *iscsiInitiatorInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *iscsiInitiatorInterfaceSuite) TestKModConnectedPlug(c *C) {
	spec := &kmod.Specification{}
	err := spec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(spec.Modules(), DeepEquals, map[string]bool{
		"iscsi_tcp":       true,
		"target_core_mod": true,
	})
}

func (s *iscsiInitiatorInterfaceSuite) TestUDevConnectedPlug(c *C) {
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	spec := udev.NewSpecification(appSet)
	// no udev specs because the interface controls it's own device cgroups
	err = spec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(spec.Snippets(), HasLen, 0)
}

func (s *iscsiInitiatorInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
