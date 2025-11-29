// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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
	"github.com/snapcore/snapd/testutil"
)

type DevLxdInterfaceSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

var _ = Suite(&DevLxdInterfaceSuite{
	iface: builtin.MustInterface("devlxd"),
})

const devlxdConsumerYaml = `name: consumer
version: 0
apps:
 app:
  plugs: [devlxd]
`

const devlxdCoreYaml = `name: core
version: 0
type: os
slots:
  devlxd:
`

func (s *DevLxdInterfaceSuite) SetUpTest(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, devlxdConsumerYaml, nil, "devlxd")
	s.slot, s.slotInfo = MockConnectedSlot(c, devlxdCoreYaml, nil, "devlxd")
}

func (s *DevLxdInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "devlxd")
}

func (s *DevLxdInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
	slot := &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "some-snap"},
		Name:      "devlxd",
		Interface: "devlxd",
	}

	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), IsNil)
}

func (s *DevLxdInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *DevLxdInterfaceSuite) TestAppArmorSpec(c *C) {
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	spec := apparmor.NewSpecification(appSet)
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "/dev/lxd/sock rw,\n")
}

func (s *DevLxdInterfaceSuite) TestSecCompSpec(c *C) {
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	spec := seccomp.NewSpecification(appSet)
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Check(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "socket AF_NETLINK - NETLINK_GENERIC\n")
}

func (s *DevLxdInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, true)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows access to the LXD devlxd socket`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "devlxd")
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "allow-installation")
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "slot-snap-type")
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "deny-auto-connection: true")
}

func (s *DevLxdInterfaceSuite) TestAutoConnect(c *C) {
	c.Check(s.iface.AutoConnect(s.plugInfo, s.slotInfo), Equals, true)
}

func (s *DevLxdInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
