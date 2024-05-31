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
	iface    *ifacetest.TestInterface
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
}

var _ = Suite(&specSuite{
	iface: &ifacetest.TestInterface{
		InterfaceName: "test",
		SecCompConnectedPlugCallback: func(spec *seccomp.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
			spec.AddSnippet("connected-plug")
			return nil
		},
		SecCompConnectedSlotCallback: func(spec *seccomp.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
			spec.AddSnippet("connected-slot")
			return nil
		},
		SecCompPermanentPlugCallback: func(spec *seccomp.Specification, plug *snap.PlugInfo) error {
			spec.AddSnippet("permanent-plug")
			return nil
		},
		SecCompPermanentSlotCallback: func(spec *seccomp.Specification, slot *snap.SlotInfo) error {
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
	s.plug = interfaces.NewConnectedPlug(s.plugInfo, nil, nil)
	s.slot = interfaces.NewConnectedSlot(s.slotInfo, nil, nil)
}

// The spec.Specification can be used through the interfaces.Specification interface
func (s *specSuite) TestSpecificationIface(c *C) {
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	spec := seccomp.NewSpecification(appSet)
	var r interfaces.Specification = spec
	c.Assert(r.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(r.AddPermanentPlug(s.iface, s.plugInfo), IsNil)
	c.Assert(spec.Snippets(), DeepEquals, map[string][]string{
		"snap.snap1.app1": {"connected-plug", "permanent-plug"},
	})
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.snap1.app1"})
	c.Assert(spec.SnippetForTag("snap.snap1.app1"), Equals, "connected-plug\npermanent-plug\n")

	appSet, err = interfaces.NewSnapAppSet(s.slot.Snap(), nil)
	c.Assert(err, IsNil)
	spec = seccomp.NewSpecification(appSet)
	r = spec
	c.Assert(r.AddConnectedSlot(s.iface, s.plug, s.slot), IsNil)
	c.Assert(r.AddPermanentSlot(s.iface, s.slotInfo), IsNil)
	c.Assert(spec.Snippets(), DeepEquals, map[string][]string{
		"snap.snap2.app2": {"connected-slot", "permanent-slot"},
	})

	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.snap2.app2"})
	c.Assert(spec.SnippetForTag("snap.snap2.app2"), Equals, "connected-slot\npermanent-slot\n")

	c.Assert(spec.SnippetForTag("non-existing"), Equals, "")
}
