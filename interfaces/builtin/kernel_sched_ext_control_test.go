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
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type KernelSchedExtControlInterfaceSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

const schedctlMockPlugSnapInfoYaml = `name: scx
version: 1.0
apps:
 scx-flash:
  command: bin/scx_flash
  plugs: [kernel-sched-ext-control]
`

const schedctlMockSlotSnapInfoYaml = `name: core
version: 1.0
type: os
slots:
 kernel-sched-ext-control:
  interface: kernel-sched-ext-control
`

var _ = Suite(&KernelSchedExtControlInterfaceSuite{
	iface: builtin.MustInterface("kernel-sched-ext-control"),
})

func (s *KernelSchedExtControlInterfaceSuite) SetUpTest(c *C) {
	s.slot, s.slotInfo = MockConnectedSlot(c, schedctlMockSlotSnapInfoYaml, nil, "kernel-sched-ext-control")
	s.plug, s.plugInfo = MockConnectedPlug(c, schedctlMockPlugSnapInfoYaml, nil, "kernel-sched-ext-control")
}

func (s *KernelSchedExtControlInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, true)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows running sched_ext userspace schedulers`)
}

func (s *KernelSchedExtControlInterfaceSuite) TestAutoConnect(c *C) {
	c.Assert(s.iface.AutoConnect(s.plugInfo, s.slotInfo), Equals, true)
}

func (s *KernelSchedExtControlInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "kernel-sched-ext-control")
}

func (s *KernelSchedExtControlInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *KernelSchedExtControlInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *KernelSchedExtControlInterfaceSuite) TestAppArmorSpec(c *C) {
	apparmorSpec := apparmor.NewSpecification(s.plug.AppSet())
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.scx.scx-flash"})
	snippet := apparmorSpec.SnippetForTag("snap.scx.scx-flash")
	c.Check(snippet, testutil.Contains, "capability bpf,")
	c.Check(snippet, testutil.Contains, "capability perfmon,")
	c.Check(snippet, testutil.Contains, "capability sys_resource,")
	c.Check(snippet, testutil.Contains, "/sys/kernel/btf/vmlinux r,")
	c.Check(snippet, testutil.Contains, "/sys/kernel/sched_ext/ r,")
	c.Check(snippet, testutil.Contains, "/sys/kernel/sched_ext/hotplug_seq r,")
	c.Check(snippet, testutil.Contains, "/sys/kernel/sched_ext/state r,")
	c.Check(snippet, testutil.Contains, "/sys/fs/bpf/ r,")
	c.Check(snippet, testutil.Contains, "/sys/fs/bpf/[^s.]**    rw,")
	c.Check(snippet, testutil.Contains, "/sys/fs/bpf/s[^n]**    rw,")
	c.Check(snippet, testutil.Contains, "/sys/fs/bpf/sn[^a]**   rw,")
	c.Check(snippet, testutil.Contains, "/sys/fs/bpf/sna[^p]**  rw,")
	c.Check(snippet, testutil.Contains, "/sys/fs/bpf/snap[^/]** rw,")
	c.Check(snippet, testutil.Contains, "/sys/fs/bpf/{s,sn,sna}{,/} rw,")
}

func (s *KernelSchedExtControlInterfaceSuite) TestSecCompSpec(c *C) {
	seccompSpec := seccomp.NewSpecification(s.plug.AppSet())
	err := seccompSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(seccompSpec.SecurityTags(), DeepEquals, []string{"snap.scx.scx-flash"})
	snippet := seccompSpec.SnippetForTag("snap.scx.scx-flash")
	c.Check(snippet, testutil.Contains, "bpf\n")
}

func (s *KernelSchedExtControlInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
