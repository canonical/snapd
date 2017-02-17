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

package seccomp_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/snap"
)

type specSuite struct {
	iface *ifacetest.TestInterface
	spec  *seccomp.Specification
	plug  *interfaces.Plug
	slot  *interfaces.Slot
}

var _ = Suite(&specSuite{
	iface: &ifacetest.TestInterface{
		InterfaceName: "test",
		SeccompConnectedPlugCallback: func(spec *seccomp.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error {
			return spec.AddSnippet(snap.AppSecurityTag("snap1", "app1"), "connected-plug")
		},
		SeccompConnectedSlotCallback: func(spec *seccomp.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error {
			return spec.AddSnippet(snap.AppSecurityTag("snap2", "app2"), "connected-slot")
		},
		SeccompPermanentPlugCallback: func(spec *seccomp.Specification, plug *interfaces.Plug) error {
			return spec.AddSnippet(snap.AppSecurityTag("snap1", "app1"), "permanent-plug")
		},
		SeccompPermanentSlotCallback: func(spec *seccomp.Specification, slot *interfaces.Slot) error {
			return spec.AddSnippet(snap.AppSecurityTag("snap2", "app2"), "permanent-slot")
		},
	},
	plug: &interfaces.Plug{
		PlugInfo: &snap.PlugInfo{
			Snap:      &snap.Info{SuggestedName: "snap1"},
			Name:      "name",
			Interface: "test",
		},
	},
	slot: &interfaces.Slot{
		SlotInfo: &snap.SlotInfo{
			Snap:      &snap.Info{SuggestedName: "snap2"},
			Name:      "name",
			Interface: "test",
		},
	},
})

func (s *specSuite) SetUpTest(c *C) {
	s.spec = &seccomp.Specification{}
}

// AddSnippet is not broken
func (s *specSuite) TestSmoke(c *C) {
	c.Assert(s.spec.AddSnippet(snap.AppSecurityTag("snap1", "app1"), "foo"), IsNil)
	c.Assert(s.spec.AddSnippet(snap.AppSecurityTag("snap2", "app2"), "bar"), IsNil)
	c.Assert(s.spec.Snippets, DeepEquals, map[string][]string{
		"snap.snap1.app1": []string{"foo"},
		"snap.snap2.app2": []string{"bar"}})
}

// The spec.Specification can be used through the interfaces.Specification interface
func (s *specSuite) TestSpecificationIface(c *C) {
	var r interfaces.Specification = s.spec
	c.Assert(r.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(r.AddConnectedSlot(s.iface, s.plug, s.slot), IsNil)
	c.Assert(r.AddPermanentPlug(s.iface, s.plug), IsNil)
	c.Assert(r.AddPermanentSlot(s.iface, s.slot), IsNil)
	c.Assert(s.spec.Snippets, DeepEquals, map[string][]string{
		"snap.snap1.app1": []string{"connected-plug", "permanent-plug"},
		"snap.snap2.app2": []string{"connected-slot", "permanent-slot"},
	})
}

func (s *specSuite) TestRemove(c *C) {
	var r interfaces.Specification = s.spec
	c.Assert(r.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(r.AddConnectedSlot(s.iface, s.plug, s.slot), IsNil)
	c.Assert(r.AddPermanentPlug(s.iface, s.plug), IsNil)
	c.Assert(r.AddPermanentSlot(s.iface, s.slot), IsNil)

	c.Check(len(s.spec.Snippets), Equals, 2)
	s.spec.Remove("snap2")
	c.Assert(s.spec.Snippets, DeepEquals, map[string][]string{
		"snap.snap1.app1": []string{"connected-plug", "permanent-plug"},
	})
	s.spec.Remove("snap1")
	c.Assert(len(s.spec.Snippets), Equals, 0)
}
