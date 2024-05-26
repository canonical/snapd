// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type PolkitAgentSuite struct {
	iface        interfaces.Interface
	coreSlotInfo *snap.SlotInfo
	coreSlot     *interfaces.ConnectedSlot
	plugInfo     *snap.PlugInfo
	plug         *interfaces.ConnectedPlug
}

var _ = Suite(&PolkitAgentSuite{
	iface: builtin.MustInterface("polkit-agent"),
})

func (s *PolkitAgentSuite) SetUpSuite(c *C) {
	const coreProviderYaml = `name: core
type: os
version: 0
slots:
  polkit-agent:
`
	s.coreSlot, s.coreSlotInfo = MockConnectedSlot(c, coreProviderYaml, nil, "polkit-agent")

	const consumerYaml = `name: consumer
version: 1.0
apps:
  app:
    command: foo
    plugs: [polkit-agent]
`
	s.plug, s.plugInfo = MockConnectedPlug(c, consumerYaml, nil, "polkit-agent")
}

func (s *PolkitAgentSuite) TestName(c *C) {
	c.Check(s.iface.Name(), Equals, "polkit-agent")
}

func (s *PolkitAgentSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Check(si.ImplicitOnClassic, Equals, false)
	c.Check(si.ImplicitOnCore, Equals, osutil.FileExists("/usr/libexec/polkit-agent-helper-1") || osutil.FileExists("/usr/lib/polkit-1/polkit-agent-helper-1"))
}

func (s *PolkitAgentSuite) TestAppArmor(c *C) {
	// If the slot is provided by a snap, access is restricted to the snap's label
	appSet := mylog.Check2(interfaces.NewSnapAppSet(s.plug.Snap(), nil))

	spec := apparmor.NewSpecification(appSet)
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.coreSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Check(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "interface=org.freedesktop.PolicyKit1.Authority")
	c.Check(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "interface=org.freedesktop.PolicyKit1.AuthenticationAgent")

	appSet = mylog.Check2(interfaces.NewSnapAppSet(s.plugInfo.Snap, nil))

	spec = apparmor.NewSpecification(appSet)
	c.Assert(spec.AddPermanentPlug(s.iface, s.plugInfo), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)

	appSet = mylog.Check2(interfaces.NewSnapAppSet(s.coreSlot.Snap(), nil))

	spec = apparmor.NewSpecification(appSet)
	c.Assert(spec.AddConnectedSlot(s.iface, s.plug, s.coreSlot), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)

	appSet = mylog.Check2(interfaces.NewSnapAppSet(s.coreSlotInfo.Snap, nil))

	spec = apparmor.NewSpecification(appSet)
	c.Assert(spec.AddPermanentSlot(s.iface, s.coreSlotInfo), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)
}

func (s *PolkitAgentSuite) TestSeccomp(c *C) {
	appSet := mylog.Check2(interfaces.NewSnapAppSet(s.plug.Snap(), nil))

	spec := seccomp.NewSpecification(appSet)
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.coreSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Check(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "socket AF_NETLINK - NETLINK_AUDIT")

	appSet = mylog.Check2(interfaces.NewSnapAppSet(s.plugInfo.Snap, nil))

	spec = seccomp.NewSpecification(appSet)
	c.Assert(spec.AddPermanentPlug(s.iface, s.plugInfo), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)

	appSet = mylog.Check2(interfaces.NewSnapAppSet(s.coreSlot.Snap(), nil))

	spec = seccomp.NewSpecification(appSet)
	c.Assert(spec.AddConnectedSlot(s.iface, s.plug, s.coreSlot), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)

	appSet = mylog.Check2(interfaces.NewSnapAppSet(s.coreSlotInfo.Snap, nil))

	spec = seccomp.NewSpecification(appSet)
	c.Assert(spec.AddPermanentSlot(s.iface, s.coreSlotInfo), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)
}

func (s *PolkitAgentSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
