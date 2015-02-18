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
	pkgname, args, err := parseSetPropertyCmdline("hello-world", "channel=edge")
	c.Assert(err, IsNil)
	c.Assert(pkgname, Equals, "hello-world")
	c.Assert(args, DeepEquals, []string{"channel=edge"})

	// special case, see spec
	// ensure that just "active=$ver" uses "ubuntu-core" as the pkg
	pkgname, args, err = parseSetPropertyCmdline("channel=alpha")
	c.Assert(err, IsNil)
	c.Assert(pkgname, Equals, "ubuntu-core")
	c.Assert(args, DeepEquals, []string{"channel=alpha"})
}
