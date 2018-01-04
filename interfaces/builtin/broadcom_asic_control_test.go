// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type BroadcomAsicControlSuite struct {
	iface interfaces.Interface
	slot  *interfaces.Slot
	plug  *interfaces.Plug
}

var _ = Suite(&BroadcomAsicControlSuite{
	iface: builtin.MustInterface("broadcom-asic-control"),
})

func (s *BroadcomAsicControlSuite) SetUpTest(c *C) {
	const producerYaml = `name: core
type: os
slots:
  broadcom-asic-control:
`
	info := snaptest.MockInfo(c, producerYaml, nil)
	s.slot = &interfaces.Slot{SlotInfo: info.Slots["broadcom-asic-control"]}

	const consumerYaml = `name: consumer
apps:
 app:
  plugs: [broadcom-asic-control]
`
	info = snaptest.MockInfo(c, consumerYaml, nil)
	s.plug = &interfaces.Plug{PlugInfo: info.Plugs["broadcom-asic-control"]}
}

func (s *BroadcomAsicControlSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "broadcom-asic-control")
}

func (s *BroadcomAsicControlSuite) TestSanitizeSlot(c *C) {
	c.Assert(s.slot.Sanitize(s.iface), IsNil)
	slot := &interfaces.Slot{SlotInfo: &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "some-snap"},
		Name:      "broadcom-asic-control",
		Interface: "broadcom-asic-control",
	}}
	c.Assert(slot.Sanitize(s.iface), ErrorMatches,
		"broadcom-asic-control slots are reserved for the core snap")
}

func (s *BroadcomAsicControlSuite) TestSanitizePlug(c *C) {
	c.Assert(s.plug.Sanitize(s.iface), IsNil)
}

func (s *BroadcomAsicControlSuite) TestAppArmorSpec(c *C) {
	spec := &apparmor.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, nil, s.slot, nil), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "/sys/module/linux_kernel_bde/{,**} r,")
}

func (s *BroadcomAsicControlSuite) TestUDevSpec(c *C) {
	spec := &udev.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, nil, s.slot, nil), IsNil)
	c.Assert(spec.Snippets(), HasLen, 3)
	c.Assert(spec.Snippets(), testutil.Contains, `# broadcom-asic-control
SUBSYSTEM=="net", KERNEL=="bcm[0-9]*", TAG+="snap_consumer_app"`)
	c.Assert(spec.Snippets(), testutil.Contains, `TAG=="snap_consumer_app", RUN+="/lib/udev/snappy-app-dev $env{ACTION} snap_consumer_app $devpath $major:$minor"`)
}

func (s *BroadcomAsicControlSuite) TestKModSpec(c *C) {
	spec := &kmod.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, nil, s.slot, nil), IsNil)
	c.Assert(spec.Modules(), DeepEquals, map[string]bool{
		"linux-user-bde":   true,
		"linux-kernel-bde": true,
		"linux-bcm-knet":   true,
	})
}

func (s *BroadcomAsicControlSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, true)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, "allows using the broadcom-asic kernel module")
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "broadcom-asic-control")
}

func (s *BroadcomAsicControlSuite) TestAutoConnect(c *C) {
	c.Assert(s.iface.AutoConnect(s.plug, s.slot), Equals, true)
}

func (s *BroadcomAsicControlSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
