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
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type OnlineAccountsServiceInterfaceSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

var _ = Suite(&OnlineAccountsServiceInterfaceSuite{
	iface: builtin.MustInterface("online-accounts-service"),
})

func (s *OnlineAccountsServiceInterfaceSuite) SetUpTest(c *C) {
	const providerYaml = `name: provider
version: 1.0
slots:
 online-accounts-service:
  interface: online-accounts-service
apps:
 app:
  command: foo
  slots: [online-accounts-service]
`
	providerInfo := snaptest.MockInfo(c, providerYaml, nil)
	s.slotInfo = providerInfo.Slots["online-accounts-service"]
	s.slot = interfaces.NewConnectedSlot(s.slotInfo, nil, nil)

	var consumerYaml = `name: consumer
version: 1.0
apps:
 app:
  command: foo
  plugs: [online-accounts-service]
`
	consumerInfo := snaptest.MockInfo(c, consumerYaml, nil)
	s.plugInfo = consumerInfo.Plugs["online-accounts-service"]
	s.plug = interfaces.NewConnectedPlug(s.plugInfo, nil, nil)
}

func (s *OnlineAccountsServiceInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "online-accounts-service")
}

func (s *OnlineAccountsServiceInterfaceSuite) TestSanitize(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *OnlineAccountsServiceInterfaceSuite) TestAppArmorConnectedPlug(c *C) {
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	spec := apparmor.NewSpecification(appSet)
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 1)
	c.Check(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `peer=(label="snap.provider.app")`)
}

func (s *OnlineAccountsServiceInterfaceSuite) TestAppArmorConnectedSlot(c *C) {
	appSet, err := interfaces.NewSnapAppSet(s.slot.Snap(), nil)
	c.Assert(err, IsNil)
	spec := apparmor.NewSpecification(appSet)
	c.Assert(spec.AddConnectedSlot(s.iface, s.plug, s.slot), IsNil)
	c.Check(spec.SnippetForTag("snap.provider.app"), testutil.Contains, `peer=(label="snap.consumer.app")`)
}

func (s *OnlineAccountsServiceInterfaceSuite) TestAppArrmorPermanentSlot(c *C) {
	appSet, err := interfaces.NewSnapAppSet(s.slotInfo.Snap, nil)
	c.Assert(err, IsNil)
	spec := apparmor.NewSpecification(appSet)
	c.Assert(spec.AddPermanentSlot(s.iface, s.slotInfo), IsNil)
	c.Assert(spec.Snippets(), HasLen, 1)
	c.Assert(spec.SnippetForTag("snap.provider.app"), testutil.Contains, `member={RequestName,ReleaseName,GetConnectionCredentials}`)
	c.Assert(spec.SnippetForTag("snap.provider.app"), testutil.Contains, `name="com.ubuntu.OnlineAccounts.Manager"`)
}

func (s *OnlineAccountsServiceInterfaceSuite) TestSecCompPermanentSlot(c *C) {
	appSet, err := interfaces.NewSnapAppSet(s.slotInfo.Snap, nil)
	c.Assert(err, IsNil)
	spec := seccomp.NewSpecification(appSet)
	c.Assert(spec.AddPermanentSlot(s.iface, s.slotInfo), IsNil)
	c.Check(spec.SnippetForTag("snap.provider.app"), testutil.Contains, "listen\n")
}

func (s *OnlineAccountsServiceInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
