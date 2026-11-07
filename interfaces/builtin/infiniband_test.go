// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type InfinibandInterfaceSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

var _ = Suite(&InfinibandInterfaceSuite{
	iface: builtin.MustInterface("infiniband"),
})

func (s *InfinibandInterfaceSuite) SetUpTest(c *C) {
	const mockPlugSnapInfoYaml = `name: consumer
version: 1.0
apps:
 app:
  command: foo
  plugs: [infiniband]
`
	const mockSlotSnapInfoYaml = `name: core
version: 1.0
type: os
slots:
 infiniband:
  interface: infiniband
`
	s.slot, s.slotInfo = MockConnectedSlot(c, mockSlotSnapInfoYaml, nil, "infiniband")
	s.plug, s.plugInfo = MockConnectedPlug(c, mockPlugSnapInfoYaml, nil, "infiniband")
}

func (s *InfinibandInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "infiniband")
}

func (s *InfinibandInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *InfinibandInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *InfinibandInterfaceSuite) TestAppArmorSpec(c *C) {
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	spec := apparmor.NewSpecification(appSet)
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "/dev/infiniband/* rw,")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "/sys/kernel/iommu_groups/{,**} r,")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "/var/tmp/doca_mlx5_hws* rw,")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "/var/tmp/dpdk_net_mlx5_* rw,")
}

func (s *InfinibandInterfaceSuite) TestSecCompSpec(c *C) {
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	spec := seccomp.NewSpecification(appSet)
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "socket AF_NETLINK - NETLINK_RDMA")
}

func (s *InfinibandInterfaceSuite) TestUDevSpec(c *C) {
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	spec := udev.NewSpecification(appSet)
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.Snippets(), HasLen, 5)
	c.Assert(spec.Snippets(), testutil.Contains, `# infiniband
SUBSYSTEM=="infiniband_verbs", TAG+="snap_consumer_app"`)
	c.Assert(spec.Snippets(), testutil.Contains, `# infiniband
SUBSYSTEM=="infiniband_mad", TAG+="snap_consumer_app"`)
	c.Assert(spec.Snippets(), testutil.Contains, `# infiniband
SUBSYSTEM=="rdma_cm", TAG+="snap_consumer_app"`)
	c.Assert(spec.Snippets(), testutil.Contains, `# infiniband
SUBSYSTEM=="infiniband", TAG+="snap_consumer_app"`)
	c.Assert(spec.Snippets(), testutil.Contains, fmt.Sprintf(`TAG=="snap_consumer_app", SUBSYSTEM!="module", SUBSYSTEM!="subsystem", RUN+="%v/snap-device-helper $env{ACTION} snap_consumer_app $devpath $major:$minor"`, dirs.DistroLibExecDir))
}

func (s *InfinibandInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, true)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows direct access to InfiniBand and RDMA hardware`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "infiniband")
}

func (s *InfinibandInterfaceSuite) TestAutoConnect(c *C) {
	c.Assert(s.iface.AutoConnect(s.plugInfo, s.slotInfo), Equals, true)
}

func (s *InfinibandInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
