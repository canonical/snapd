// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type NetworkManagerObserveInterfaceSuite struct {
	iface        interfaces.Interface
	plug         *interfaces.ConnectedPlug
	plugInfo     *snap.PlugInfo
	appSlot      *interfaces.ConnectedSlot
	appSlotInfo  *snap.SlotInfo
	coreSlot     *interfaces.ConnectedSlot
	coreSlotInfo *snap.SlotInfo
}

var _ = Suite(&NetworkManagerObserveInterfaceSuite{
	iface: builtin.MustInterface("network-manager-observe"),
})

const networkManagerObserveConsumerYaml = `name: consumer
version: 0
apps:
 app:
  plugs: [network-manager-observe]
`

const networkManagerObserveProducerYaml = `name: producer
version: 0
apps:
 app:
  slots: [network-manager-observe]
`

const networkManagerObserveCoreYaml = `name: core
type: os
version: 0
slots:
  network-manager-observe:
`

func (s *NetworkManagerObserveInterfaceSuite) SetUpTest(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, networkManagerObserveConsumerYaml, nil, "network-manager-observe")
	s.appSlot, s.appSlotInfo = MockConnectedSlot(c, networkManagerObserveProducerYaml, nil, "network-manager-observe")
	s.coreSlot, s.coreSlotInfo = MockConnectedSlot(c, networkManagerObserveCoreYaml, nil, "network-manager-observe")
}

func (s *NetworkManagerObserveInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "network-manager-observe")
}

func (s *NetworkManagerObserveInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.coreSlotInfo), IsNil)
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.appSlotInfo), IsNil)
	// network-manager-observe slot can now be used on snap other than core.
	slot := &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "some-snap"},
		Name:      "network-manager-observe",
		Interface: "network-manager-observe",
	}
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), IsNil)
}

func (s *NetworkManagerObserveInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *NetworkManagerObserveInterfaceSuite) TestAppArmorSpec(c *C) {
	// on a core system with NM slot coming from a regular app snap.
	restore := release.MockOnClassic(false)
	defer restore()

	// connected plug to app slot
	spec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap(), nil))
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.appSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `path="/org/freedesktop/NetworkManager{,/{ActiveConnection,Devices}/*}"`)
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `peer=(label="snap.producer.app"),`)

	// connected app slot to plug
	spec = apparmor.NewSpecification(interfaces.NewSnapAppSet(s.appSlot.Snap(), nil))
	c.Assert(spec.AddConnectedSlot(s.iface, s.plug, s.appSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.producer.app"})
	c.Assert(spec.SnippetForTag("snap.producer.app"), testutil.Contains, `path="/org/freedesktop/NetworkManager{,/{ActiveConnection,Devices}/*}"`)
	c.Assert(spec.SnippetForTag("snap.producer.app"), testutil.Contains, `peer=(label="snap.consumer.app"),`)

	// on a classic system with NM slot coming from the core snap.
	restore = release.MockOnClassic(true)
	defer restore()

	// connected plug to core slot
	spec = apparmor.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap(), nil))
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.coreSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `path="/org/freedesktop/NetworkManager{,/{ActiveConnection,Devices}/*}"`)
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "peer=(label=unconfined),")

	// connected core slot to plug
	spec = apparmor.NewSpecification(interfaces.NewSnapAppSet(s.coreSlot.Snap(), nil))
	c.Assert(spec.AddConnectedSlot(s.iface, s.plug, s.coreSlot), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)
}

func (s *NetworkManagerObserveInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, false)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows observing NetworkManager settings`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "network-manager-observe")
}

func (s *NetworkManagerObserveInterfaceSuite) TestAutoConnect(c *C) {
	c.Assert(s.iface.AutoConnect(s.plugInfo, s.coreSlotInfo), Equals, true)
	c.Assert(s.iface.AutoConnect(s.plugInfo, s.appSlotInfo), Equals, true)
}

func (s *NetworkManagerObserveInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
