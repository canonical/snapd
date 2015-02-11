package snappy

import (
	. "launchpad.net/gocheck"
)

func (s *SnapTestSuite) TestSetPropertyEmpty(c *C) {
	err := SetProperty([]string{})
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
	err := SetProperty([]string{"hello-world", "null=1.61"})
	c.Assert(err, IsNil)

	// simple-case for set-active
	err = SetProperty([]string{"hello-world", "active=2.71"})
	c.Assert(err, IsNil)
	c.Assert(activePkg, Equals, "hello-world")
	c.Assert(activeVer, Equals, "2.71")

	// special case, see spec
	// ensure that just "active=$ver" uses "ubuntu-core" as the pkg
	err = SetProperty([]string{"active=181"})
	c.Assert(err, IsNil)
	c.Assert(activePkg, Equals, "ubuntu-core")
	c.Assert(activeVer, Equals, "181")

	// ensure unknown property raises a error
	err = SetProperty([]string{"no-such-property=foo"})
	c.Assert(err, NotNil)
}
