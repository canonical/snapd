// -*- Mode: Go; indent-tabs-mode: t -*-

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

package snapfs

import (
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"launchpad.net/snappy/helpers"

	. "gopkg.in/check.v1"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type SquashfsTestSuite struct {
}

var _ = Suite(&SquashfsTestSuite{})

func makeSnap(c *C, manifest, data string) *Snap {
	tmp := c.MkDir()
	err := os.MkdirAll(filepath.Join(tmp, "meta"), 0755)

	// our regular package yaml
	err = ioutil.WriteFile(filepath.Join(tmp, "meta", "package.yaml"), []byte(manifest), 0644)
	c.Assert(err, IsNil)

	// for click compat
	err = os.MkdirAll(filepath.Join(tmp, ".click"), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(tmp, ".click", "manifest"), []byte(manifest), 0644)
	c.Assert(err, IsNil)

	// some data
	err = ioutil.WriteFile(filepath.Join(tmp, "data.bin"), []byte(data), 0644)
	c.Assert(err, IsNil)

	// build it
	cur, _ := os.Getwd()
	snap := New(filepath.Join(cur, "foo.snap"))
	err = snap.Build(tmp)
	c.Assert(err, IsNil)

	return snap
}

func (s *SquashfsTestSuite) SetUpTest(c *C) {
	err := os.Chdir(c.MkDir())
	c.Assert(err, IsNil)
}

func (s *SquashfsTestSuite) TestName(c *C) {
	snap := New("/path/to/foo.snap")
	c.Assert(snap.Name(), Equals, "foo.snap")
}

// FIXME: stub that needs to be fleshed out once assertions land
//        and we actually do verify
func (s *SquashfsTestSuite) TestVerify(c *C) {
	err := New("foo").Verify(false)
	c.Assert(err, IsNil)
}

func (s *SquashfsTestSuite) TestReadFile(c *C) {
	snap := makeSnap(c, "name: foo", "")

	content, err := snap.ReadFile("meta/package.yaml")
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, "name: foo")
}

func (s *SquashfsTestSuite) TestUnpackGlob(c *C) {
	data := "some random data"
	snap := makeSnap(c, "", data)

	outputDir := c.MkDir()
	err := snap.Unpack("data*", outputDir)
	c.Assert(err, IsNil)

	// this is the file we expect
	content, err := ioutil.ReadFile(filepath.Join(outputDir, "data.bin"))
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, data)

	// ensure glob was honored
	c.Assert(helpers.FileExists(filepath.Join(outputDir, "meta/package.yaml")), Equals, false)
}

func (s *SquashfsTestSuite) TestUnpackMeta(c *C) {
	snap := makeSnap(c, "", "random-data")

	outputDir := c.MkDir()
	err := snap.UnpackMeta(outputDir)
	c.Assert(err, IsNil)

	// we got the meta/ stuff
	c.Assert(helpers.FileExists(filepath.Join(outputDir, "meta/package.yaml")), Equals, true)
	// ... but not the data
	c.Assert(helpers.FileExists(filepath.Join(outputDir, "data.bin")), Equals, false)
}

func (s *SquashfsTestSuite) TestBuild(c *C) {
	buildDir := c.MkDir()
	err := os.MkdirAll(filepath.Join(buildDir, "/random/dir"), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(buildDir, "data.bin"), []byte("data"), 0644)
	c.Assert(err, IsNil)

	snap := New(filepath.Join(c.MkDir(), "foo.snap"))
	err = snap.Build(buildDir)
	c.Assert(err, IsNil)

	// unsquashfs writes a funny header like:
	//     "Parallel unsquashfs: Using 1 processor"
	//     "1 inodes (1 blocks) to write"
	outputWithHeader, err := exec.Command("unsquashfs", "-n", "-l", snap.path).Output()
	c.Assert(err, IsNil)
	split := strings.Split(string(outputWithHeader), "\n")
	output := strings.Join(split[3:], "\n")
	c.Assert(string(output), Equals, `squashfs-root
squashfs-root/data.bin
squashfs-root/random
squashfs-root/random/dir
`)
}

func (s *SquashfsTestSuite) TestRunCommandGood(c *C) {
	err := runCommand("true")
	c.Assert(err, IsNil)
}

func (s *SquashfsTestSuite) TestRunCommandBad(c *C) {
	err := runCommand("false")
	c.Assert(err, ErrorMatches, regexp.QuoteMeta(`cmd: "false" failed: exit status 1 ("")`))
}

func (s *SquashfsTestSuite) TestRunCommandUgly(c *C) {
	err := runCommand("cat", "/no/such/file")
	c.Assert(err, ErrorMatches, regexp.QuoteMeta(`cmd: "cat /no/such/file" failed: exit status 1 ("cat: /no/such/file: No such file or directory\n")`))
}
