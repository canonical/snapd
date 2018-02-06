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

package apparmor_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
)

type specSuite struct {
	iface    *ifacetest.TestInterface
	spec     *apparmor.Specification
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
}

var _ = Suite(&specSuite{
	iface: &ifacetest.TestInterface{
		InterfaceName: "test",
		AppArmorConnectedPlugCallback: func(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
			spec.AddSnippet("connected-plug")
			return nil
		},
		AppArmorConnectedSlotCallback: func(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
			spec.AddSnippet("connected-slot")
			return nil
		},
		AppArmorPermanentPlugCallback: func(spec *apparmor.Specification, plug *snap.PlugInfo) error {
			spec.AddSnippet("permanent-plug")
			return nil
		},
		AppArmorPermanentSlotCallback: func(spec *apparmor.Specification, slot *snap.SlotInfo) error {
			spec.AddSnippet("permanent-slot")
			return nil
		},
	},
	plugInfo: &snap.PlugInfo{
		Snap:      &snap.Info{SuggestedName: "snap1"},
		Name:      "name",
		Interface: "test",
		Apps: map[string]*snap.AppInfo{
			"app1": {
				Snap: &snap.Info{
					SuggestedName: "snap1",
				},
				Name: "app1"}},
	},
	slotInfo: &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "snap2"},
		Name:      "name",
		Interface: "test",
		Apps: map[string]*snap.AppInfo{
			"app2": {
				Snap: &snap.Info{
					SuggestedName: "snap2",
				},
				Name: "app2"}},
	},
})

func (s *specSuite) SetUpTest(c *C) {
	s.spec = &apparmor.Specification{}
	s.plug = interfaces.NewConnectedPlug(s.plugInfo, nil)
	s.slot = interfaces.NewConnectedSlot(s.slotInfo, nil)
}

// The spec.Specification can be used through the interfaces.Specification interface
func (s *specSuite) TestSpecificationIface(c *C) {
	var r interfaces.Specification = s.spec
	c.Assert(r.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(r.AddConnectedSlot(s.iface, s.plug, s.slot), IsNil)
	c.Assert(r.AddPermanentPlug(s.iface, s.plugInfo), IsNil)
	c.Assert(r.AddPermanentSlot(s.iface, s.slotInfo), IsNil)
	c.Assert(s.spec.Snippets(), DeepEquals, map[string][]string{
		"snap.snap1.app1": {"connected-plug", "permanent-plug"},
		"snap.snap2.app2": {"connected-slot", "permanent-slot"},
	})
}

const snapWithLayout = `
name: vanguard
apps:
  vanguard:
    command: vanguard
layout:
  /usr:
    bind: $SNAP/usr
  /mytmp:
    type: tmpfs
    mode: 1777
  /mylink:
    symlink: $SNAP/link/target
`

func (s *specSuite) TestApparmorSnippetsFromLayout(c *C) {
	snapInfo := snaptest.MockInfo(c, snapWithLayout, &snap.SideInfo{Revision: snap.R(42)})
	s.spec.AddSnapLayout(snapInfo)
	c.Assert(s.spec.Snippets(), DeepEquals, map[string][]string{
		"snap.vanguard.vanguard": {
			"# Layout path: /mylink\n/mylink{,/**} mrwklix,",
			"# Layout path: /mytmp\n/mytmp{,/**} mrwklix,",
			"# Layout path: /usr\n/usr{,/**} mrwklix,",
		},
	})
}
