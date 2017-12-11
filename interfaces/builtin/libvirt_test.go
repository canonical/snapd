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

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type LibvirtInterfaceSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	plugInfo *snap.PlugInfo
}

var _ = Suite(&LibvirtInterfaceSuite{
	iface: builtin.MustInterface("libvirt"),
	slotInfo: &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "libvirt"},
		Name:      "libvirt",
		Interface: "libvirt",
	},
	plugInfo: &snap.PlugInfo{
		Snap:      &snap.Info{SuggestedName: "other"},
		Name:      "libvirt",
		Interface: "libvirt",
	},
})

func (s *LibvirtInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "libvirt")
}

func (s *LibvirtInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), ErrorMatches, ".*libvirt slots are reserved for the core snap.*")
}

func (s *LibvirtInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *LibvirtInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
