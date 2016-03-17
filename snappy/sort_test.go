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

package snappy

import (
	"sort"

	. "gopkg.in/check.v1"
)

type SortTestSuite struct {
}

var _ = Suite(&SortTestSuite{})

func (s *SortTestSuite) TestChOrder(c *C) {
	c.Assert(chOrder(uint8('~')), Equals, -1)
	c.Assert(chOrder(uint8('0')), Equals, 0)
	c.Assert(chOrder(uint8('2')), Equals, 0)
	c.Assert(chOrder(uint8('a')), Equals, 97)
}

func (s *SortTestSuite) TestVersionCompare(c *C) {
	c.Assert(VersionCompare("1.0", "2.0"), Equals, -1)
	c.Assert(VersionCompare("1.3", "1.2.2.2"), Equals, 1)

	c.Assert(VersionCompare("1.3", "1.3.1"), Equals, -1)

	c.Assert(VersionCompare("1.0", "1.0~"), Equals, 1)
	c.Assert(VersionCompare("7.2p2", "7.2"), Equals, 1)
	c.Assert(VersionCompare("0.4a6", "0.4"), Equals, 1)

	c.Assert(VersionCompare("0pre", "0pre"), Equals, 0)
	c.Assert(VersionCompare("0pree", "0pre"), Equals, 1)

	c.Assert(VersionCompare("1.18.36:5.4", "1.18.36:5.5"), Equals, -1)
	c.Assert(VersionCompare("1.18.36:5.4", "1.18.37:1.1"), Equals, -1)

	c.Assert(VersionCompare("2.0.7pre1", "2.0.7r"), Equals, -1)

	c.Assert(VersionCompare("0.10.0", "0.8.7"), Equals, 1)

	// subrev
	c.Assert(VersionCompare("1.0-1", "1.0-2"), Equals, -1)
	c.Assert(VersionCompare("1.0-1.1", "1.0-1"), Equals, 1)
	c.Assert(VersionCompare("1.0-1.1", "1.0-1.1"), Equals, 0)

	// do we like strange versions? Yes we like strange versions…
	c.Assert(VersionCompare("0", "0"), Equals, 0)
	c.Assert(VersionCompare("0", "00"), Equals, 0)
}

func (s *SortTestSuite) TestVersionInvalid(c *C) {
	c.Assert(VersionIsValid("1:2"), Equals, false)
	c.Assert(VersionIsValid("1--1"), Equals, false)
	c.Assert(VersionIsValid("1.0"), Equals, true)
}

func (s *SortTestSuite) TestSort(c *C) {
	versions := []string{"2.0", "1.0", "1.2.2", "1.2"}
	sort.Sort(ByVersion(versions))
	c.Assert(versions, DeepEquals, []string{"1.0", "1.2", "1.2.2", "2.0"})
}

func (s *SortTestSuite) TestSortSnaps(c *C) {
	snaps := []*Snap{
		&Snap{m: &snapYaml{Version: "2.0"}},
		&Snap{m: &snapYaml{Version: "1.0"}},
	}
	sort.Sort(BySnapVersion(snaps))
	c.Assert(snaps[0].Version(), Equals, "1.0")
	c.Assert(snaps[1].Version(), Equals, "2.0")
}

func (s *SortTestSuite) TestSideloadVersion(c *C) {
	n := 1000
	vs := make(ByVersion, n)

	for i := 0; i < n; i++ {
		vs[i] = newSideloadVersion()
	}

	c.Check(sort.IsSorted(vs), Equals, true)
}
