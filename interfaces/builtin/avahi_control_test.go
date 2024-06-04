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
	"github.com/snapcore/snapd/interfaces/dbus"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type AvahiControlInterfaceSuite struct {
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

var _ = Suite(&AvahiControlInterfaceSuite{
	iface: builtin.MustInterface("avahi-control"),
})

const avahiControlConsumerYaml = `name: consumer
version: 0
apps:
 app:
  plugs: [avahi-control]
`

const avahiControlProducerYaml = `name: producer
version: 0
apps:
 app:
  slots: [avahi-control]
`

const avahiControlCoreYaml = `name: core
version: 0
type: os
slots:
  avahi-control:
`

const avahiControlSnapdYaml = `name: snapd
version: 0
type: snapd
slots:
  avahi-control:
`

func (s *AvahiControlInterfaceSuite) SetUpTest(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, avahiControlConsumerYaml, nil, "avahi-control")
	s.appSlot, s.appSlotInfo = MockConnectedSlot(c, avahiControlProducerYaml, nil, "avahi-control")
	s.coreSlot, s.coreSlotInfo = MockConnectedSlot(c, avahiControlCoreYaml, nil, "avahi-control")
	s.snapdSlot, s.snapdSlotInfo = MockConnectedSlot(c, avahiControlSnapdYaml, nil, "avahi-control")
}

func (s *AvahiControlInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "avahi-control")
}

func (s *AvahiControlInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.coreSlotInfo), IsNil)
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.appSlotInfo), IsNil)
	// avahi-control slot can now be used on snap other than core.
	slot := &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "some-snap"},
		Name:      "avahi-control",
		Interface: "avahi-control",
	}
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), IsNil)
}

func (s *AvahiControlInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *AvahiControlInterfaceSuite) testAppArmorSpecWithProducer(c *C,
	slot *interfaces.ConnectedSlot, slotInfo *snap.SlotInfo) {
	// connected plug to app slot
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	spec := apparmor.NewSpecification(appSet)
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "name=org.freedesktop.Avahi")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `peer=(label="snap.producer.app"),`)
	// make sure control includes also observe capabilities
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `interface=org.freedesktop.Avahi.AddressResolver`)
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `interface=org.freedesktop.Avahi.HostNameResolver`)
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `interface=org.freedesktop.Avahi.ServiceResolver`)
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `interface=org.freedesktop.Avahi.RecordBrowser`)
	// control capabilities
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `member=Set*`)
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `member=EntryGroupNew`)
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `interface=org.freedesktop.Avahi.EntryGroup`)

	// connected app slot to plug
	appSet, err = interfaces.NewSnapAppSet(slot.Snap(), nil)
	c.Assert(err, IsNil)
	spec = apparmor.NewSpecification(appSet)
	c.Assert(spec.AddConnectedSlot(s.iface, s.plug, slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.producer.app"})
	c.Assert(spec.SnippetForTag("snap.producer.app"), testutil.Contains, `interface=org.freedesktop.Avahi`)
	c.Assert(spec.SnippetForTag("snap.producer.app"), testutil.Contains, `peer=(label="snap.consumer.app"),`)
	// make sure control includes also observe capabilities
	c.Assert(spec.SnippetForTag("snap.producer.app"), testutil.Contains, `interface=org.freedesktop.Avahi.AddressResolver`)
	c.Assert(spec.SnippetForTag("snap.producer.app"), testutil.Contains, `interface=org.freedesktop.Avahi.HostNameResolver`)
	c.Assert(spec.SnippetForTag("snap.producer.app"), testutil.Contains, `interface=org.freedesktop.Avahi.ServiceResolver`)
	c.Assert(spec.SnippetForTag("snap.producer.app"), testutil.Contains, `interface=org.freedesktop.Avahi.RecordBrowser`)
	// control capabilities
	c.Assert(spec.SnippetForTag("snap.producer.app"), testutil.Contains, `interface=org.freedesktop.Avahi.EntryGroup`)

	// permanent app slot
	appSet, err = interfaces.NewSnapAppSet(slotInfo.Snap, nil)
	c.Assert(err, IsNil)
	spec = apparmor.NewSpecification(appSet)
	c.Assert(spec.AddPermanentSlot(s.iface, slotInfo), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.producer.app"})
	c.Assert(spec.SnippetForTag("snap.producer.app"), testutil.Contains, `dbus (bind)
    bus=system
    name="org.freedesktop.Avahi",`)
}

func (s *AvahiControlInterfaceSuite) testAppArmorSpecFromSystem(c *C,
	slot *interfaces.ConnectedSlot, slotInfo *snap.SlotInfo) {
	// connected plug to core slot
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	spec := apparmor.NewSpecification(appSet)
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "name=org.freedesktop.Avahi")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "peer=(label=\"{unconfined,/usr/sbin/avahi-daemon,avahi-daemon}\"),")
	// make sure control includes also observe capabilities
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `interface=org.freedesktop.Avahi.AddressResolver`)
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `interface=org.freedesktop.Avahi.HostNameResolver`)
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `interface=org.freedesktop.Avahi.ServiceResolver`)
	// control capabilities
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `member=Set*`)
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `member=EntryGroupNew`)
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `interface=org.freedesktop.Avahi.EntryGroup`)

	// connected core slot to plug
	appSet, err = interfaces.NewSnapAppSet(slot.Snap(), nil)
	c.Assert(err, IsNil)
	spec = apparmor.NewSpecification(appSet)
	c.Assert(spec.AddConnectedSlot(s.iface, s.plug, slot), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)

	// permanent core slot
	appSet, err = interfaces.NewSnapAppSet(slotInfo.Snap, nil)
	c.Assert(err, IsNil)
	spec = apparmor.NewSpecification(appSet)
	c.Assert(spec.AddPermanentSlot(s.iface, slotInfo), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)
}

