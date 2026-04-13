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

package dbus_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/dbus"
	"github.com/snapcore/snapd/interfaces/ifacetest"
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
		DBusConnectedPlugCallback: func(spec *dbus.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
			spec.AddSnippet("connected-plug")
			return nil
		},
		DBusConnectedSlotCallback: func(spec *dbus.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
			spec.AddSnippet("connected-slot")
			return nil
		},
		DBusPermanentPlugCallback: func(spec *dbus.Specification, plug *snap.PlugInfo) error {
			spec.AddSnippet("permanent-plug")
			return nil
		},
		DBusPermanentSlotCallback: func(spec *dbus.Specification, slot *snap.SlotInfo) error {
			spec.AddSnippet("permanent-slot")
			return nil
		},
	},
})

func (s *specSuite) SetUpTest(c *C) {
	const plugYaml = `name: snap1
version: 1
apps:
 app1:
  plugs: [name]
`
	s.plug, s.plugInfo = ifacetest.MockConnectedPlug(c, plugYaml, nil, "name")

	const slotYaml = `name: snap2
version: 1
slots:
 name:
  interface: test
apps:
 app2:
`
	s.slot, s.slotInfo = ifacetest.MockConnectedSlot(c, slotYaml, nil, "name")
}

// The spec.Specification can be used through the interfaces.Specification interface
func (s *specSuite) TestSpecificationIface(c *C) {
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	spec := dbus.NewSpecification(appSet)
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
	spec = dbus.NewSpecification(appSet)
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

// TestSnippetPriorityOrdering checks that snippets are ordered by priority
// (ascending) regardless of insertion order.
func (s *specSuite) TestSnippetPriorityOrdering(c *C) {
	iface := &ifacetest.TestInterface{
		InterfaceName: "test",
		DBusConnectedPlugCallback: func(spec *dbus.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
			// Insert in reverse priority order to exercise sorting.
			spec.AddSnippetWithPriority("high-priority", 10)
			spec.AddSnippetWithPriority("low-priority", 1)
			spec.AddSnippetWithPriority("mid-priority", 5)
			return nil
		},
	}

	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	spec := dbus.NewSpecification(appSet)
	c.Assert(spec.AddConnectedPlug(iface, s.plug, s.slot), IsNil)

	// Snippets should be sorted by priority ascending: 1, 5, 10.
	c.Assert(spec.SnippetForTag("snap.snap1.app1"), Equals,
		"low-priority\nmid-priority\nhigh-priority\n")
	c.Assert(spec.Snippets(), DeepEquals, map[string][]string{
		"snap.snap1.app1": {"low-priority", "mid-priority", "high-priority"},
	})
}

// TestSnippetEqualPriorityLexicographicOrder checks that snippets with equal
// priority are sorted lexicographically for a deterministic result.
func (s *specSuite) TestSnippetEqualPriorityLexicographicOrder(c *C) {
	iface := &ifacetest.TestInterface{
		InterfaceName: "test",
		DBusConnectedPlugCallback: func(spec *dbus.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
			// Same priority – insertion order should not matter.
			spec.AddSnippetWithPriority("zebra", 0)
			spec.AddSnippetWithPriority("apple", 0)
			spec.AddSnippetWithPriority("mango", 0)
			return nil
		},
	}

	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	spec := dbus.NewSpecification(appSet)
	c.Assert(spec.AddConnectedPlug(iface, s.plug, s.slot), IsNil)

	// Snippets with equal priority must be sorted lexicographically.
	c.Assert(spec.SnippetForTag("snap.snap1.app1"), Equals,
		"apple\nmango\nzebra\n")
	c.Assert(spec.Snippets(), DeepEquals, map[string][]string{
		"snap.snap1.app1": {"apple", "mango", "zebra"},
	})
}

// TestSnippetDeterministicRegardlessOfInsertionOrder ensures that two
// specifications with the same snippets added in different orders produce
// identical combined output.
func (s *specSuite) TestSnippetDeterministicRegardlessOfInsertionOrder(c *C) {
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)

	makeSpec := func(insertFn func(*dbus.Specification)) string {
		iface := &ifacetest.TestInterface{
			InterfaceName: "test",
			DBusConnectedPlugCallback: func(spec *dbus.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
				insertFn(spec)
				return nil
			},
		}
		spec := dbus.NewSpecification(appSet)
		c.Assert(spec.AddConnectedPlug(iface, s.plug, s.slot), IsNil)
		return spec.SnippetForTag("snap.snap1.app1")
	}

	order1 := makeSpec(func(spec *dbus.Specification) {
		spec.AddSnippetWithPriority("snippet-b", 2)
		spec.AddSnippetWithPriority("snippet-a", 1)
		spec.AddSnippetWithPriority("snippet-c", 2)
	})
	order2 := makeSpec(func(spec *dbus.Specification) {
		spec.AddSnippetWithPriority("snippet-c", 2)
		spec.AddSnippetWithPriority("snippet-b", 2)
		spec.AddSnippetWithPriority("snippet-a", 1)
	})

	c.Assert(order1, Equals, order2)
	c.Assert(order1, Equals, "snippet-a\nsnippet-b\nsnippet-c\n")
}

// TestAddSnippetDefaultPriority verifies that AddSnippet uses priority 0 and
// that mixing AddSnippet and AddSnippetWithPriority works correctly.
func (s *specSuite) TestAddSnippetDefaultPriority(c *C) {
	iface := &ifacetest.TestInterface{
		InterfaceName: "test",
		DBusConnectedPlugCallback: func(spec *dbus.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
			spec.AddSnippetWithPriority("before-default", -1)
			spec.AddSnippet("default-priority")
			spec.AddSnippetWithPriority("after-default", 1)
			return nil
		},
	}

	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	spec := dbus.NewSpecification(appSet)
	c.Assert(spec.AddConnectedPlug(iface, s.plug, s.slot), IsNil)

	// priority -1 < 0 (default) < 1
	c.Assert(spec.SnippetForTag("snap.snap1.app1"), Equals,
		"before-default\ndefault-priority\nafter-default\n")
}
