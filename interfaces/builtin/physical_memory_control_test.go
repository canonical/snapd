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
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type PhysicalMemoryControlInterfaceSuite struct {
	iface interfaces.Interface
	slot  *interfaces.Slot
	plug  *interfaces.Plug
}

var _ = Suite(&PhysicalMemoryControlInterfaceSuite{
	iface: builtin.MustInterface("physical-memory-control"),
})

const physicalMemoryControlConsumerYaml = `name: consumer
apps:
 app:
  plugs: [physical-memory-control]
`

const physicalMemoryControlCoreYaml = `name: core
type: os
slots:
  physical-memory-control:
`

func (s *PhysicalMemoryControlInterfaceSuite) SetUpTest(c *C) {
	s.plug = MockPlug(c, physicalMemoryControlConsumerYaml, nil, "physical-memory-control")
	s.slot = MockSlot(c, physicalMemoryControlCoreYaml, nil, "physical-memory-control")
}

func (s *PhysicalMemoryControlInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "physical-memory-control")
}

func (s *PhysicalMemoryControlInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(s.slot.Sanitize(s.iface), IsNil)
	slot := &interfaces.Slot{SlotInfo: &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "some-snap"},
		Name:      "physical-memory-control",
		Interface: "physical-memory-control",
	}}
	c.Assert(slot.Sanitize(s.iface), ErrorMatches,
		"physical-memory-control slots are reserved for the core snap")
}

func (s *PhysicalMemoryControlInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(s.plug.Sanitize(s.iface), IsNil)
}

func (s *PhysicalMemoryControlInterfaceSuite) TestAppArmorSpec(c *C) {
	spec := &apparmor.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `/dev/mem rw,`)
}

func (s *PhysicalMemoryControlInterfaceSuite) TestUDevSpec(c *C) {
	spec := &udev.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.Snippets(), HasLen, 1)
	c.Assert(spec.Snippets()[0], DeepEquals, `KERNEL=="mem", TAG+="snap_consumer_app"`)
}

func (s *PhysicalMemoryControlInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, true)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows write access to all physical memory`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "physical-memory-control")
}

func (s *PhysicalMemoryControlInterfaceSuite) TestAutoConnect(c *C) {
	c.Assert(s.iface.AutoConnect(s.plug, s.slot), Equals, true)
}

func (s *PhysicalMemoryControlInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
