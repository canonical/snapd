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
	iface    interfaces.Interface
	plug     *interfaces.Plug
	appSlot  *interfaces.Slot
	coreSlot *interfaces.Slot
}

var _ = Suite(&AvahiControlInterfaceSuite{
	iface: builtin.MustInterface("avahi-control"),
})

const avahiControlConsumerYaml = `name: consumer
apps:
 app:
  plugs: [avahi-control]
`

const avahiControlProducerYaml = `name: producer
apps:
 app:
  slots: [avahi-control]
`

const avahiControlCoreYaml = `name: core
slots:
  avahi-control:
`

func (s *AvahiControlInterfaceSuite) SetUpTest(c *C) {
	s.plug = MockPlug(c, avahiControlConsumerYaml, nil, "avahi-control")
	s.appSlot = MockSlot(c, avahiControlProducerYaml, nil, "avahi-control")
	s.coreSlot = MockSlot(c, avahiControlCoreYaml, nil, "avahi-control")
}

func (s *AvahiControlInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "avahi-control")
}

func (s *AvahiControlInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(s.coreSlot.Sanitize(s.iface), IsNil)
	c.Assert(s.appSlot.Sanitize(s.iface), IsNil)
	// avahi-control slot can now be used on snap other than core.
	slot := &interfaces.Slot{SlotInfo: &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "some-snap"},
		Name:      "avahi-control",
		Interface: "avahi-control",
	}}
	c.Assert(slot.Sanitize(s.iface), IsNil)
}

func (s *AvahiControlInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(s.plug.Sanitize(s.iface), IsNil)
}

func (s *AvahiControlInterfaceSuite) TestAppArmorSpec(c *C) {
	// on a core system with avahi slot coming from a regular app snap.
	restore := release.MockOnClassic(false)
	defer restore()

	// connected plug to app slot
	spec := &apparmor.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, nil, s.appSlot, nil), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "name=org.freedesktop.Avahi")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `peer=(label="snap.producer.app"),`)

	// connected app slot to plug
	spec = &apparmor.Specification{}
	c.Assert(spec.AddConnectedSlot(s.iface, s.plug, nil, s.appSlot, nil), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.producer.app"})
	c.Assert(spec.SnippetForTag("snap.producer.app"), testutil.Contains, `interface=org.freedesktop.Avahi`)
	c.Assert(spec.SnippetForTag("snap.producer.app"), testutil.Contains, `peer=(label="snap.consumer.app"),`)

	// on a classic system with avahi slot coming from the core snap.
	restore = release.MockOnClassic(true)
	defer restore()

	// connected plug to core slot
	spec = &apparmor.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, nil, s.coreSlot, nil), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "name=org.freedesktop.Avahi")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "peer=(label=unconfined),")

	// connected app slot to plug
	spec = &apparmor.Specification{}
	c.Assert(spec.AddConnectedSlot(s.iface, s.plug, nil, s.coreSlot, nil), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)
}

func (s *AvahiControlInterfaceSuite) TestDBusSpec(c *C) {
	// on a core system with avahi slot coming from a regular app snap.
	restore := release.MockOnClassic(false)
	defer restore()

	spec := &dbus.Specification{}
	c.Assert(spec.AddPermanentSlot(s.iface, s.appSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.producer.app"})
	c.Assert(spec.SnippetForTag("snap.producer.app"), testutil.Contains, `<allow own="org.freedesktop.Avahi"/>`)

	// on a classic system with avahi slot coming from the core snap.
	restore = release.MockOnClassic(true)
	defer restore()

	spec = &dbus.Specification{}
	c.Assert(spec.AddPermanentSlot(s.iface, s.coreSlot), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)
}

func (s *AvahiControlInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, false)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows control over service discovery on a local network via the mDNS/DNS-SD protocol suite`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "avahi-control")
}

func (s *AvahiControlInterfaceSuite) TestAutoConnect(c *C) {
	c.Assert(s.iface.AutoConnect(s.plug, s.coreSlot), Equals, true)
	c.Assert(s.iface.AutoConnect(s.plug, s.appSlot), Equals, true)
}

func (s *AvahiControlInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
