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

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/seccomp"
	apparmor_sandbox "github.com/snapcore/snapd/sandbox/apparmor"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type QrtrInterfaceSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

var _ = Suite(&QrtrInterfaceSuite{
	iface: builtin.MustInterface("qualcomm-ipc-router"),
})

const qipcrtrConsumerYaml = `name: consumer
version: 0
apps:
 app:
  plugs: [qualcomm-ipc-router]
`

const qipcrtrCoreYaml = `name: core
version: 0
type: os
slots:
  qualcomm-ipc-router:
`

func (s *QrtrInterfaceSuite) SetUpTest(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, qipcrtrConsumerYaml, nil, "qualcomm-ipc-router")
	s.slot, s.slotInfo = MockConnectedSlot(c, qipcrtrCoreYaml, nil, "qualcomm-ipc-router")
}

func (s *QrtrInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "qualcomm-ipc-router")
}

func (s *QrtrInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *QrtrInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *QrtrInterfaceSuite) TestSanitizePlugConnectionFullAppArmorSandboxFeatures(c *C) {
	r := apparmor_sandbox.MockFeatures(nil, nil, []string{"qipcrtr-socket"}, nil)
	defer r()
	c.Assert(interfaces.BeforeConnectPlug(s.iface, s.plug), IsNil)
}

func (s *QrtrInterfaceSuite) TestSanitizePlugConnectionMissingAppArmorSandboxFeatures(c *C) {
	r := apparmor_sandbox.MockLevel(apparmor_sandbox.Full)
	defer r()
	r = apparmor_sandbox.MockFeatures(nil, nil, nil, nil)
	defer r()
	err := interfaces.BeforeConnectPlug(s.iface, s.plug)
	c.Assert(err, ErrorMatches, "cannot connect plug on system without qipcrtr socket support")
}

func (s *QrtrInterfaceSuite) TestSanitizePlugConnectionMissingNoAppArmor(c *C) {
	r := apparmor_sandbox.MockLevel(apparmor_sandbox.Unsupported)
	defer r()
	err := interfaces.BeforeConnectPlug(s.iface, s.plug)
	c.Assert(err, IsNil)
}

func (s *QrtrInterfaceSuite) TestAppArmorSpec(c *C) {
	spec := &apparmor.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "network qipcrtr,\n")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "capability net_admin,\n")
}

func (s *QrtrInterfaceSuite) TestSecCompSpec(c *C) {
	spec := &seccomp.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "bind\n")
}

func (s *QrtrInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, true)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows access to the Qualcomm IPC Router sockets`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "qualcomm-ipc-router")
}

func (s *QrtrInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
