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

package squashfs

import (
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/testutil"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type SquashfsTestSuite struct {
}

var _ = Suite(&SquashfsTestSuite{})

func makeSnap(c *C, manifest, data string) *Snap {
	tmp := c.MkDir()
	err := os.MkdirAll(filepath.Join(tmp, "meta", "hooks", "dir"), 0755)
	c.Assert(err, IsNil)

	// our regular snap.yaml
	err = ioutil.WriteFile(filepath.Join(tmp, "meta", "snap.yaml"), []byte(manifest), 0644)
	c.Assert(err, IsNil)

	// some hooks
	err = ioutil.WriteFile(filepath.Join(tmp, "meta", "hooks", "foo-hook"), nil, 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(tmp, "meta", "hooks", "bar-hook"), nil, 0755)
	c.Assert(err, IsNil)
	// And a file in another directory in there, just for testing (not a valid
	// hook)
	err = ioutil.WriteFile(filepath.Join(tmp, "meta", "hooks", "dir", "baz"), nil, 0755)
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
	os.Setenv("SNAPPY_SKIP_CHATTR_FOR_TESTS", "1")
	err := os.Chdir(c.MkDir())
	c.Assert(err, IsNil)
}

func (s *SquashfsTestSuite) TearDownTest(c *C) {
	os.Unsetenv("SNAPPY_SKIP_CHATTR_FOR_TESTS")
}

func (s *SquashfsTestSuite) TestInstallSimple(c *C) {
	snap := makeSnap(c, "name: test", "")
	targetPath := filepath.Join(c.MkDir(), "target.snap")
	mountDir := c.MkDir()
	err := snap.Install(targetPath, mountDir)
	c.Assert(err, IsNil)
	c.Check(osutil.FileExists(targetPath), Equals, true)
}

func (s *SquashfsTestSuite) TestInstallNotCopyTwice(c *C) {
	snap := makeSnap(c, "name: test2", "")
	targetPath := filepath.Join(c.MkDir(), "target.snap")
	mountDir := c.MkDir()
	err := snap.Install(targetPath, mountDir)
	c.Assert(err, IsNil)

	cmd := testutil.MockCommand(c, "cp", "")
	defer cmd.Restore()
	err = snap.Install(targetPath, mountDir)
	c.Assert(err, IsNil)
	c.Assert(cmd.Calls(), HasLen, 0)
}

func (s *SquashfsTestSuite) TestPath(c *C) {
	p := "/path/to/foo.snap"
	snap := New("/path/to/foo.snap")
	c.Assert(snap.Path(), Equals, p)
}

func (s *SquashfsTestSuite) TestReadFile(c *C) {
	snap := makeSnap(c, "name: foo", "")

	content, err := snap.ReadFile("meta/snap.yaml")
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, "name: foo")
}

func (s *SquashfsTestSuite) TestListDir(c *C) {
	snap := makeSnap(c, "name: foo", "")

	fileNames, err := snap.ListDir("meta/hooks")
	c.Assert(err, IsNil)
	c.Assert(len(fileNames), Equals, 3)
	c.Check(fileNames[0], Equals, "bar-hook")
	c.Check(fileNames[1], Equals, "dir")
	c.Check(fileNames[2], Equals, "foo-hook")
}

// TestUnpackGlob tests the internal unpack
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
	c.Assert(osutil.FileExists(filepath.Join(outputDir, "meta/snap.yaml")), Equals, false)
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
