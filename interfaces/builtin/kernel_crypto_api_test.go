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

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type kernelCryptoApiInterfaceSuite struct {
	iface        interfaces.Interface
	coreSlotInfo *snap.SlotInfo
	coreSlot     *interfaces.ConnectedSlot
	plugInfo     *snap.PlugInfo
	plug         *interfaces.ConnectedPlug
}

var _ = Suite(&kernelCryptoApiInterfaceSuite{
	iface: builtin.MustInterface("kernel-crypto-api"),
})

const kernelCryptoApiConsumerYaml = `name: consumer
version: 0
apps:
 app:
  plugs: [kernel-crypto-api]
`

const kernelCryptoApiCoreYaml = `name: core
version: 0
type: os
slots:
  kernel-crypto-api:
`

func (s *kernelCryptoApiInterfaceSuite) SetUpTest(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, kernelCryptoApiConsumerYaml, nil, "kernel-crypto-api")
	s.coreSlot, s.coreSlotInfo = MockConnectedSlot(c, kernelCryptoApiCoreYaml, nil, "kernel-crypto-api")
}

func (s *kernelCryptoApiInterfaceSuite) TearDownTest(c *C) {
	dirs.SetRootDir("/")
}

func (s *kernelCryptoApiInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "kernel-crypto-api")
}

func (s *kernelCryptoApiInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.coreSlotInfo), IsNil)
}

func (s *kernelCryptoApiInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *kernelCryptoApiInterfaceSuite) TestAppArmorSpec(c *C) {
	spec := &apparmor.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.coreSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "# Description: Can access the Linux kernel crypto API")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "network alg seqpacket,")
}

func (s *kernelCryptoApiInterfaceSuite) TestSeccompSpec(c *C) {
	spec := &seccomp.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.coreSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "# Description: Can access the Linux kernel crypto API")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "socket AF_NETLINK - NETLINK_CRYPTO")
}

func (s *kernelCryptoApiInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, true)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows access to the Linux kernel crypto API`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "kernel-crypto-api")
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "deny-auto-connection: true")
}

func (s *kernelCryptoApiInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