func (s *AvahiControlInterfaceSuite) TestAppArmorSpec(c *C) {
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

func (s *AvahiControlInterfaceSuite) testDBusSpecSlotByApp(c *C, classic bool) {
	restore := release.MockOnClassic(classic)
	defer restore()

	appSet, err := interfaces.NewSnapAppSet(s.appSlotInfo.Snap, nil)
	c.Assert(err, IsNil)
	spec := dbus.NewSpecification(appSet)
	c.Assert(spec.AddPermanentSlot(s.iface, s.appSlotInfo), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.producer.app"})
	c.Assert(spec.SnippetForTag("snap.producer.app"), testutil.Contains, `<allow own="org.freedesktop.Avahi"/>`)
}

func (s *AvahiControlInterfaceSuite) testDBusSpecSlotBySystem(c *C, slotInfo *snap.SlotInfo) {
	restore := release.MockOnClassic(true)
	defer restore()

	appSet, err := interfaces.NewSnapAppSet(slotInfo.Snap, nil)
	c.Assert(err, IsNil)
	spec := dbus.NewSpecification(appSet)
	c.Assert(spec.AddPermanentSlot(s.iface, slotInfo), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)
}

func (s *AvahiControlInterfaceSuite) TestDBusSpecSlot(c *C) {
	// on a core system with avahi slot coming from a regular app snap.
	s.testDBusSpecSlotByApp(c, false)
	// on a classic system with avahi slot coming from a regular app snap.
	s.testDBusSpecSlotByApp(c, true)

	// on a classic system with avahi slot coming from the core snap.
	s.testDBusSpecSlotBySystem(c, s.coreSlotInfo)
	// on a classic system with avahi slot coming from the snapd snap.
	s.testDBusSpecSlotBySystem(c, s.snapdSlotInfo)
}

func (s *AvahiControlInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, false)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows control over service discovery on a local network via the mDNS/DNS-SD protocol suite`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "avahi-control")
}

func (s *AvahiControlInterfaceSuite) TestAutoConnect(c *C) {
	c.Assert(s.iface.AutoConnect(s.plugInfo, s.coreSlotInfo), Equals, true)
	c.Assert(s.iface.AutoConnect(s.plugInfo, s.appSlotInfo), Equals, true)
}

func (s *AvahiControlInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
