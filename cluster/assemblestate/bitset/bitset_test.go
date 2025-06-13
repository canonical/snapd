package bitset_test

import (
	"testing"

	"github.com/snapcore/snapd/cluster/assemblestate/bitset"
	"gopkg.in/check.v1"
)

type BitsetSuite struct{}

var _ = check.Suite(&BitsetSuite{})

func Test(t *testing.T) { check.TestingT(t) }

func (s *BitsetSuite) TestSetHasClear(c *check.C) {
	bs := bitset.Bitset[int]{}

	bs.Set(3)
	bs.Set(70)

	c.Assert(bs.Has(3), check.Equals, true)
	c.Assert(bs.Has(70), check.Equals, true)
	c.Assert(bs.Has(1), check.Equals, false)
	c.Assert(bs.Has(128), check.Equals, false)

	count := bs.Count()
	bs.Set(3)
	c.Assert(bs.Count(), check.Equals, count)

	bs.Clear(3)
	c.Assert(bs.Has(3), check.Equals, false)
	c.Assert(bs.Count(), check.Equals, 1)
}

func (s *BitsetSuite) TestAll(c *check.C) {
	expected := []int{0, 1, 63, 64, 127}
	bs := bitset.Bitset[int]{}
	for _, v := range expected {
		bs.Set(v)
	}

	c.Assert(bs.All(), check.DeepEquals, expected)
}

func (s *BitsetSuite) TestRange(c *check.C) {
	values := []int{2, 65, 5, 130}
	bs := bitset.Bitset[int]{}
	for _, v := range values {
		bs.Set(v)
	}

	got := make([]int, 0, len(values))
	bs.Range(func(v int) bool {
		got = append(got, v)
		return true
	})

	c.Assert(got, check.DeepEquals, []int{2, 5, 65, 130})

	var first int
	var called bool
	bs.Range(func(v int) bool {
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
	one := bitset.Bitset[int]{}
	two := bitset.Bitset[int]{}

	for _, v := range []int{1, 2, 70, 100, 256, 1024} {
		one.Set(v)
	}
	for _, v := range []int{2, 3, 100, 256} {
		two.Set(v)
	}

	diff := one.Diff(&two)
	c.Assert(diff.Has(1), check.Equals, true)
	c.Assert(diff.Has(2), check.Equals, false)
	c.Assert(diff.Has(3), check.Equals, false)
	c.Assert(diff.Has(70), check.Equals, true)
	c.Assert(diff.Has(100), check.Equals, false)
	c.Assert(diff.Has(256), check.Equals, false)
	c.Assert(diff.Has(1024), check.Equals, true)
	c.Assert(diff.Count(), check.Equals, 3)

	one = bitset.Bitset[int]{}
	one.Set(1)

	two = bitset.Bitset[int]{}
	two.Set(1)

	c.Assert(one.Diff(&two).Count(), check.Equals, 0)
}

func (s *BitsetSuite) TestEquals(c *check.C) {
	x := bitset.Bitset[int]{}
	y := bitset.Bitset[int]{}

	x.Set(10)
	x.Set(200)

	y.Set(10)
	y.Set(200)

	c.Assert(x.Equals(&y), check.Equals, true)

	y.Set(1)
	c.Assert(x.Equals(&y), check.Equals, false)
}

func (s *BitsetSuite) TestCount(c *check.C) {
	bs := bitset.Bitset[int]{}
	for i := 0; i < 100; i++ {
		bs.Set(i)
	}
	c.Assert(bs.Count(), check.Equals, 100)
}
