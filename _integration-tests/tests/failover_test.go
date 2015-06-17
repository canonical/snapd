package tests

import . "gopkg.in/check.v1"

type FailoverSuite struct {
	CommonSuite
}

var _ = Suite(&FailoverSuite{})

func (s *FailoverSuite) TestSimple(c *C) {
	c.Assert(1, Equals, 1)
}
