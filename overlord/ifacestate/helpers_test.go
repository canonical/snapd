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
	"github.com/snapcore/snapd/overlord/state"
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

func (s *helpersSuite) TestRemapIncomingConnRef(c *C) {
	// When "snapd" snap is the host for implicit slots then slots on core are
	// re-mapped to slots on snapd.
	restore := ifacestate.MockImplicitSlotsOnSnapd(true)
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

	// When "snapd" is not the host for implicit slots then nothing is changed.
	restore = ifacestate.MockImplicitSlotsOnSnapd(false)
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

func (s *helpersSuite) TestRemapOutgoingConnRef(c *C) {
	restore := ifacestate.MockImplicitSlotsOnSnapd(true)
	defer restore()

	cref := &interfaces.ConnRef{
		PlugRef: interfaces.PlugRef{Snap: "example", Name: "network"},
		SlotRef: interfaces.SlotRef{Snap: "snapd", Name: "network"},
	}
	// Outgoing connection references are re-mapped when snapd is the host of
	// implicit slots so that on the outside, it seems that core is the host
	// (consistently with pre-snapd behavior).
	ifacestate.RemapOutgoingConnRef(s.st, cref)
	c.Assert(cref, DeepEquals, &interfaces.ConnRef{
		PlugRef: interfaces.PlugRef{Snap: "example", Name: "network"},
		SlotRef: interfaces.SlotRef{Snap: "core", Name: "network"},
	})
}
