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
	"github.com/snapcore/snapd/overlord"
	/*
		"github.com/snapcore/snapd/overlord/ifacestate"
		"github.com/snapcore/snapd/release"
		"github.com/snapcore/snapd/snap/snaptest"
	*/
	"github.com/snapcore/snapd/overlord/state"

	. "gopkg.in/check.v1"
)

type implicitSuite struct {
	o     *overlord.Overlord
	state *state.State
}

var _ = Suite(&implicitSuite{})

func (s *implicitSuite) SetUpTest(c *C) {
	s.o = overlord.Mock()
	s.state = s.o.State()
}

/*
func (s *implicitSuite) TestAddImplicitSlotsOnCore(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	info := snaptest.MockInfo(c, "{name: core, type: os, version: 0}", nil)
	ifacestate.AddImplicitSlots(s.state, info)
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
}

func (s *implicitSuite) TestAddImplicitSlotsOnClassic(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	info := snaptest.MockInfo(c, "{name: core, type: os, version: 0}", nil)
	ifacestate.AddImplicitSlots(s.state, info)
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
*/
