package tests

import . "gopkg.in/check.v1"

func (s *InstallSuite) TestSimple(c *C) {
	c.Assert(1, Equals, 1)
}
