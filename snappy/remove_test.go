package snappy

import (
	"fmt"

	. "launchpad.net/gocheck"
)

func (s *SnapTestSuite) TestRemoveNonExistingRaisesError(c *C) {
	pkgName := "some-random-non-existing-stuff"
	err := Remove(pkgName)
	c.Assert(err, NotNil)
	c.Assert(err.Error(), Equals, fmt.Sprintf("Can not find snap %s", pkgName))
}
