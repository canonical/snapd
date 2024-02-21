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
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type LxdInterfaceSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

var _ = Suite(&LxdInterfaceSuite{
	iface: builtin.MustInterface("lxd"),
})

const lxdConsumerYaml = `name: consumer
version: 0
apps:
 app:
  plugs: [lxd]
`

const lxdCoreYaml = `name: core
version: 0
type: os
slots:
  lxd:
`

func (s *LxdInterfaceSuite) SetUpTest(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, lxdConsumerYaml, nil, "lxd")
	s.slot, s.slotInfo = MockConnectedSlot(c, lxdCoreYaml, nil, "lxd")
}

func (s *LxdInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "lxd")
}

func (s *LxdInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
	slot := &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "some-snap"},
		Name:      "lxd",
		Interface: "lxd",
	}

	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), IsNil)
}

func (s *LxdInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *LxdInterfaceSuite) TestAppArmorSpec(c *C) {
	spec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap()))
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "/var/snap/lxd/common/lxd/unix.socket rw,\n")
}

func (s *LxdInterfaceSuite) TestSecCompSpec(c *C) {
	spec := seccomp.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap()))
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Check(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "socket AF_NETLINK - NETLINK_GENERIC\n")
}

func (s *LxdInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, false)
	c.Assert(si.ImplicitOnClassic, Equals, false)
	c.Assert(si.Summary, Equals, `allows access to the LXD socket`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "lxd")
}

func (s *LxdInterfaceSuite) TestAutoConnect(c *C) {
	c.Check(s.iface.AutoConnect(nil, nil), Equals, true)
}

func (s *LxdInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
