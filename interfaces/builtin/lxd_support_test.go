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

type LxdSupportInterfaceSuite struct {
	iface interfaces.Interface
	slot  *interfaces.Slot
	plug  *interfaces.Plug
}

var _ = Suite(&LxdSupportInterfaceSuite{
	iface: builtin.MustInterface("lxd-support"),
})

const lxdSupportConsumerYaml = `name: consumer
apps:
 app:
  plugs: [lxd-support]
`

const lxdSupportCoreYaml = `name: core
type: os
slots:
  lxd-support:
`

func (s *LxdSupportInterfaceSuite) SetUpTest(c *C) {
	s.plug = MockPlug(c, lxdSupportConsumerYaml, nil, "lxd-support")
	s.slot = MockSlot(c, lxdSupportCoreYaml, nil, "lxd-support")
}

func (s *LxdSupportInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "lxd-support")
}

func (s *LxdSupportInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(s.slot.Sanitize(s.iface), IsNil)
	slot := &interfaces.Slot{SlotInfo: &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "some-snap"},
		Name:      "lxd-support",
		Interface: "lxd-support",
	}}

	c.Assert(slot.Sanitize(s.iface), ErrorMatches,
		"lxd-support slots are reserved for the core snap")
}

func (s *LxdSupportInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(s.plug.Sanitize(s.iface), IsNil)
}

func (s *LxdSupportInterfaceSuite) TestAppArmorSpec(c *C) {
	spec := &apparmor.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, nil, s.slot, nil), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "/usr/sbin/aa-exec ux,\n")
}

func (s *LxdSupportInterfaceSuite) TestSecCompSpec(c *C) {
	spec := &seccomp.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, nil, s.slot, nil), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "@unrestricted\n")
}

func (s *LxdSupportInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, true)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows operating as the LXD service`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "lxd-support")
	c.Assert(si.BaseDeclarationPlugs, testutil.Contains, "lxd-support")
}

func (s *LxdSupportInterfaceSuite) TestAutoConnect(c *C) {
	c.Check(s.iface.AutoConnect(nil, nil), Equals, true)
}

func (s *LxdSupportInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
