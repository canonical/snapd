package bimap_test

import (
	"testing"

	"github.com/snapcore/snapd/cluster/assemblestate/bimap"
	"gopkg.in/check.v1"
)

type BimapSuite struct{}

var _ = check.Suite(&BimapSuite{})

func Test(t *testing.T) { check.TestingT(t) }

func (s *BimapSuite) TestAdd(c *check.C) {
	bm := bimap.New[string, int]()

	indexA := bm.Add("a")
	c.Assert(indexA, check.Equals, 0)

	indexB := bm.Add("b")
	c.Assert(indexB, check.Equals, 1)

	// adding another value doesn't invalidate other indexes
	indexA = bm.Add("a")
	c.Assert(indexA, check.Equals, 0)

	c.Assert(len(bm.Values()), check.Equals, 2, check.Commentf("duplicate insert expanded slice"))
}

func (s *BimapSuite) TestIndexOfAndValue(c *check.C) {
	bm := bimap.New[string, int]()
	bm.Add("a")

	idx, ok := bm.IndexOf("a")
	c.Assert(ok, check.Equals, true)
	c.Assert(idx, check.Equals, 0)

	_, ok = bm.IndexOf("b")
	c.Assert(ok, check.Equals, false)
}

func (s *BimapSuite) TestValue(c *check.C) {
	bm := bimap.New[string, int]()
	idx := bm.Add("a")

	val := bm.Value(idx)
	c.Assert(val, check.Equals, "a")
}

func (s *BimapSuite) TestValuesOrdering(c *check.C) {
	expected := []string{"x", "y", "z"}

	bm := bimap.New[string, int]()
	for _, v := range expected {
		bm.Add(v)
	}

	c.Assert(bm.Values(), check.DeepEquals, expected)
}
