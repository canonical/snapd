// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package snap_test

import (
	"github.com/ubuntu-core/snappy/interfaces/builtin"
	"github.com/ubuntu-core/snappy/snap"

	. "gopkg.in/check.v1"
)

type SpecialSuite struct{}

var _ = Suite(&SpecialSuite{})

func (s *InfoSnapYamlTestSuite) TestAddImplicitSlots(c *C) {
	osYaml := []byte("name: ubuntu-core\ntype: os\n")
	info, err := snap.InfoFromSnapYaml(osYaml)
	c.Assert(err, IsNil)
	snap.AddImplicitSlots(info)
	c.Assert(info.Slots["network"].Interface, Equals, "network")
	c.Assert(info.Slots["network"].Name, Equals, "network")
	c.Assert(info.Slots["network"].Snap, Equals, info)
	c.Assert(info.Slots, HasLen, 15)
}

func (s *InfoSnapYamlTestSuite) TestImplicitSlotsAreRealInterfaces(c *C) {
	known := make(map[string]bool)
	for _, iface := range builtin.Interfaces() {
		known[iface.Name()] = true
	}
	for _, ifaceName := range snap.ImplicitSlotsForTests {
		c.Check(known[ifaceName], Equals, true)
	}
}
