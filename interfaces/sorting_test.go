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

package interfaces

import (
	"sort"

	. "gopkg.in/check.v1"
)

type SortingSuite struct{}

var _ = Suite(&SortingSuite{})

func (s *SortingSuite) TestSortBySlotRef(c *C) {
	list := []SlotRef{
		{
			Snap: "snap-2",
			Name: "name-2",
		},
		{
			Snap: "snap-1",
			Name: "name-2",
		},
		{
			Snap: "snap-1",
			Name: "name-1",
		},
	}
	sort.Sort(bySlotRef(list))
	c.Assert(list, DeepEquals, []SlotRef{
		{
			Snap: "snap-1",
			Name: "name-1",
		},
		{
			Snap: "snap-1",
			Name: "name-2",
		},
		{
			Snap: "snap-2",
			Name: "name-2",
		},
	})
}

func (s *SortingSuite) TestSortByPlugRef(c *C) {
	list := []PlugRef{
		{
			Snap: "snap-2",
			Name: "name-2",
		},
		{
			Snap: "snap-1",
			Name: "name-2",
		},
		{
			Snap: "snap-1",
			Name: "name-1",
		},
	}
	sort.Sort(byPlugRef(list))
	c.Assert(list, DeepEquals, []PlugRef{
		{
			Snap: "snap-1",
			Name: "name-1",
		},
		{
			Snap: "snap-1",
			Name: "name-2",
		},
		{
			Snap: "snap-2",
			Name: "name-2",
		},
	})
}
