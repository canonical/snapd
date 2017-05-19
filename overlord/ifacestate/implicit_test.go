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
	"github.com/snapcore/snapd/overlord/ifacestate"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"

	. "gopkg.in/check.v1"
)

type implicitSuite struct{}

var _ = Suite(&implicitSuite{})

func (implicitSuite) TestAddImplicitSlotsOutsideClassic(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	osYaml := []byte("name: ubuntu-core\ntype: os\n")
	info, err := snap.InfoFromSnapYaml(osYaml)
	c.Assert(err, IsNil)
	ifacestate.AddImplicitSlots(info)
	c.Assert(info.Slots["network"].Interface, Equals, "network")
	c.Assert(info.Slots["network"].Name, Equals, "network")
	c.Assert(info.Slots["network"].Snap, Equals, info)
	// Ensure that we have *some* implicit slots
	c.Assert(len(info.Slots) > 10, Equals, true)
}

func (implicitSuite) TestAddImplicitSlotsOnClassic(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	osYaml := []byte("name: ubuntu-core\ntype: os\n")
	info, err := snap.InfoFromSnapYaml(osYaml)
	c.Assert(err, IsNil)
	ifacestate.AddImplicitSlots(info)
	c.Assert(info.Slots["unity7"].Interface, Equals, "unity7")
	c.Assert(info.Slots["unity7"].Name, Equals, "unity7")
	c.Assert(info.Slots["unity7"].Snap, Equals, info)
	// Ensure that we have *some* implicit slots
	c.Assert(len(info.Slots) > 10, Equals, true)
}
