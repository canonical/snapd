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
	"fmt"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/dbus"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type BluezInterfaceSuite struct {
	iface        interfaces.Interface
	appSlot      *interfaces.ConnectedSlot
	appSlotInfo  *snap.SlotInfo
	coreSlot     *interfaces.ConnectedSlot
	coreSlotInfo *snap.SlotInfo
	plug         *interfaces.ConnectedPlug
	plugInfo     *snap.PlugInfo
}

var _ = Suite(&BluezInterfaceSuite{
	iface: builtin.MustInterface("bluez"),
})

const bluezConsumerYaml = `name: consumer
version: 0
apps:
 app:
  plugs: [bluez]
`

const bluezConsumerTwoAppsYaml = `name: consumer
version: 0
apps:
 app1:
  plugs: [bluez]
 app2:
  plugs: [bluez]
`

const bluezConsumerThreeAppsYaml = `name: consumer
version: 0
apps:
 app1:
  plugs: [bluez]
 app2:
  plugs: [bluez]
 app3:
`

const bluezProducerYaml = `name: producer
version: 0
apps:
 app:
  slots: [bluez]
`

const bluezProducerTwoAppsYaml = `name: producer
version: 0
apps:
 app1:
  slots: [bluez]
 app2:
  slots: [bluez]
`

const bluezProducerThreeAppsYaml = `name: producer
version: 0
apps:
 app1:
  slots: [bluez]
 app2:
 app3:
  slots: [bluez]
`

const bluezCoreYaml = `name: core
type: os
version: 0
slots:
  bluez:
`

func (s *BluezInterfaceSuite) SetUpTest(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, bluezConsumerYaml, nil, "bluez")
	s.appSlot, s.appSlotInfo = MockConnectedSlot(c, bluezProducerYaml, nil, "bluez")
	s.coreSlot, s.coreSlotInfo = MockConnectedSlot(c, bluezCoreYaml, nil, "bluez")
}

func (s *BluezInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "bluez")
}

func (s *BluezInterfaceSuite) TestAppArmorSpec(c *C) {
	// on a core system with bluez slot coming from a regular app snap.
	restore := release.MockOnClassic(false)
	defer restore()

	// The label uses short form when exactly one app is bound to the bluez slot
	spec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap()))
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.appSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `peer=(label="snap.producer.app"),`)

	// The label glob when all apps are bound to the bluez slot
	slot, _ := MockConnectedSlot(c, bluezProducerTwoAppsYaml, nil, "bluez")
	spec = apparmor.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap()))
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `peer=(label="snap.producer.*"),`)

	// The label uses alternation when some, but not all, apps is bound to the bluez slot
	slot, _ = MockConnectedSlot(c, bluezProducerThreeAppsYaml, nil, "bluez")
	spec = apparmor.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap()))
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `peer=(label="snap.producer.{app1,app3}"),`)

	// The label uses short form when exactly one app is bound to the bluez plug
	spec = apparmor.NewSpecification(interfaces.NewSnapAppSet(s.appSlot.Snap()))
	c.Assert(spec.AddConnectedSlot(s.iface, s.plug, s.appSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.producer.app"})
	c.Assert(spec.SnippetForTag("snap.producer.app"), testutil.Contains, `peer=(label="snap.consumer.app"),`)

	// The label glob when all apps are bound to the bluez plug
	plug, _ := MockConnectedPlug(c, bluezConsumerTwoAppsYaml, nil, "bluez")
	spec = apparmor.NewSpecification(interfaces.NewSnapAppSet(s.appSlot.Snap()))
	c.Assert(spec.AddConnectedSlot(s.iface, plug, s.appSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.producer.app"})
	c.Assert(spec.SnippetForTag("snap.producer.app"), testutil.Contains, `peer=(label="snap.consumer.*"),`)

	// The label uses alternation when some, but not all, apps is bound to the bluez plug
	plug, _ = MockConnectedPlug(c, bluezConsumerThreeAppsYaml, nil, "bluez")
	spec = apparmor.NewSpecification(interfaces.NewSnapAppSet(s.appSlot.Snap()))
	c.Assert(spec.AddConnectedSlot(s.iface, plug, s.appSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.producer.app"})
	c.Assert(spec.SnippetForTag("snap.producer.app"), testutil.Contains, `peer=(label="snap.consumer.{app1,app2}"),`)

	// permanent slot have a non-nil security snippet for apparmor
	spec = apparmor.NewSpecification(interfaces.NewSnapAppSet(s.appSlotInfo.Snap))
	c.Assert(spec.AddPermanentSlot(s.iface, s.appSlotInfo), IsNil)

	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.producer.app"})
	c.Assert(spec.SnippetForTag("snap.producer.app"), testutil.Contains, `peer=(label=unconfined),`)

	spec = apparmor.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap()))
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.appSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `peer=(label="snap.producer.app"),`)

	// on a classic system with bluez slot coming from the core snap.
	restore = release.MockOnClassic(true)
	defer restore()

	// connected plug to core slot
	spec = apparmor.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap()))
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.coreSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "peer=(name=org.bluez, label=unconfined)")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "peer=(name=org.bluez.obex, label=unconfined)")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "peer=(name=org.bluez.mesh, label=unconfined)")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "peer=(label=unconfined),")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `interface=org.freedesktop.DBus.ObjectManager`)
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `interface=org.freedesktop.DBus.*`)

	// connected core slot to plug
	spec = apparmor.NewSpecification(interfaces.NewSnapAppSet(s.coreSlot.Snap()))
	c.Assert(spec.AddConnectedSlot(s.iface, s.plug, s.coreSlot), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)

	// permanent core slot
	spec = apparmor.NewSpecification(interfaces.NewSnapAppSet(s.coreSlotInfo.Snap))
	c.Assert(spec.AddPermanentSlot(s.iface, s.coreSlotInfo), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)
}

