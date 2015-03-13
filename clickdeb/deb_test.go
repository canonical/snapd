package clickdeb

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	. "launchpad.net/gocheck"
	"launchpad.net/snappy/helpers"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

type ClickDebTestSuite struct {
}

var _ = Suite(&ClickDebTestSuite{})

var testDebControl = []byte(`Package: foo
Version: 1.0
Architecture: all
Description: some description
`)

func makeTestDeb(c *C, compressor string) string {
	builddir := c.MkDir()

	// debian stuff
	err := os.MkdirAll(filepath.Join(builddir, "DEBIAN"), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(builddir, "DEBIAN", "control"), testDebControl, 0644)
	c.Assert(err, IsNil)

	// some content
	binPath := filepath.Join(builddir, "usr", "bin")
	err = os.MkdirAll(binPath, 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(binPath, "foo"), []byte(""), 0644)
	c.Assert(err, IsNil)

	// build it
	debName := filepath.Join(builddir, "foo_1.0_all.deb")
	cmd := exec.Command("fakeroot", "dpkg-deb", fmt.Sprintf("-Z%s", compressor), "--build", builddir, debName)
	err = cmd.Run()
	c.Assert(err, IsNil)

	return debName
}

func (s *ClickDebTestSuite) TestSnapDebControlContent(c *C) {
	debName := makeTestDeb(c, "gzip")

	d := ClickDeb{Path: debName}
	content, err := d.ControlContent("control")
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, string(testDebControl))
}

func (s *ClickDebTestSuite) TestSnapDebUnpack(c *C) {
	targetDir := c.MkDir()

	for _, comp := range []string{"gzip", "bzip2", "xz"} {
		debName := makeTestDeb(c, comp)
		d := ClickDeb{Path: debName}
		err := d.Unpack(targetDir)
		c.Assert(err, IsNil)
		expectedFile := filepath.Join(targetDir, "usr", "bin", "foo")
		c.Assert(helpers.FileExists(expectedFile), Equals, true)
	}
}

func (s *ClickDebTestSuite) TestClickVerifyContentFnSimple(c *C) {
	newPath, err := clickVerifyContentFn("foo")
	c.Assert(err, IsNil)
	c.Assert(newPath, Equals, "foo")
}

func (s *ClickDebTestSuite) TestClickVerifyContentFnStillOk(c *C) {
	newPath, err := clickVerifyContentFn("./foo/bar/../baz")
	c.Assert(err, IsNil)
	c.Assert(newPath, Equals, "foo/baz")
}

func (s *ClickDebTestSuite) TestClickVerifyContentFnNotOk(c *C) {
	_, err := clickVerifyContentFn("./foo/../../baz")
	c.Assert(err, Equals, ErrSnapInvalidContent)
}
