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

	c.Assert(VersionCompare("7.2p2", "7.2"), Equals, 1)
	c.Assert(VersionCompare("0.4a6", "0.4"), Equals, 1)

	c.Assert(VersionCompare("0pre", "0pre"), Equals, 0)
	c.Assert(VersionCompare("0pree", "0pre"), Equals, 1)

	c.Assert(VersionCompare("1.18.36:5.4", "1.18.36:5.5"), Equals, -1)
	c.Assert(VersionCompare("1.18.36:5.4", "1.18.37:1.1"), Equals, -1)
	c.Assert(VersionCompare("1.18.36-0.17.35", "1.18.36"), Equals, 1)

	c.Assert(VersionCompare("2.0.7pre1", "2.0.7r"), Equals, -1)

	c.Assert(VersionCompare("0.10.0", "0.8.7"), Equals, 1)

	// do we like strange versions? Yes we like strange versions…
	c.Assert(VersionCompare("0", "0"), Equals, 0)
	c.Assert(VersionCompare("0", "00"), Equals, 0)
}

func (s *SortTestSuite) TestSort(c *C) {

	versions := []string{"2.0", "1.0", "1.2.2", "1.2"}
	sort.Sort(ByVersion(versions))
	c.Assert(versions, DeepEquals, []string{"1.0", "1.2", "1.2.2", "2.0"})
}
