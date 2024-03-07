// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

type ContactsServiceInterfaceSuite struct {
	iface    interfaces.Interface
	slot     *interfaces.ConnectedSlot
	slotInfo *snap.SlotInfo
	plug     *interfaces.ConnectedPlug
	plugInfo *snap.PlugInfo
}

var _ = Suite(&ContactsServiceInterfaceSuite{
	iface: builtin.MustInterface("contacts-service"),
})

func (s *ContactsServiceInterfaceSuite) SetUpTest(c *C) {
	const coreYaml = `name: core
version: 0
type: os
slots:
 contacts-service:
  interface: contacts-service
`
	s.slot, s.slotInfo = MockConnectedSlot(c, coreYaml, nil, "contacts-service")

	var consumerYaml = `name: consumer
version: 0
apps:
 app:
  plugs: [contacts-service]
`
	s.plug, s.plugInfo = MockConnectedPlug(c, consumerYaml, nil, "contacts-service")
}

func (s *ContactsServiceInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "contacts-service")
}

func (s *ContactsServiceInterfaceSuite) TestSanitize(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *ContactsServiceInterfaceSuite) TestAppArmorConnectedPlug(c *C) {
	spec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap(), nil))
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 1)
	c.Check(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `interface=org.gnome.evolution.dataserver.Source`)
	c.Check(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `interface=org.gnome.evolution.dataserver.AddressBook`)
	c.Check(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `interface=org.gnome.evolution.dataserver.AddressBookFactory`)
	c.Check(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `interface=org.gnome.evolution.dataserver.AddressBookView`)
}

func (s *ContactsServiceInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Check(si.ImplicitOnCore, Equals, false)
	c.Check(si.ImplicitOnClassic, Equals, !(release.ReleaseInfo.ID == "ubuntu" && release.ReleaseInfo.VersionID == "14.04"))
	c.Check(si.Summary, Equals, "allows communication with Evolution Data Service Address Book")
	c.Check(si.BaseDeclarationSlots, testutil.Contains, "contacts-service")
}

func (s *ContactsServiceInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
