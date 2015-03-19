package main

import (
	. "launchpad.net/gocheck"
)

func (s *CmdTestSuite) TestSplitPkgAndDeveloperSimple(c *C) {
	name, developer := pkgAndDeveloper("paste.mvo")
	c.Assert(name, Equals, "paste")
	c.Assert(developer, Equals, "mvo")
}

func (s *CmdTestSuite) TestSplitPkgAndDeveloperNoDeveloper(c *C) {
	name, developer := pkgAndDeveloper("paste")
	c.Assert(name, Equals, "paste")
	c.Assert(developer, Equals, "")
}
