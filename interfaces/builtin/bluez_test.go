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
	"github.com/snapcore/snapd/interfaces/dbus"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/testutil"
)

type BluezInterfaceSuite struct {
	iface    interfaces.Interface
	appSlot  *interfaces.Slot
	coreSlot *interfaces.Slot
	plug     *interfaces.Plug
}

var _ = Suite(&BluezInterfaceSuite{
	iface: builtin.MustInterface("bluez"),
})

const bluezConsumerYaml = `name: consumer
apps:
 app:
  plugs: [bluez]
`

const bluezConsumerTwoAppsYaml = `name: consumer
apps:
 app1:
  plugs: [bluez]
 app2:
  plugs: [bluez]
`

const bluezConsumerThreeAppsYaml = `name: consumer
apps:
 app1:
  plugs: [bluez]
 app2:
  plugs: [bluez]
 app3:
`

const bluezProducerYaml = `name: producer
apps:
 app:
  slots: [bluez]
`

const bluezProducerTwoAppsYaml = `name: producer
apps:
 app1:
  slots: [bluez]
 app2:
  slots: [bluez]
`

const bluezProducerThreeAppsYaml = `name: producer
apps:
 app1:
  slots: [bluez]
 app2:
 app3:
  slots: [bluez]
`

const bluezCoreYaml = `name: core
slots:
  bluez:
`

func (s *BluezInterfaceSuite) SetUpTest(c *C) {
	s.plug = MockPlug(c, bluezConsumerYaml, nil, "bluez")
	s.appSlot = MockSlot(c, bluezProducerYaml, nil, "bluez")
	s.coreSlot = MockSlot(c, bluezCoreYaml, nil, "bluez")
}

func (s *BluezInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "bluez")
}

func (s *BluezInterfaceSuite) TestAppArmorSpec(c *C) {
	// on a core system with bluez slot coming from a regular app snap.
	restore := release.MockOnClassic(false)
	defer restore()

	// The label uses short form when exactly one app is bound to the bluez slot
	spec := &apparmor.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.appSlot, nil), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `peer=(label="snap.producer.app"),`)

	// The label glob when all apps are bound to the bluez slot
	slot := MockSlot(c, bluezProducerTwoAppsYaml, nil, "bluez")
	spec = &apparmor.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, slot, nil), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `peer=(label="snap.producer.*"),`)

	// The label uses alternation when some, but not all, apps is bound to the bluez slot
	slot = MockSlot(c, bluezProducerThreeAppsYaml, nil, "bluez")
	spec = &apparmor.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, slot, nil), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `peer=(label="snap.producer.{app1,app3}"),`)

	// The label uses short form when exactly one app is bound to the bluez plug
	spec = &apparmor.Specification{}
	c.Assert(spec.AddConnectedSlot(s.iface, s.plug, nil, s.appSlot, nil), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.producer.app"})
	c.Assert(spec.SnippetForTag("snap.producer.app"), testutil.Contains, `peer=(label="snap.consumer.app"),`)

	// The label glob when all apps are bound to the bluez plug
	plug := MockPlug(c, bluezConsumerTwoAppsYaml, nil, "bluez")
	spec = &apparmor.Specification{}
	c.Assert(spec.AddConnectedSlot(s.iface, plug, nil, s.appSlot, nil), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.producer.app"})
	c.Assert(spec.SnippetForTag("snap.producer.app"), testutil.Contains, `peer=(label="snap.consumer.*"),`)

	// The label uses alternation when some, but not all, apps is bound to the bluez plug
	plug = MockPlug(c, bluezConsumerThreeAppsYaml, nil, "bluez")
	spec = &apparmor.Specification{}
	c.Assert(spec.AddConnectedSlot(s.iface, plug, nil, s.appSlot, nil), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.producer.app"})
	c.Assert(spec.SnippetForTag("snap.producer.app"), testutil.Contains, `peer=(label="snap.consumer.{app1,app2}"),`)

	// permanent slot have a non-nil security snippet for apparmor
	spec = &apparmor.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.appSlot, nil), IsNil)
	c.Assert(spec.AddPermanentSlot(s.iface, s.appSlot.SlotInfo), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app", "snap.producer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `peer=(label="snap.producer.app"),`)
	c.Assert(spec.SnippetForTag("snap.producer.app"), testutil.Contains, `peer=(label=unconfined),`)

	// on a classic system with bluez slot coming from the core snap.
	restore = release.MockOnClassic(true)
	defer restore()

	// connected plug to core slot
	spec = &apparmor.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.coreSlot, nil), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "peer=(name=org.bluez, label=unconfined)")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "peer=(name=org.bluez.obex, label=unconfined)")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "peer=(label=unconfined),")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `interface=org.freedesktop.DBus.ObjectManager`)
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `interface=org.freedesktop.DBus.*`)

	// connected core slot to plug
	spec = &apparmor.Specification{}
	c.Assert(spec.AddConnectedSlot(s.iface, s.plug, nil, s.coreSlot, nil), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)

	// permanent core slot
	spec = &apparmor.Specification{}
	c.Assert(spec.AddPermanentSlot(s.iface, s.coreSlot.SlotInfo), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)
}

