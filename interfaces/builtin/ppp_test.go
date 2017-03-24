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
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/dbus"
	"github.com/snapcore/snapd/interfaces/kmod"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type PppInterfaceSuite struct {
	iface interfaces.Interface
	slot  *interfaces.Slot
	plug  *interfaces.Plug
}

var _ = Suite(&PppInterfaceSuite{})

func (s *PppInterfaceSuite) SetUpTest(c *C) {
	const mockPlugSnapInfo = `name: other
version: 1.0
apps:
 app:
  command: foo
  plugs: [ppp]
`

	const mockSlotSnapInfo = `name: ppp
version: 1.0
apps:
 app:
  command: foo
  slots: [ppp]
`

	s.iface = &builtin.PppInterface{}
	slotSnap := snaptest.MockInfo(c, mockPlugSnapInfo, nil)
	s.slot = &interfaces.Slot{SlotInfo: slotSnap.Slots["ppp"]}

	plugSnap := snaptest.MockInfo(c, mockPlugSnapInfo, nil)
	s.plug = &interfaces.Plug{PlugInfo: plugSnap.Plugs["ppp"]}
}

func (s *PppInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "ppp")
}

func (s *PppInterfaceSuite) TestUsedSecuritySystems(c *C) {
	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app"), testutil.Contains, `/usr/sbin/pppd ix,`)

	apparmorSpec = &apparmor.Specification{}
	err = apparmorSpec.AddPermanentSlot(s.iface, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), HasLen, 0)

	dbusSpec := &dbus.Specification{}
	err = dbusSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(dbusSpec.SecurityTags(), HasLen, 0)
	dbusSpec = &dbus.Specification{}
	err = dbusSpec.AddPermanentSlot(s.iface, s.slot)
	c.Assert(err, IsNil)
	c.Assert(dbusSpec.SecurityTags(), HasLen, 0)

	snippet, err := s.iface.PermanentSlotSnippet(s.slot, interfaces.SecurityUDev)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)

	spec := &kmod.Specification{}
	err = spec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(spec.Modules(), DeepEquals, map[string]bool{
		"ppp_generic": true,
	})
}
