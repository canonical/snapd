package snappy

import (
	. "launchpad.net/gocheck"
)

func (s *SnapTestSuite) TestParseSetPropertyCmdlineEmpty(c *C) {
	err := ParseSetPropertyCmdline([]string{}...)
	c.Assert(err, NotNil)
}

func (s *SnapTestSuite) TestParseSetPropertyCmdline(c *C) {
	var activePkg, activeVer string
	mockSetActive := func(k, v string) error {
		activePkg = k
		activeVer = v
		return nil
	}
	setFuncs = map[string]func(k, v string) error{
		"active": mockSetActive,
		"null":   func(k, v string) error { return nil },
	}

	// the "null" property in this test does nothing, its just
	// there to ensure that setFunc works with multiple entries
	err := ParseSetPropertyCmdline("hello-world", "null=1.61")
	c.Assert(err, IsNil)

	// simple-case for set-active
	err = ParseSetPropertyCmdline("hello-world", "active=2.71")
	c.Assert(err, IsNil)
	c.Assert(activePkg, Equals, "hello-world")
	c.Assert(activeVer, Equals, "2.71")

	// special case, see spec
	// ensure that just "active=$ver" uses "ubuntu-core" as the pkg
	err = ParseSetPropertyCmdline("active=181")
	c.Assert(err, IsNil)
	c.Assert(activePkg, Equals, "ubuntu-core")
	c.Assert(activeVer, Equals, "181")

	// ensure unknown property raises a error
	err = ParseSetPropertyCmdline("no-such-property=foo")
	c.Assert(err, NotNil)

	// ensure incorrect format raises a error
	err = ParseSetPropertyCmdline("hello-world", "active")
	c.Assert(err, NotNil)

	// ensure additional "=" in active are ok (even though this is
	// not a valid version number)
	err = ParseSetPropertyCmdline("hello-world", "active=1.0=really2.0")
	c.Assert(err, IsNil)
	c.Assert(activePkg, Equals, "hello-world")
	c.Assert(activeVer, Equals, "1.0=really2.0")
}
