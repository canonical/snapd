// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/dbus"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type AvahiObserveInterfaceSuite struct {
	iface         interfaces.Interface
	plug          *interfaces.ConnectedPlug
	plugInfo      *snap.PlugInfo
	appSlot       *interfaces.ConnectedSlot
	appSlotInfo   *snap.SlotInfo
	coreSlot      *interfaces.ConnectedSlot
	coreSlotInfo  *snap.SlotInfo
	snapdSlot     *interfaces.ConnectedSlot
	snapdSlotInfo *snap.SlotInfo
}

var _ = Suite(&AvahiObserveInterfaceSuite{
	iface: builtin.MustInterface("avahi-observe"),
})

const avahiObserveConsumerYaml = `name: consumer
version: 0
apps:
 app:
  plugs: [avahi-observe]
`

const avahiObserveProducerYaml = `name: producer
version: 0
apps:
 app:
  slots: [avahi-observe]
`

const avahiObserveCoreYaml = `name: core
version: 0
type: os
slots:
  avahi-observe:
`

const avahiObserveSnapdYaml = `name: snapd
version: 0
type: snapd
slots:
  avahi-observe:
`

func (s *AvahiObserveInterfaceSuite) SetUpTest(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, avahiObserveConsumerYaml, nil, "avahi-observe")
	s.appSlot, s.appSlotInfo = MockConnectedSlot(c, avahiObserveProducerYaml, nil, "avahi-observe")
	s.coreSlot, s.coreSlotInfo = MockConnectedSlot(c, avahiObserveCoreYaml, nil, "avahi-observe")
	s.snapdSlot, s.snapdSlotInfo = MockConnectedSlot(c, avahiObserveSnapdYaml, nil, "avahi-observe")
}

func (s *AvahiObserveInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "avahi-observe")
}

func (s *AvahiObserveInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.coreSlotInfo), IsNil)
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.appSlotInfo), IsNil)
	// avahi-observe slot can now be used on snap other than core.
	slot := &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "some-snap"},
		Name:      "avahi-observe",
		Interface: "avahi-observe",
	}
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), IsNil)
}

func (s *AvahiObserveInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *AvahiObserveInterfaceSuite) testAppArmorSpecWithProducer(c *C,
	slot *interfaces.ConnectedSlot, slotInfo *snap.SlotInfo,
) {
	// connected plug to app slot
	appSet := mylog.Check2(interfaces.NewSnapAppSet(s.plug.Snap(), nil))

	spec := apparmor.NewSpecification(appSet)
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "name=org.freedesktop.Avahi")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `peer=(label="snap.producer.app"),`)
	// make sure observe does have observe but not control capabilities
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `interface=org.freedesktop.Avahi.AddressResolver`)
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `interface=org.freedesktop.Avahi.HostNameResolver`)
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `interface=org.freedesktop.Avahi.ServiceResolver`)
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `interface=org.freedesktop.Avahi.RecordBrowser`)
	// control capabilities
	c.Assert(spec.SnippetForTag("snap.consumer.app"), Not(testutil.Contains), `member=Set*`)
	c.Assert(spec.SnippetForTag("snap.consumer.app"), Not(testutil.Contains), `member=EntryGroupNew`)
	c.Assert(spec.SnippetForTag("snap.consumer.app"), Not(testutil.Contains), `interface=org.freedesktop.Avahi.EntryGroup`)

	// connected app slot to plug
	appSet = mylog.Check2(interfaces.NewSnapAppSet(slot.Snap(), nil))

	spec = apparmor.NewSpecification(appSet)
	c.Assert(spec.AddConnectedSlot(s.iface, s.plug, slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.producer.app"})
	c.Assert(spec.SnippetForTag("snap.producer.app"), testutil.Contains, `interface=org.freedesktop.Avahi`)
	c.Assert(spec.SnippetForTag("snap.producer.app"), testutil.Contains, `peer=(label="snap.consumer.app"),`)
	// make sure observe does have observe but not control capabilities
	c.Assert(spec.SnippetForTag("snap.producer.app"), testutil.Contains, `interface=org.freedesktop.Avahi.AddressResolver`)
	c.Assert(spec.SnippetForTag("snap.producer.app"), testutil.Contains, `interface=org.freedesktop.Avahi.HostNameResolver`)
	c.Assert(spec.SnippetForTag("snap.producer.app"), testutil.Contains, `interface=org.freedesktop.Avahi.ServiceResolver`)
	c.Assert(spec.SnippetForTag("snap.producer.app"), testutil.Contains, `interface=org.freedesktop.Avahi.RecordBrowser`)
	// control capabilities
	c.Assert(spec.SnippetForTag("snap.producer.app"), Not(testutil.Contains), `interface=org.freedesktop.Avahi.EntryGroup`)

	// permanent app slot
	appSet = mylog.Check2(interfaces.NewSnapAppSet(slotInfo.Snap, nil))

	spec = apparmor.NewSpecification(appSet)
	c.Assert(spec.AddPermanentSlot(s.iface, slotInfo), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.producer.app"})
	c.Assert(spec.SnippetForTag("snap.producer.app"), testutil.Contains, `dbus (bind)
    bus=system
    name="org.freedesktop.Avahi",`)
}

func (s *AvahiObserveInterfaceSuite) testAppArmorSpecFromSystem(c *C,
	slot *interfaces.ConnectedSlot, slotInfo *snap.SlotInfo,
) {
	// connected plug to core slot
	appSet := mylog.Check2(interfaces.NewSnapAppSet(s.plug.Snap(), nil))

	spec := apparmor.NewSpecification(appSet)
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "name=org.freedesktop.Avahi")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "peer=(label=\"{unconfined,/usr/sbin/avahi-daemon,avahi-daemon}\"),")
	// make sure observe does have observe but not control capabilities
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `interface=org.freedesktop.Avahi.AddressResolver`)
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `interface=org.freedesktop.Avahi.HostNameResolver`)
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `interface=org.freedesktop.Avahi.ServiceResolver`)
	// control capabilities
	c.Assert(spec.SnippetForTag("snap.consumer.app"), Not(testutil.Contains), `member=Set*`)
	c.Assert(spec.SnippetForTag("snap.consumer.app"), Not(testutil.Contains), `member=EntryGroupNew`)
	c.Assert(spec.SnippetForTag("snap.consumer.app"), Not(testutil.Contains), `interface=org.freedesktop.Avahi.EntryGroup`)

	// connected core slot to plug
	appSet = mylog.Check2(interfaces.NewSnapAppSet(slot.Snap(), nil))

	spec = apparmor.NewSpecification(appSet)
	c.Assert(spec.AddConnectedSlot(s.iface, s.plug, slot), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)

	// permanent core slot
	appSet = mylog.Check2(interfaces.NewSnapAppSet(slotInfo.Snap, nil))

	spec = apparmor.NewSpecification(appSet)
	c.Assert(spec.AddPermanentSlot(s.iface, slotInfo), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)
}

