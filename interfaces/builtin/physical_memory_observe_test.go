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
	"github.com/snapcore/snapd/interfaces/udev"

	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type PhysicalMemoryObserveInterfaceSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

var _ = Suite(&PhysicalMemoryObserveInterfaceSuite{
	iface: builtin.MustInterface("physical-memory-observe"),
})

const physicalMemoryObserveConsumerYaml = `name: consumer
apps:
 app:
  plugs: [physical-memory-observe]
`

const physicalMemoryObserveCoreYaml = `name: core
type: os
slots:
  physical-memory-observe:
`

func (s *PhysicalMemoryObserveInterfaceSuite) SetUpTest(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, physicalMemoryObserveConsumerYaml, nil, "physical-memory-observe")
	s.slot, s.slotInfo = MockConnectedSlot(c, physicalMemoryObserveCoreYaml, nil, "physical-memory-observe")
}

func (s *PhysicalMemoryObserveInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "physical-memory-observe")
}

func (s *PhysicalMemoryObserveInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.SanitizeSlot(s.iface, s.slotInfo), IsNil)
	slot := &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "some-snap"},
		Name:      "physical-memory-observe",
		Interface: "physical-memory-observe",
	}
	c.Assert(interfaces.SanitizeSlot(s.iface, slot), ErrorMatches,
		"physical-memory-observe slots are reserved for the core snap")
}

func (s *PhysicalMemoryObserveInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.SanitizePlug(s.iface, s.plugInfo), IsNil)
}

func (s *PhysicalMemoryObserveInterfaceSuite) TestAppArmorSpec(c *C) {
	spec := &apparmor.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `/dev/mem r,`)
}

func (s *PhysicalMemoryObserveInterfaceSuite) TestUDevSpec(c *C) {
	spec := &udev.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.Snippets(), HasLen, 1)
	c.Assert(spec.Snippets(), testutil.Contains, `# physical-memory-observe
KERNEL=="mem", TAG+="snap_consumer_app"`)
}

func (s *PhysicalMemoryObserveInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, true)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows read access to all physical memory`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "physical-memory-observe")
}

func (s *PhysicalMemoryObserveInterfaceSuite) TestAutoConnect(c *C) {
	// FIXME: fix AutoConnect methods to use ConnectedPlug/Slot
	c.Assert(s.iface.AutoConnect(&interfaces.Plug{PlugInfo: s.plugInfo}, &interfaces.Slot{SlotInfo: s.slotInfo}), Equals, true)
}

func (s *PhysicalMemoryObserveInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
