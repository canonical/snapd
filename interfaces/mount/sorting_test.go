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

package mount

import (
	"sort"

	. "gopkg.in/check.v1"
)

type sortSuite struct{}

var _ = Suite(&sortSuite{})

func (s *sortSuite) TestTrailingSlashesComparison(c *C) {
	entries := []Entry{{Dir: "b"}, {Dir: "ba"}, {Dir: "ab"}, {Dir: "a"}}
	sort.Sort(byDir(entries))
	c.Assert(entries, DeepEquals, []Entry{
		{Dir: "a"}, {Dir: "ab"}, {Dir: "b"}, {Dir: "ba"},
	})
}
