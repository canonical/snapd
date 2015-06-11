// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package partition

import (
	. "gopkg.in/check.v1"
)

func (s *PartitionTestSuite) TestMountEntryArray(c *C) {
	mea := mountEntryArray{}

	c.Assert(mea.Len(), Equals, 0)

	me := mountEntry{source: "/dev",
		target:    "/dev",
		options:   "bind",
		bindMount: true}

	mea = append(mea, me)
	c.Assert(mea.Len(), Equals, 1)

	me = mountEntry{source: "/foo",
		target:    "/foo",
		options:   "",
		bindMount: false}

	mea = append(mea, me)
	c.Assert(mea.Len(), Equals, 2)

	c.Assert(mea.Less(0, 1), Equals, true)
	c.Assert(mea.Less(1, 0), Equals, false)

	mea.Swap(0, 1)
	c.Assert(mea.Less(0, 1), Equals, false)
	c.Assert(mea.Less(1, 0), Equals, true)

	results := removeMountByTarget(mea, "invalid")

	// No change expected
	c.Assert(results, DeepEquals, mea)

	results = removeMountByTarget(mea, "/dev")

	c.Assert(len(results), Equals, 1)
	c.Assert(results[0], Equals, mountEntry{source: "/foo",
		target: "/foo", options: "", bindMount: false})
}

