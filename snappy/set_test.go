package snappy

import (
	. "launchpad.net/gocheck"
)

func (s *SnapTestSuite) TestParseSetPropertyCmdlineEmpty(c *C) {
	err := SetProperty("ubuntu-core", []string{}...)
	c.Assert(err, NotNil)
}

func (s *SnapTestSuite) TestSetProperty(c *C) {
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
	err := SetProperty("hello-world", "null=1.61")
	c.Assert(err, IsNil)

	// simple-case for set-active
	err = SetProperty("hello-world", "active=2.71")
	c.Assert(err, IsNil)
	c.Assert(activePkg, Equals, "hello-world")
	c.Assert(activeVer, Equals, "2.71")

	// ensure unknown property raises a error
	err = SetProperty("ubuntu-core", "no-such-property=foo")
	c.Assert(err, NotNil)

	// ensure incorrect format raises a error
	err = SetProperty("hello-world", "active")
	c.Assert(err, NotNil)

	// ensure additional "=" in active are ok (even though this is
	// not a valid version number)
	err = SetProperty("hello-world", "active=1.0=really2.0")
	c.Assert(err, IsNil)
	c.Assert(activePkg, Equals, "hello-world")
	c.Assert(activeVer, Equals, "1.0=really2.0")
}
