// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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

package intset_test

import (
	"testing"

	"github.com/snapcore/snapd/cluster/assemblestate/intset"
	"gopkg.in/check.v1"
)

type BitsetSuite struct{}

var _ = check.Suite(&BitsetSuite{})

func Test(t *testing.T) { check.TestingT(t) }

func (s *BitsetSuite) TestSetHasClear(c *check.C) {
	is := intset.IntSet[int]{}

	is.Add(3)
	is.Add(70)

	c.Assert(is.Contains(3), check.Equals, true)
	c.Assert(is.Contains(70), check.Equals, true)
	c.Assert(is.Contains(1), check.Equals, false)
	c.Assert(is.Contains(128), check.Equals, false)

	count := is.Count()
	is.Add(3)
	c.Assert(is.Count(), check.Equals, count)

	is.Remove(3)
	c.Assert(is.Contains(3), check.Equals, false)
	c.Assert(is.Count(), check.Equals, 1)
}

func (s *BitsetSuite) TestAll(c *check.C) {
	expected := []int{0, 1, 63, 64, 127}
	is := intset.IntSet[int]{}
	for _, v := range expected {
		is.Add(v)
	}

	c.Assert(is.All(), check.DeepEquals, expected)
}

func (s *BitsetSuite) TestRange(c *check.C) {
	values := []int{2, 65, 5, 130}
	is := intset.IntSet[int]{}
	for _, v := range values {
		is.Add(v)
	}

	got := make([]int, 0, len(values))
	is.Range(func(v int) bool {
		got = append(got, v)
		return true
	})

	c.Assert(got, check.DeepEquals, []int{2, 5, 65, 130})

	var first int
	var called bool
	is.Range(func(v int) bool {
		if called {
			c.Fatal("unexpected second call")
		}
		called = true
		first = v
		return false
	})
	c.Assert(first, check.Equals, 2)
}

func (s *BitsetSuite) TestDiff(c *check.C) {
	one := intset.IntSet[int]{}
	two := intset.IntSet[int]{}

	for _, v := range []int{1, 2, 70, 100, 256, 1024} {
		one.Add(v)
	}
	for _, v := range []int{2, 3, 100, 256} {
		two.Add(v)
	}

	diff := one.Diff(&two)
	c.Assert(diff.Contains(1), check.Equals, true)
	c.Assert(diff.Contains(2), check.Equals, false)
	c.Assert(diff.Contains(3), check.Equals, false)
	c.Assert(diff.Contains(70), check.Equals, true)
	c.Assert(diff.Contains(100), check.Equals, false)
	c.Assert(diff.Contains(256), check.Equals, false)
	c.Assert(diff.Contains(1024), check.Equals, true)
	c.Assert(diff.Count(), check.Equals, 3)

	one = intset.IntSet[int]{}
	one.Add(1)

	two = intset.IntSet[int]{}
	two.Add(1)

	c.Assert(one.Diff(&two).Count(), check.Equals, 0)
}

func (s *BitsetSuite) TestEqual(c *check.C) {
	x := intset.IntSet[int]{}
	y := intset.IntSet[int]{}

	x.Add(10)
	x.Add(200)

	y.Add(10)
	y.Add(200)

	c.Assert(x.Equal(&y), check.Equals, true)

	y.Add(1)
	c.Assert(x.Equal(&y), check.Equals, false)

	// check that self-equality works, should use a pointer comparison
	c.Assert(x.Equal(&x), check.Equals, true)
}

func (s *BitsetSuite) TestEqualSizeDiff(c *check.C) {
	x := intset.IntSet[int]{}
	y := intset.IntSet[int]{}

	x.Add(100)
	x.Remove(100)

	c.Assert(x.Equal(&y), check.Equals, true)
	c.Assert(y.Equal(&x), check.Equals, true)

	y.Add(100)

	c.Assert(x.Equal(&y), check.Equals, false)
	c.Assert(y.Equal(&x), check.Equals, false)
}

func (s *BitsetSuite) TestCount(c *check.C) {
	is := intset.IntSet[int]{}
	for i := 0; i < 100; i++ {
		is.Add(i)
	}
	c.Assert(is.Count(), check.Equals, 100)
}
