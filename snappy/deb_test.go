package snappy

import (
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"

	. "launchpad.net/gocheck"
)

var testDebControl = []byte(`Package: foo
Version: 1.0
Architecture: all
Description: some description
`)

func makeTestDeb(c *C) string {
	builddir := c.MkDir()
	err := os.MkdirAll(filepath.Join(builddir, "DEBIAN"), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(builddir, "DEBIAN", "control"), testDebControl, 0644)
	c.Assert(err, IsNil)

	debName := filepath.Join(builddir, "foo_1.0_all.deb")
	cmd := exec.Command("fakeroot", "dpkg-deb", "-Zgzip", "--build", builddir, debName)
	err = cmd.Run()
	c.Assert(err, IsNil)

	return debName
}

func (s *SnapTestSuite) TestSnapDebControlContent(c *C) {
	debName := makeTestDeb(c)
	d := clickDeb{path: debName}
	content, err := d.controlContent("control")
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, string(testDebControl))
}

func (s *SnapTestSuite) TestClickVerifyContentFnSimple(c *C) {
	newPath, err := clickVerifyContentFn("foo")
	c.Assert(err, IsNil)
	c.Assert(newPath, Equals, "foo")
}

func (s *SnapTestSuite) TestClickVerifyContentFnStillOk(c *C) {
	newPath, err := clickVerifyContentFn("./foo/bar/../baz")
	c.Assert(err, IsNil)
	c.Assert(newPath, Equals, "foo/baz")
}

func (s *SnapTestSuite) TestClickVerifyContentFnNotOk(c *C) {
	_, err := clickVerifyContentFn("./foo/../../baz")
	c.Assert(err, Equals, ErrSnapInvalidContent)
}
