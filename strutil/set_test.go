// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

package strutil_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/strutil"
)

type orderedSetSuite struct {
	set strutil.OrderedSet
}

var _ = Suite(&orderedSetSuite{})

func (s *orderedSetSuite) SetUpTest(c *C) {
	s.set = strutil.OrderedSet{}
}

func (s *orderedSetSuite) TestZeroValueItems(c *C) {
	c.Assert(s.set.Items(), HasLen, 0)
}

func (s *orderedSetSuite) TestZeroValueContains(c *C) {
	c.Check(s.set.Contains("foo"), Equals, false)
}

func (s *orderedSetSuite) TestZeroValueIndexOf(c *C) {
	c.Check(s.set.Contains("foo"), Equals, false)
}

func (s *orderedSetSuite) TestZeroValuePut(c *C) {
	items := []string{"foo", "bar", "froz"}
	for idx, item := range items {
		s.set.Put(item)
		c.Check(s.set.Contains(item), Equals, true)
		realIdx, ok := s.set.IndexOf(item)
		c.Check(ok, Equals, true)
		c.Check(idx, Equals, realIdx)
		c.Check(s.set.Size(), Equals, idx+1)
		c.Check(s.set.Items(), DeepEquals, items[:idx+1])
	}
}

func (s *orderedSetSuite) TestZeroValueSize(c *C) {
	c.Assert(s.set.Size(), Equals, 0)
}

func (s *orderedSetSuite) TestDeduplication(c *C) {
	s.set.Put("a")
	s.set.Put("b")
	s.set.Put("a")
	s.set.Put("c")

	c.Assert(s.set.Items(), DeepEquals, []string{"a", "b", "c"})
	c.Check(s.set.Size(), Equals, 3)
	c.Check(s.set.Contains("a"), Equals, true)
	c.Check(s.set.Contains("b"), Equals, true)
	c.Check(s.set.Contains("c"), Equals, true)
}
