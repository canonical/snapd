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
	"fmt"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type XilinxDmaInterfaceSuite struct {
	iface        interfaces.Interface
	coreSlotInfo *snap.SlotInfo
	coreSlot     *interfaces.ConnectedSlot
	plugInfo     *snap.PlugInfo
	plug         *interfaces.ConnectedPlug
}

var _ = Suite(&XilinxDmaInterfaceSuite{
	iface: builtin.MustInterface("xilinx-dma"),
})

const xilinxDmaConsumerYaml = `name: consumer
version: 0
apps:
 app:
  plugs: [xilinx-dma]
`

const xilinxDmaCoreYaml = `name: core
version: 0
type: os
slots:
  xilinx-dma:
`

func (s *XilinxDmaInterfaceSuite) SetUpTest(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, xilinxDmaConsumerYaml, nil, "xilinx-dma")
	s.coreSlot, s.coreSlotInfo = MockConnectedSlot(c, xilinxDmaCoreYaml, nil, "xilinx-dma")
}

func (s *XilinxDmaInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "xilinx-dma")
}

func (s *XilinxDmaInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.coreSlotInfo), IsNil)
}

func (s *XilinxDmaInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *XilinxDmaInterfaceSuite) TestAppArmorSpec(c *C) {
	spec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap()))
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.coreSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `/dev/xdma[0-9]*_{c2h,h2c,events}_[0-9]* rw,`)
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `/dev/xdma/card[0-9]*/** rw,`)
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `/dev/xdma[0-9]*_{control,user,xvc} rw,`)
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `/sys/module/xdma/parameters/* r,`)
}

func (s *XilinxDmaInterfaceSuite) TestUDevSpec(c *C) {
	spec := udev.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap()))
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.coreSlot), IsNil)
	c.Assert(spec.Snippets(), HasLen, 2)
	c.Assert(spec.Snippets(), testutil.Contains, `# xilinx-dma
SUBSYSTEM=="xdma", TAG+="snap_consumer_app"`)
	c.Assert(spec.Snippets(), testutil.Contains, fmt.Sprintf(
		`TAG=="snap_consumer_app", SUBSYSTEM!="module", SUBSYSTEM!="subsystem", RUN+="%v/snap-device-helper snap_consumer_app"`, dirs.DistroLibExecDir))
}

func (s *XilinxDmaInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, true)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows access to Xilinx DMA IP on a connected PCIe card`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "deny-auto-connection: true")
	c.Assert(si.BaseDeclarationPlugs, testutil.Contains, "allow-installation: false")
}

func (s *XilinxDmaInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