func (s *AvahiObserveInterfaceSuite) TestAppArmorSpec(c *C) {
	// on a core system with avahi slot coming from a regular app snap.
	restore := release.MockOnClassic(false)
	defer restore()
	s.testAppArmorSpecWithProducer(c, s.appSlot, s.appSlotInfo)

	// on a classic system with avahi slot coming from the system by core snap.
	restore = release.MockOnClassic(true)
	defer restore()
	s.testAppArmorSpecWithProducer(c, s.appSlot, s.appSlotInfo)

	// on a classic system with avahi slot coming from the system by core snap.
	restore = release.MockOnClassic(true)
	defer restore()
	s.testAppArmorSpecFromSystem(c, s.coreSlot, s.coreSlotInfo)

	// on a classic system with avahi slot coming from the system by snapd snap.
	restore = release.MockOnClassic(true)
	defer restore()
	s.testAppArmorSpecFromSystem(c, s.snapdSlot, s.snapdSlotInfo)
}

func (s *AvahiObserveInterfaceSuite) testDBusSpecSlotByApp(c *C, classic bool) {
	restore := release.MockOnClassic(classic)
	defer restore()

	appSet := mylog.Check2(interfaces.NewSnapAppSet(s.appSlotInfo.Snap, nil))

	spec := dbus.NewSpecification(appSet)
	c.Assert(spec.AddPermanentSlot(s.iface, s.appSlotInfo), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.producer.app"})
	c.Assert(spec.SnippetForTag("snap.producer.app"), testutil.Contains, `<allow own="org.freedesktop.Avahi"/>`)
}

func (s *AvahiObserveInterfaceSuite) testDBusSpecSlotBySystem(c *C, slotInfo *snap.SlotInfo) {
	restore := release.MockOnClassic(true)
	defer restore()

	appSet := mylog.Check2(interfaces.NewSnapAppSet(slotInfo.Snap, nil))

	spec := dbus.NewSpecification(appSet)
	c.Assert(spec.AddPermanentSlot(s.iface, slotInfo), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)
}

func (s *AvahiObserveInterfaceSuite) TestDBusSpecSlot(c *C) {
	// on a core system with avahi slot coming from a regular app snap.
	s.testDBusSpecSlotByApp(c, false)
	// on a classic system with avahi slot coming from a regular app snap.
	s.testDBusSpecSlotByApp(c, true)

	// on a classic system with avahi slot coming from the core snap.
	s.testDBusSpecSlotBySystem(c, s.coreSlotInfo)
	// on a classic system with avahi slot coming from the snapd snap.
	s.testDBusSpecSlotBySystem(c, s.snapdSlotInfo)
}

func (s *AvahiObserveInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, false)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows discovery on a local network via the mDNS/DNS-SD protocol suite`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "avahi-observe")
}

func (s *AvahiObserveInterfaceSuite) TestAutoConnect(c *C) {
	c.Assert(s.iface.AutoConnect(s.plugInfo, s.coreSlotInfo), Equals, true)
	c.Assert(s.iface.AutoConnect(s.plugInfo, s.appSlotInfo), Equals, true)
}

func (s *AvahiObserveInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
