// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

type kernelCryptoAPIInterfaceSuite struct {
	iface        interfaces.Interface
	coreSlotInfo *snap.SlotInfo
	coreSlot     *interfaces.ConnectedSlot
	plugInfo     *snap.PlugInfo
	plug         *interfaces.ConnectedPlug
}

var _ = Suite(&kernelCryptoAPIInterfaceSuite{
	iface: builtin.MustInterface("kernel-crypto-api"),
})

const kernelCryptoAPIConsumerYaml = `name: consumer
version: 0
apps:
 app:
  plugs: [kernel-crypto-api]
`

const kernelCryptoAPICoreYaml = `name: core
version: 0
type: os
slots:
  kernel-crypto-api:
`

func (s *kernelCryptoAPIInterfaceSuite) SetUpTest(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, kernelCryptoAPIConsumerYaml, nil, "kernel-crypto-api")
	s.coreSlot, s.coreSlotInfo = MockConnectedSlot(c, kernelCryptoAPICoreYaml, nil, "kernel-crypto-api")
}

func (s *kernelCryptoAPIInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "kernel-crypto-api")
}

func (s *kernelCryptoAPIInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.coreSlotInfo), IsNil)
}

func (s *kernelCryptoAPIInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *kernelCryptoAPIInterfaceSuite) TestAppArmorSpec(c *C) {
	spec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap()))
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.coreSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "# Description: Can access the Linux kernel crypto API")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "network alg seqpacket,")
}

func (s *kernelCryptoAPIInterfaceSuite) TestSeccompSpec(c *C) {
	spec := seccomp.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap()))
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.coreSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "# Description: Can access the Linux kernel crypto API")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "socket AF_NETLINK - NETLINK_CRYPTO")
}

func (s *kernelCryptoAPIInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, true)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows access to the Linux kernel crypto API`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "kernel-crypto-api")
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "deny-auto-connection: true")
}

func (s *kernelCryptoAPIInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
