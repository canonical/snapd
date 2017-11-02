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
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type GnomeOnlineAccountsServiceInterfaceSuite struct {
	iface interfaces.Interface
	slot  *interfaces.Slot
	plug  *interfaces.Plug
}

var _ = Suite(&GnomeOnlineAccountsServiceInterfaceSuite{
	iface: builtin.MustInterface("gnome-online-accounts-service"),
})

func (s *GnomeOnlineAccountsServiceInterfaceSuite) SetUpTest(c *C) {
	const coreYaml = `name: core
type: os
slots:
 gnome-online-accounts-service:
  interface: gnome-online-accounts-service
`
	coreInfo := snaptest.MockInfo(c, coreYaml, nil)
	s.slot = &interfaces.Slot{SlotInfo: coreInfo.Slots["gnome-online-accounts-service"]}

	var consumerYaml = `name: consumer
apps:
 app:
  plugs: [gnome-online-accounts-service]
`
	consumerInfo := snaptest.MockInfo(c, consumerYaml, nil)
	s.plug = &interfaces.Plug{PlugInfo: consumerInfo.Plugs["gnome-online-accounts-service"]}
}

func (s *GnomeOnlineAccountsServiceInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "gnome-online-accounts-service")
}

func (s *GnomeOnlineAccountsServiceInterfaceSuite) TestSanitize(c *C) {
	c.Assert(s.plug.Sanitize(s.iface), IsNil)
	c.Assert(s.slot.Sanitize(s.iface), IsNil)
}

func (s *GnomeOnlineAccountsServiceInterfaceSuite) TestAppArmorConnectedPlug(c *C) {
	spec := &apparmor.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, nil, s.slot, nil), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 1)
	c.Check(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `interface=org.gnome.OnlineAccounts.*`)
}

func (s *GnomeOnlineAccountsServiceInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
