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

package ifacestate_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/ifacestate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
)

type implicitSuite struct{}

var _ = Suite(&implicitSuite{})

func (implicitSuite) TestAddImplicitSlotsOnCore(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	st := state.New(nil)
	hotplugSlots := map[string]*ifacestate.HotplugSlotInfo{
		"foo": {
			Name:        "foo",
			Interface:   "empty",
			StaticAttrs: map[string]interface{}{"attr": "value"},
			HotplugKey:  "1234",
		},
	}
	st.Lock()
	defer st.Unlock()
	st.Set("hotplug-slots", hotplugSlots)

	info := snaptest.MockInfo(c, "{name: core, type: os, version: 0}", nil)
	c.Assert(ifacestate.AddImplicitSlots(st, info), IsNil)
	// Ensure that some slots that exist in core systems are present.
	for _, name := range []string{"network"} {
		slot := info.Slots[name]
		c.Assert(slot.Interface, Equals, name)
		c.Assert(slot.Name, Equals, name)
		c.Assert(slot.Snap, Equals, info)
	}
	// Ensure that some slots that exist is just classic systems are absent.
	for _, name := range []string{"unity7"} {
		c.Assert(info.Slots[name], IsNil)
	}

	// Ensure that we have *some* implicit slots
	c.Assert(len(info.Slots) > 10, Equals, true)

	// Ensure hotplug slots were added
	slot := info.Slots["foo"]
	c.Assert(slot, NotNil)
	c.Assert(slot.Interface, Equals, "empty")
	c.Assert(slot.Attrs, DeepEquals, map[string]interface{}{"attr": "value"})
	c.Assert(slot.HotplugKey, DeepEquals, snap.HotplugKey("1234"))
}

func (implicitSuite) TestAddImplicitSlotsOnClassic(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	info := snaptest.MockInfo(c, "{name: core, type: os, version: 0}", nil)

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	c.Assert(ifacestate.AddImplicitSlots(st, info), IsNil)
	// Ensure that some slots that exist in classic systems are present.
	for _, name := range []string{"network", "unity7"} {
		slot := info.Slots[name]
		c.Assert(slot.Interface, Equals, name)
		c.Assert(slot.Name, Equals, name)
		c.Assert(slot.Snap, Equals, info)
	}
	// Ensure that we have *some* implicit slots
	c.Assert(len(info.Slots) > 10, Equals, true)
}

func (implicitSuite) TestAddImplicitSlotsErrorSlotExists(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	info := snaptest.MockInfo(c, "{name: core, type: os, version: 0}", nil)

	st := state.New(nil)
	hotplugSlots := map[string]*ifacestate.HotplugSlotInfo{
		"unity7": {
			Name:       "unity7",
			Interface:  "unity7",
			HotplugKey: "1234",
		},
	}
	st.Lock()
	defer st.Unlock()
	st.Set("hotplug-slots", hotplugSlots)

	c.Assert(ifacestate.AddImplicitSlots(st, info), ErrorMatches, `cannot add hotplug slot unity7: slot already exists`)
}
