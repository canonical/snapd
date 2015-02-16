package main

import (
	"testing"

	. "launchpad.net/gocheck"
)

// Hook up gocheck into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type CmdTestSuite struct {
}

var _ = Suite(&CmdTestSuite{})

func (s *CmdTestSuite) TestParseSetPropertyCmdline(c *C) {

	// simple case
	pkgname, args, err := parseSetPropertyCmdline("hello-world", "active=1")
	c.Assert(err, IsNil)
	c.Assert(pkgname, Equals, "hello-world")
	c.Assert(args, DeepEquals, []string{"active=1"})

	// special case, see spec
	// ensure that just "active=$ver" uses "ubuntu-core" as the pkg
	pkgname, args, err = parseSetPropertyCmdline("active=181")
	c.Assert(err, IsNil)
	c.Assert(pkgname, Equals, "ubuntu-core")
	c.Assert(args, DeepEquals, []string{"active=181"})
}
