// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/overlord/ifacestate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

type helpersSuite struct {
	st *state.State
}

var _ = Suite(&helpersSuite{})

func (s *helpersSuite) SetUpTest(c *C) {
	s.st = state.New(nil)
}

func (s *helpersSuite) TearDownTest(c *C) {
}

// MockSnapdPresence arranges for state to have the required presence of "snapd" snap.
func MockSnapdPresence(c *C, st *state.State, isPresent bool) (restore func()) {
	var origState snapstate.SnapState

	err := snapstate.Get(st, "snapd", &origState)
	if err != state.ErrNoState {
		c.Assert(err, IsNil)
	}

	if isPresent {
		snapstate.Set(st, "snapd", &snapstate.SnapState{
			SnapType: string(snap.TypeApp),
			Sequence: []*snap.SideInfo{{
				Revision: snap.R(1),
			}},
			Active:  true,
			Current: snap.R(1),
		})
	} else {
		snapstate.Set(st, "snapd", nil)
	}

	return func() {
		// Restoring a snap state with empty sequence is just like removing it
		// so we don't need to special-case or remember if ErrNoState happened.
		snapstate.Set(st, "snapd", &origState)
	}
}

func (s *helpersSuite) TestHasSnapdSnap(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	// Not having any state means we don't "have" snapd snap
	c.Assert(ifacestate.HasSnapdSnap(s.st), Equals, false)

	// Having an active "snapd" snap means we have it.
	snapstate.Set(s.st, "snapd", &snapstate.SnapState{
		SnapType: string(snap.TypeApp),
		Sequence: []*snap.SideInfo{{
			Revision: snap.R(1),
		}},
		Active:  true,
		Current: snap.R(1),
	})
	c.Assert(ifacestate.HasSnapdSnap(s.st), Equals, true)

	// Having an inactive "snapd" snap also means we have it.
	snapstate.Set(s.st, "snapd", &snapstate.SnapState{
		SnapType: string(snap.TypeApp),
		Sequence: []*snap.SideInfo{{
			Revision: snap.R(1),
		}},
		Current: snap.R(1),
	})
	c.Assert(ifacestate.HasSnapdSnap(s.st), Equals, true)
}

func (s *helpersSuite) TestRemapIncomingConnRef(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	// When "snapd" snap is present, incoming requests re-map "core" snap
	// to "snapd" snap for interface connections.
	restore := MockSnapdPresence(c, s.st, true)
	defer restore()

	cref := &interfaces.ConnRef{
		PlugRef: interfaces.PlugRef{Snap: "example", Name: "network"},
		SlotRef: interfaces.SlotRef{Snap: "core", Name: "network"},
	}
	ifacestate.RemapIncomingConnRef(s.st, cref)
	c.Assert(cref, DeepEquals, &interfaces.ConnRef{
		PlugRef: interfaces.PlugRef{Snap: "example", Name: "network"},
		SlotRef: interfaces.SlotRef{Snap: "snapd", Name: "network"},
	})

	// When "snapd" snap is absent, requests are not changed in any way.
	restore = MockSnapdPresence(c, s.st, false)
	defer restore()

	cref = &interfaces.ConnRef{
		PlugRef: interfaces.PlugRef{Snap: "example", Name: "network"},
		SlotRef: interfaces.SlotRef{Snap: "core", Name: "network"},
	}
	ifacestate.RemapIncomingConnRef(s.st, cref)
	c.Assert(cref, DeepEquals, &interfaces.ConnRef{
		PlugRef: interfaces.PlugRef{Snap: "example", Name: "network"},
		SlotRef: interfaces.SlotRef{Snap: "core", Name: "network"},
	})
}

func (s *helpersSuite) TestRemapIncomingConnStrings(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	// When "snapd" snap is present, incoming requests re-map "core" snap
	// to "snapd" snap for interface connections.
	restore := MockSnapdPresence(c, s.st, true)
	defer restore()

	plugSnap, plugName, slotSnap, slotName := "example", "network", "core", "network"
	plugSnapX, plugNameX, slotSnapX, slotNameX := ifacestate.RemapIncomingConnStrings(s.st, plugSnap, plugName, slotSnap, slotName)
	c.Assert(plugSnapX, Equals, "example")
	c.Assert(plugNameX, Equals, "network")
	c.Assert(slotSnapX, Equals, "snapd")
	c.Assert(slotNameX, Equals, "network")

	// When "snapd" snap is absent, requests are not changed in any way.
	restore = MockSnapdPresence(c, s.st, false)
	defer restore()

	plugSnapX, plugNameX, slotSnapX, slotNameX = ifacestate.RemapIncomingConnStrings(s.st, plugSnap, plugName, slotSnap, slotName)
	c.Assert(plugSnapX, Equals, "example")
	c.Assert(plugNameX, Equals, "network")
	c.Assert(slotSnapX, Equals, "core")
	c.Assert(slotNameX, Equals, "network")
}

func (s *helpersSuite) TestRemapOutgoingConnRef(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	// When "snapd" snap is present, outgoing requests re-map "snapd" snap
	// to "core" snap for on-disk data.
	restore := MockSnapdPresence(c, s.st, true)
	defer restore()

	cref := &interfaces.ConnRef{
		PlugRef: interfaces.PlugRef{Snap: "example", Name: "network"},
		SlotRef: interfaces.SlotRef{Snap: "snapd", Name: "network"},
	}
	ifacestate.RemapOutgoingConnRef(s.st, cref)
	c.Assert(cref, DeepEquals, &interfaces.ConnRef{
		PlugRef: interfaces.PlugRef{Snap: "example", Name: "network"},
		SlotRef: interfaces.SlotRef{Snap: "core", Name: "network"},
	})
}
