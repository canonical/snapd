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

type SchedulerControlInterfaceSuite struct {
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
  plugs: [scheduler-control]
`

const schedctlMockSlotSnapInfoYaml = `name: core
version: 1.0
type: os
slots:
 scheduler-control:
  interface: scheduler-control
`

var _ = Suite(&SchedulerControlInterfaceSuite{
	iface: builtin.MustInterface("scheduler-control"),
})

func (s *SchedulerControlInterfaceSuite) SetUpTest(c *C) {
	s.slot, s.slotInfo = MockConnectedSlot(c, schedctlMockSlotSnapInfoYaml, nil, "scheduler-control")
	s.plug, s.plugInfo = MockConnectedPlug(c, schedctlMockPlugSnapInfoYaml, nil, "scheduler-control")
}

func (s *SchedulerControlInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, true)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows running sched_ext userspace schedulers`)
}

func (s *SchedulerControlInterfaceSuite) TestAutoConnect(c *C) {
	c.Assert(s.iface.AutoConnect(s.plugInfo, s.slotInfo), Equals, true)
}

func (s *SchedulerControlInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "scheduler-control")
}

func (s *SchedulerControlInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *SchedulerControlInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *SchedulerControlInterfaceSuite) TestAppArmorSpec(c *C) {
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
	c.Check(snippet, testutil.Contains, "/sys/fs/bpf/** rw,")
	c.Check(snippet, testutil.Contains, "/sys/devices/system/cpu/cpu*/power/pm_qos_resume_latency_us w,")
	c.Check(snippet, testutil.Contains, "/sys/kernel/debug/energy_model/ r,")
	c.Check(snippet, testutil.Contains, "/sys/kernel/debug/energy_model/** r,")
	c.Check(snippet, testutil.Contains, "/var/snap/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/common/run/scx/root/stats rw,")
}

func (s *SchedulerControlInterfaceSuite) TestSecCompSpec(c *C) {
	seccompSpec := seccomp.NewSpecification(s.plug.AppSet())
	err := seccompSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(seccompSpec.SecurityTags(), DeepEquals, []string{"snap.scx.scx-flash"})
	snippet := seccompSpec.SnippetForTag("snap.scx.scx-flash")
	c.Check(snippet, testutil.Contains, "bpf\n")
	c.Check(snippet, testutil.Contains, "bind\n")
	c.Check(snippet, testutil.Contains, "listen\n")
	c.Check(snippet, testutil.Contains, "accept\n")
	c.Check(snippet, testutil.Contains, "accept4\n")
}

func (s *SchedulerControlInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
