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
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type AccountsServiceInterfaceSuite struct {
	iface    interfaces.Interface
	slot     *interfaces.ConnectedSlot
	slotInfo *snap.SlotInfo
	plug     *interfaces.ConnectedPlug
	plugInfo *snap.PlugInfo
}

var _ = Suite(&AccountsServiceInterfaceSuite{
	iface: builtin.MustInterface("accounts-service"),
})

func (s *AccountsServiceInterfaceSuite) SetUpTest(c *C) {
	const coreYaml = `name: core
version: 1.0
type: os
slots:
 accounts-service:
  interface: accounts-service
`
	s.slot, s.slotInfo = MockConnectedSlot(c, coreYaml, nil, "accounts-service")

	var consumerYaml = `name: consumer
version: 1.0
apps:
 app:
  plugs: [accounts-service]
`
	s.plug, s.plugInfo = MockConnectedPlug(c, consumerYaml, nil, "accounts-service")
}

func (s *AccountsServiceInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "accounts-service")
}

func (s *AccountsServiceInterfaceSuite) TestSanitize(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *AccountsServiceInterfaceSuite) TestAppArmorConnectedPlug(c *C) {
	spec := &apparmor.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 1)
	c.Check(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `interface=org.gnome.OnlineAccounts.*`)
}

func (s *AccountsServiceInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
