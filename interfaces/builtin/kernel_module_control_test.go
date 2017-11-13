// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type KernelModuleControlInterfaceSuite struct {
	iface interfaces.Interface
	slot  *interfaces.Slot
	plug  *interfaces.Plug
}

var _ = Suite(&KernelModuleControlInterfaceSuite{
	iface: builtin.MustInterface("kernel-module-control"),
})

const kernelmodctlConsumerYaml = `name: consumer
apps:
 app:
  plugs: [kernel-module-control]
`

const kernelmodctlCoreYaml = `name: core
type: os
slots:
  kernel-module-control:
`

func (s *KernelModuleControlInterfaceSuite) SetUpTest(c *C) {
	s.plug = MockPlug(c, kernelmodctlConsumerYaml, nil, "kernel-module-control")
	s.slot = MockSlot(c, kernelmodctlCoreYaml, nil, "kernel-module-control")
}

func (s *KernelModuleControlInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "kernel-module-control")
}

func (s *KernelModuleControlInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(s.slot.Sanitize(s.iface), IsNil)
	slot := &interfaces.Slot{SlotInfo: &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "some-snap"},
		Name:      "kernel-module-control",
		Interface: "kernel-module-control",
	}}
	c.Assert(slot.Sanitize(s.iface), ErrorMatches,
		"kernel-module-control slots are reserved for the core snap")
}

func (s *KernelModuleControlInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(s.plug.Sanitize(s.iface), IsNil)
}

func (s *KernelModuleControlInterfaceSuite) TestAppArmorSpec(c *C) {
	spec := &apparmor.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, nil, s.slot, nil), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "capability sys_module,")
}

func (s *KernelModuleControlInterfaceSuite) TestSecCompSpec(c *C) {
	spec := &seccomp.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, nil, s.slot, nil), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "finit_module\n")
}

func (s *KernelModuleControlInterfaceSuite) TestUDevSpec(c *C) {
	spec := &udev.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, nil, s.slot, nil), IsNil)
	c.Assert(spec.Snippets(), HasLen, 1)
	c.Assert(spec.Snippets(), testutil.Contains, `# kernel-module-control
KERNEL=="mem", TAG+="snap_consumer_app"`)
}

func (s *KernelModuleControlInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, true)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows insertion, removal and querying of kernel modules`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "kernel-module-control")
}

func (s *KernelModuleControlInterfaceSuite) TestAutoConnect(c *C) {
	c.Assert(s.iface.AutoConnect(s.plug, s.slot), Equals, true)
}

func (s *KernelModuleControlInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