func (s *BluezInterfaceSuite) TestDBusSpec(c *C) {
	// on a core system with bluez slot coming from a regular app snap.
	restore := release.MockOnClassic(false)
	defer restore()

	spec := &dbus.Specification{}
	c.Assert(spec.AddPermanentSlot(s.iface, s.appSlot.SlotInfo), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.producer.app"})
	c.Assert(spec.SnippetForTag("snap.producer.app"), testutil.Contains, `<allow own="org.bluez"/>`)

	// on a classic system with bluez slot coming from the core snap.
	restore = release.MockOnClassic(true)
	defer restore()

	spec = &dbus.Specification{}
	c.Assert(spec.AddPermanentSlot(s.iface, s.coreSlot.SlotInfo), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)
}

func (s *BluezInterfaceSuite) TestSecCompSpec(c *C) {
	// on a core system with bluez slot coming from a regular app snap.
	restore := release.MockOnClassic(false)
	defer restore()

	spec := &seccomp.Specification{}
	c.Assert(spec.AddPermanentSlot(s.iface, s.appSlot.SlotInfo), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.producer.app"})
	c.Assert(spec.SnippetForTag("snap.producer.app"), testutil.Contains, "listen\n")

	// on a classic system with bluez slot coming from the core snap.
	restore = release.MockOnClassic(true)
	defer restore()

	spec = &seccomp.Specification{}
	c.Assert(spec.AddPermanentSlot(s.iface, s.coreSlot.SlotInfo), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)

}

func (s *BluezInterfaceSuite) TestUDevSpec(c *C) {
	// on a core system with bluez slot coming from a regular app snap.
	restore := release.MockOnClassic(false)
	defer restore()

	spec := &udev.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.appSlot, nil), IsNil)
	c.Assert(spec.Snippets(), HasLen, 1)
	c.Assert(spec.Snippets()[0], testutil.Contains, `KERNEL=="rfkill", TAG+="snap_consumer_app"`)

	// on a classic system with bluez slot coming from the core snap.
	restore = release.MockOnClassic(true)
	defer restore()

	spec = &udev.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.coreSlot, nil), IsNil)
	c.Assert(spec.Snippets(), HasLen, 1)
	c.Assert(spec.Snippets()[0], testutil.Contains, `KERNEL=="rfkill", TAG+="snap_consumer_app"`)

}

func (s *BluezInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, false)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows operating as the bluez service`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "bluez")
}

func (s *BluezInterfaceSuite) TestAutoConnect(c *C) {
	c.Assert(s.iface.AutoConnect(s.plug, s.coreSlot), Equals, true)
	c.Assert(s.iface.AutoConnect(s.plug, s.appSlot), Equals, true)
}

func (s *BluezInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