func (s *BluezInterfaceSuite) TestDBusSpec(c *C) {
	// on a core system with bluez slot coming from a regular app snap.
	restore := release.MockOnClassic(false)
	defer restore()

	spec := dbus.NewSpecification(interfaces.NewSnapAppSet(s.appSlotInfo.Snap))
	c.Assert(spec.AddPermanentSlot(s.iface, s.appSlotInfo), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.producer.app"})
	c.Assert(spec.SnippetForTag("snap.producer.app"), testutil.Contains, `<allow own="org.bluez"/>`)

	// on a classic system with bluez slot coming from the core snap.
	restore = release.MockOnClassic(true)
	defer restore()

	spec = dbus.NewSpecification(interfaces.NewSnapAppSet(s.coreSlotInfo.Snap))
	c.Assert(spec.AddPermanentSlot(s.iface, s.coreSlotInfo), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)
}

func (s *BluezInterfaceSuite) TestSecCompSpec(c *C) {
	// on a core system with bluez slot coming from a regular app snap.
	restore := release.MockOnClassic(false)
	defer restore()

	spec := seccomp.NewSpecification(interfaces.NewSnapAppSet(s.appSlotInfo.Snap))
	c.Assert(spec.AddPermanentSlot(s.iface, s.appSlotInfo), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.producer.app"})
	c.Assert(spec.SnippetForTag("snap.producer.app"), testutil.Contains, "listen\n")

	// on a classic system with bluez slot coming from the core snap.
	restore = release.MockOnClassic(true)
	defer restore()

	spec = seccomp.NewSpecification(interfaces.NewSnapAppSet(s.coreSlotInfo.Snap))
	c.Assert(spec.AddPermanentSlot(s.iface, s.coreSlotInfo), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)

}

func (s *BluezInterfaceSuite) TestUDevSpec(c *C) {
	// on a core system with bluez slot coming from a regular app snap.
	restore := release.MockOnClassic(false)
	defer restore()

	spec := udev.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap()))
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.appSlot), IsNil)
	c.Assert(spec.Snippets(), HasLen, 2)
	c.Assert(spec.Snippets(), testutil.Contains, `# bluez
KERNEL=="rfkill", TAG+="snap_consumer_app"`)
	c.Assert(spec.Snippets(), testutil.Contains, fmt.Sprintf(`TAG=="snap_consumer_app", SUBSYSTEM!="module", SUBSYSTEM!="subsystem", RUN+="%v/snap-device-helper $env{ACTION} snap_consumer_app $devpath $major:$minor"`, dirs.DistroLibExecDir))

	// on a classic system with bluez slot coming from the core snap.
	restore = release.MockOnClassic(true)
	defer restore()

	spec = udev.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap()))
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.coreSlot), IsNil)
	c.Assert(spec.Snippets(), HasLen, 2)
	c.Assert(spec.Snippets()[0], testutil.Contains, `KERNEL=="rfkill", TAG+="snap_consumer_app"`)
	c.Assert(spec.Snippets(), testutil.Contains, fmt.Sprintf(`TAG=="snap_consumer_app", SUBSYSTEM!="module", SUBSYSTEM!="subsystem", RUN+="%v/snap-device-helper $env{ACTION} snap_consumer_app $devpath $major:$minor"`, dirs.DistroLibExecDir))

}

func (s *BluezInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, false)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows operating as the bluez service`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "bluez")
}

func (s *BluezInterfaceSuite) TestAutoConnect(c *C) {
	c.Assert(s.iface.AutoConnect(s.plugInfo, s.coreSlotInfo), Equals, true)
	c.Assert(s.iface.AutoConnect(s.plugInfo, s.appSlotInfo), Equals, true)
}

func (s *BluezInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
