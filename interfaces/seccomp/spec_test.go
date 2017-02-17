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
		SecCompConnectedPlugCallback: func(spec *seccomp.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error {
			return spec.AddSnippet([]byte("connected-plug"))
		},
		SecCompConnectedSlotCallback: func(spec *seccomp.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error {
			return spec.AddSnippet([]byte("connected-slot"))
		},
		SecCompPermanentPlugCallback: func(spec *seccomp.Specification, plug *interfaces.Plug) error {
			return spec.AddSnippet([]byte("permanent-plug"))
		},
		SecCompPermanentSlotCallback: func(spec *seccomp.Specification, slot *interfaces.Slot) error {
			return spec.AddSnippet([]byte("permanent-slot"))
		},
	},
	plug: &interfaces.Plug{
		PlugInfo: &snap.PlugInfo{
			Snap:      &snap.Info{SuggestedName: "snap1"},
			Name:      "name",
			Interface: "test",
			Apps: map[string]*snap.AppInfo{
				"app1": &snap.AppInfo{
					Snap: &snap.Info{
						SuggestedName: "snap1",
					},
					Name: "app1"}},
		},
	},
	slot: &interfaces.Slot{
		SlotInfo: &snap.SlotInfo{
			Snap:      &snap.Info{SuggestedName: "snap2"},
			Name:      "name",
			Interface: "test",
			Apps: map[string]*snap.AppInfo{
				"app2": &snap.AppInfo{
					Snap: &snap.Info{
						SuggestedName: "snap2",
					},
					Name: "app2"}},
		},
	},
})

func (s *specSuite) SetUpTest(c *C) {
	s.spec = &seccomp.Specification{}
}

// The spec.Specification can be used through the interfaces.Specification interface
func (s *specSuite) TestSpecificationIface(c *C) {
	var r interfaces.Specification = s.spec
	c.Assert(r.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(r.AddConnectedSlot(s.iface, s.plug, s.slot), IsNil)
	c.Assert(r.AddPermanentPlug(s.iface, s.plug), IsNil)
	c.Assert(r.AddPermanentSlot(s.iface, s.slot), IsNil)
	c.Assert(s.spec.Snippets, DeepEquals, map[string][][]byte{
		"snap.snap1.app1": [][]byte{[]byte("connected-plug"), []byte("permanent-plug")},
		"snap.snap2.app2": [][]byte{[]byte("connected-slot"), []byte("permanent-slot")},
	})
}
