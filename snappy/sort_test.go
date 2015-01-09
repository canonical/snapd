package snappy

import (
	. "gopkg.in/check.v1"
)

type SortTestSuite struct{
}

var _ = Suite(&SortTestSuite{})


func (s *SortTestSuite) TestVersionCompare(c *C) {
	c.Assert(VersionCompare("1.0", "2.0"), Equals, -1)
	c.Assert(VersionCompare("1.3", "1.2.2.2"), Equals, 1)

	c.Assert(VersionCompare("1.3", "1.3.1"), Equals, -1)

	c.Assert(VersionCompare("7.2p2", "7.2"), Equals, 1)
	c.Assert(VersionCompare("0.4a6", "0.4"), Equals, 1)
	
	c.Assert(VersionCompare("0pre", "0pre"), Equals, 0)
}
