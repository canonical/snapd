package snappy

import (
	. "launchpad.net/gocheck"
)

func (s *SnapTestSuite) TestRollbackWithVersion(c *C) {
	makeTwoTestSnaps(c, SnapTypeApp)
	c.Assert(ActiveSnapByName("foo").Version(), Equals, "2.0")

	// rollback with version
	version, err := Rollback("foo", "1.0")
	c.Assert(err, IsNil)
	c.Assert(version, Equals, "1.0")

	c.Assert(ActiveSnapByName("foo").Version(), Equals, "1.0")
}

func (s *SnapTestSuite) TestRollbackFindVersion(c *C) {
	makeTwoTestSnaps(c, SnapTypeApp)
	c.Assert(ActiveSnapByName("foo").Version(), Equals, "2.0")

	// rollback without version
	version, err := Rollback("foo", "")
	c.Assert(err, IsNil)
	c.Assert(version, Equals, "1.0")

	c.Assert(ActiveSnapByName("foo").Version(), Equals, "1.0")
}
