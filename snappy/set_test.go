package snappy

import (
	"os"
	"path/filepath"
	"strings"

	. "launchpad.net/gocheck"
)

func (s *SnapTestSuite) TestParseSetPropertyCmdlineEmpty(c *C) {
	err := SetProperty("ubuntu-core", []string{}...)
	c.Assert(err, NotNil)
}

func (s *SnapTestSuite) TestSetProperty(c *C) {
	var ratingPkg, ratingVal string
	mockSetRating := func(k, v string) error {
		ratingPkg = k
		ratingVal = v
		return nil
	}
	setFuncs = map[string]func(k, v string) error{
		"rating": mockSetRating,
		"null":   func(k, v string) error { return nil },
	}

	// the "null" property in this test does nothing, its just
	// there to ensure that setFunc works with multiple entries
	err := SetProperty("hello-world", "null=1.61")
	c.Assert(err, IsNil)

	// simple-case for set
	err = SetProperty("hello-world", "rating=4")
	c.Assert(err, IsNil)
	c.Assert(ratingPkg, Equals, "hello-world")
	c.Assert(ratingVal, Equals, "4")

	// ensure unknown property raises a error
	err = SetProperty("ubuntu-core", "no-such-property=foo")
	c.Assert(err, NotNil)

	// ensure incorrect format raises a error
	err = SetProperty("hello-world", "rating")
	c.Assert(err, NotNil)

	// ensure additional "=" in SetProperty are ok (even though this is
	// not a valid rating of course)
	err = SetProperty("hello-world", "rating=1=2")
	c.Assert(err, IsNil)
	c.Assert(ratingPkg, Equals, "hello-world")
	c.Assert(ratingVal, Equals, "1=2")
}

func (s *SnapTestSuite) TestSetActive(c *C) {
	makeTwoTestSnaps(c, SnapTypeApp)

	path, err := filepath.EvalSymlinks(filepath.Join(snapAppsDir, "foo", "current"))
	c.Assert(err, IsNil)
	c.Assert(strings.HasSuffix(path, "/foo/2.0"), Equals, true)

	// setActive has some ugly print
	devnull, err := os.Open(os.DevNull)
	c.Assert(err, IsNil)
	oldStdout := os.Stdout
	os.Stdout = devnull
	defer func() {
		os.Stdout = oldStdout
	}()

	err = makeSnapActiveByNameAndVersion("foo", "1.0")
	c.Assert(err, IsNil)
	path, err = filepath.EvalSymlinks(filepath.Join(snapAppsDir, "foo", "current"))
	c.Assert(strings.HasSuffix(path, "/foo/1.0"), Equals, true)
}
