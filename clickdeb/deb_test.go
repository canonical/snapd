/*
 * Copyright (C) 2014-2015 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */
package clickdeb

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
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

func makeTestDebDir(c *C) string {
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
	err = ioutil.WriteFile(filepath.Join(binPath, "foo"), []byte("foo"), 0644)
	c.Assert(err, IsNil)

	return builddir
}

func makeTestDeb(c *C, compressor string) string {
	builddir := makeTestDebDir(c)

	// build it
	debName := filepath.Join(builddir, "foo_1.0_all.deb")
	cmd := exec.Command("fakeroot", "dpkg-deb", fmt.Sprintf("-Z%s", compressor), "--build", builddir, debName)
	err := cmd.Run()
	c.Assert(err, IsNil)

	return debName
}

func (s *ClickDebTestSuite) TestSnapDebBuild(c *C) {
	builddir := makeTestDebDir(c)

	debDir := c.MkDir()
	d := ClickDeb{Path: filepath.Join(debDir, "foo_1.0_all.deb")}
	err := d.Build(builddir)
	c.Assert(err, IsNil)
	c.Assert(helpers.FileExists(d.Path), Equals, true)

	// control
	cmd := exec.Command("dpkg-deb", "-I", d.Path)
	output, err := cmd.CombinedOutput()
	c.Assert(err, IsNil)
	c.Assert(strings.Contains(string(output), "Package: foo\n"), Equals, true)

	// data
	cmd = exec.Command("dpkg-deb", "-c", d.Path)
	output, err = cmd.CombinedOutput()
	c.Assert(err, IsNil)
	c.Assert(strings.Contains(string(output), "./usr/bin/foo"), Equals, true)
	c.Assert(strings.Contains(string(output), "DEBIAN"), Equals, false)
}

func (s *ClickDebTestSuite) TestSnapDebControlMember(c *C) {
	debName := makeTestDeb(c, "gzip")

	d := ClickDeb{Path: debName}
	content, err := d.ControlMember("control")
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

func (s *ClickDebTestSuite) TestTarCreate(c *C) {
	// setup
	builddir := c.MkDir()
	err := os.MkdirAll(filepath.Join(builddir, "etc"), 0700)
	c.Assert(err, IsNil)

	err = ioutil.WriteFile(filepath.Join(builddir, "foo"), []byte("foo"), 0644)
	c.Assert(err, IsNil)

	err = ioutil.WriteFile(filepath.Join(builddir, "exclude-me"), []byte("me"), 0644)
	c.Assert(err, IsNil)

	err = os.Symlink("foo", filepath.Join(builddir, "link-to-foo"))
	c.Assert(err, IsNil)

	// create tar
	tempdir := c.MkDir()
	tarfile := filepath.Join(tempdir, "data.tar.gz")
	err = tarGzCreate(tarfile, builddir, func(path string) bool {
		return !strings.HasSuffix(path, "exclude-me")
	})
	c.Assert(err, IsNil)

	// verify
	output, err := exec.Command("tar", "tvf", tarfile).CombinedOutput()
	c.Assert(err, IsNil)

	// exclusion works
	c.Assert(strings.Contains(string(output), "exclude-me"), Equals, false)

	// we got the expected content for the file
	r, err := regexp.Compile("-rw-r--r--[ ]+root/root[ ]+3[ ]+(.*)./foo")
	c.Assert(err, IsNil)
	c.Assert(r.Match(output), Equals, true)

	// and for the dir
	r, err = regexp.Compile("drwx------[ ]+root/root[ ]+0[ ]+(.*)./etc")
	c.Assert(err, IsNil)
	c.Assert(r.Match(output), Equals, true)

	// and for the symlink
	r, err = regexp.Compile("lrwxrwxrwx[ ]+root/root[ ]+0[ ]+(.*)./link-to-foo -> foo")
	c.Assert(err, IsNil)
	c.Assert(r.Match(output), Equals, true)
}
