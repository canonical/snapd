package main_test

import (
	. "gopkg.in/check.v1"

	snap "github.com/snapcore/snapd/cmd/snap"
)

func (s *SnapSuite) TestNotLoggedInUserWhoami(c *C) {
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"whoami"})
	c.Assert(err, IsNil)
	c.Check(s.Stdout(), Equals, "email: -\n")
}

func (s *SnapSuite) TestExtraParamErrorWhoami(c *C) {
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"whoami", "test"})
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, "too many arguments for command")
}
