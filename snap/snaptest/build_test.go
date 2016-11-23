// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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

package snaptest_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"

	. "gopkg.in/check.v1"
)

type BuildTestSuite struct {
	testutil.BaseTest
}

var _ = Suite(&BuildTestSuite{})

func (s *BuildTestSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	// chdir into a tempdir
	pwd, err := os.Getwd()
	c.Assert(err, IsNil)
	s.AddCleanup(func() { os.Chdir(pwd) })
	err = os.Chdir(c.MkDir())
	c.Assert(err, IsNil)

	// use fake root
	dirs.SetRootDir(c.MkDir())
}

func makeExampleSnapSourceDir(c *C, snapYamlContent string) string {
	tempdir := c.MkDir()

	// use meta/snap.yaml
	metaDir := filepath.Join(tempdir, "meta")
	err := os.Mkdir(metaDir, 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(metaDir, "snap.yaml"), []byte(snapYamlContent), 0644)
	c.Assert(err, IsNil)

	const helloBinContent = `#!/bin/sh
printf "hello world"
`

	// an example binary
	binDir := filepath.Join(tempdir, "bin")
	err = os.Mkdir(binDir, 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(binDir, "hello-world"), []byte(helloBinContent), 0755)
	c.Assert(err, IsNil)

	// unusual permissions for dir
	tmpDir := filepath.Join(tempdir, "tmp")
	err = os.Mkdir(tmpDir, 0755)
	c.Assert(err, IsNil)
	// avoid umask
	err = os.Chmod(tmpDir, 01777)
	c.Assert(err, IsNil)

	// and file
	someFile := filepath.Join(tempdir, "file-with-perm")
	err = ioutil.WriteFile(someFile, []byte(""), 0666)
	c.Assert(err, IsNil)
	err = os.Chmod(someFile, 0666)
	c.Assert(err, IsNil)

	// an example symlink
	err = os.Symlink("bin/hello-world", filepath.Join(tempdir, "symlink"))
	c.Assert(err, IsNil)

	return tempdir
}

func (s *BuildTestSuite) TestBuildNoManifestFails(c *C) {
	sourceDir := makeExampleSnapSourceDir(c, "")
	c.Assert(os.Remove(filepath.Join(sourceDir, "meta", "snap.yaml")), IsNil)
	_, err := snaptest.BuildSquashfsSnap(sourceDir, "")
	c.Assert(err, NotNil) // XXX maybe make the error more explicit
}

func (s *BuildTestSuite) TestCopyCopies(c *C) {
	sourceDir := makeExampleSnapSourceDir(c, "name: hello")
	// actually this'll be on /tmp so it'll be a link
	target := c.MkDir()
	c.Assert(snaptest.CopyToBuildDir(sourceDir, target), IsNil)
	out, err := exec.Command("diff", "-qrN", sourceDir, target).Output()
	c.Check(err, IsNil)
	c.Check(out, DeepEquals, []byte{})
}

func (s *BuildTestSuite) TestCopyActuallyCopies(c *C) {
	sourceDir := makeExampleSnapSourceDir(c, "name: hello")

	// hoping to get the non-linking behaviour via /dev/shm
	target, err := ioutil.TempDir("/dev/shm", "copy")
	// sbuild environments won't allow writing to /dev/shm, so its
	// ok to skip there
	if os.IsPermission(err) {
		c.Skip("/dev/shm is not writable for us")
	}
	c.Assert(err, IsNil)

	c.Assert(snaptest.CopyToBuildDir(sourceDir, target), IsNil)
	out, err := exec.Command("diff", "-qrN", sourceDir, target).Output()
	c.Check(err, IsNil)
	c.Check(out, DeepEquals, []byte{})
}

func (s *BuildTestSuite) TestCopyExcludesBackups(c *C) {
	sourceDir := makeExampleSnapSourceDir(c, "name: hello")
	target := c.MkDir()
	// add a backup file
	c.Assert(ioutil.WriteFile(filepath.Join(sourceDir, "foo~"), []byte("hi"), 0755), IsNil)
	c.Assert(snaptest.CopyToBuildDir(sourceDir, target), IsNil)
	cmd := exec.Command("diff", "-qr", sourceDir, target)
	cmd.Env = append(cmd.Env, "LANG=C")
	out, err := cmd.Output()
	c.Check(err, NotNil)
	c.Check(string(out), Matches, `(?m)Only in \S+: foo~`)
}

func (s *BuildTestSuite) TestCopyExcludesTopLevelDEBIAN(c *C) {
	sourceDir := makeExampleSnapSourceDir(c, "name: hello")
	target := c.MkDir()
	// add a toplevel DEBIAN
	c.Assert(os.MkdirAll(filepath.Join(sourceDir, "DEBIAN", "foo"), 0755), IsNil)
	// and a non-toplevel DEBIAN
	c.Assert(os.MkdirAll(filepath.Join(sourceDir, "bar", "DEBIAN", "baz"), 0755), IsNil)
	c.Assert(snaptest.CopyToBuildDir(sourceDir, target), IsNil)
	cmd := exec.Command("diff", "-qr", sourceDir, target)
	cmd.Env = append(cmd.Env, "LANG=C")
	out, err := cmd.Output()
	c.Check(err, NotNil)
	c.Check(string(out), Matches, `(?m)Only in \S+: DEBIAN`)
	// but *only one* DEBIAN is skipped
	c.Check(strings.Count(string(out), "Only in"), Equals, 1)
}

func (s *BuildTestSuite) TestCopyExcludesWholeDirs(c *C) {
	sourceDir := makeExampleSnapSourceDir(c, "name: hello")
	target := c.MkDir()
	// add a file inside a skipped dir
	c.Assert(os.Mkdir(filepath.Join(sourceDir, ".bzr"), 0755), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(sourceDir, ".bzr", "foo"), []byte("hi"), 0755), IsNil)
	c.Assert(snaptest.CopyToBuildDir(sourceDir, target), IsNil)
	out, _ := exec.Command("find", sourceDir).Output()
	c.Check(string(out), Not(Equals), "")
	cmd := exec.Command("diff", "-qr", sourceDir, target)
	cmd.Env = append(cmd.Env, "LANG=C")
	out, err := cmd.Output()
	c.Check(err, NotNil)
	c.Check(string(out), Matches, `(?m)Only in \S+: \.bzr`)
}

func (s *BuildTestSuite) TestExcludeDynamicFalseIfNoSnapignore(c *C) {
	basedir := c.MkDir()
	c.Check(snaptest.ShouldExcludeDynamic(basedir, "foo"), Equals, false)
}

func (s *BuildTestSuite) TestExcludeDynamicWorksIfSnapignore(c *C) {
	basedir := c.MkDir()
	c.Assert(ioutil.WriteFile(filepath.Join(basedir, ".snapignore"), []byte("foo\nb.r\n"), 0644), IsNil)
	c.Check(snaptest.ShouldExcludeDynamic(basedir, "foo"), Equals, true)
	c.Check(snaptest.ShouldExcludeDynamic(basedir, "bar"), Equals, true)
	c.Check(snaptest.ShouldExcludeDynamic(basedir, "bzr"), Equals, true)
	c.Check(snaptest.ShouldExcludeDynamic(basedir, "baz"), Equals, false)
}

func (s *BuildTestSuite) TestExcludeDynamicWeirdRegexps(c *C) {
	basedir := c.MkDir()
	c.Assert(ioutil.WriteFile(filepath.Join(basedir, ".snapignore"), []byte("*hello\n"), 0644), IsNil)
	// note "*hello" is not a valid regexp, so will be taken literally (not globbed!)
	c.Check(snaptest.ShouldExcludeDynamic(basedir, "ahello"), Equals, false)
	c.Check(snaptest.ShouldExcludeDynamic(basedir, "*hello"), Equals, true)
}

func (s *BuildTestSuite) TestDebArchitecture(c *C) {
	c.Check(snaptest.DebArchitecture(&snap.Info{Architectures: []string{"foo"}}), Equals, "foo")
	c.Check(snaptest.DebArchitecture(&snap.Info{Architectures: []string{"foo", "bar"}}), Equals, "multi")
	c.Check(snaptest.DebArchitecture(&snap.Info{Architectures: nil}), Equals, "unknown")
}

func (s *BuildTestSuite) TestBuildFailsForUnknownType(c *C) {
	sourceDir := makeExampleSnapSourceDir(c, `name: hello
version: 1.0.1
`)
	err := syscall.Mkfifo(filepath.Join(sourceDir, "fifo"), 0644)
	c.Assert(err, IsNil)

	_, err = snaptest.BuildSquashfsSnap(sourceDir, "")
	c.Assert(err, ErrorMatches, "cannot handle type of file .*")
}

func (s *BuildTestSuite) TestBuildSquashfsSimple(c *C) {
	sourceDir := makeExampleSnapSourceDir(c, `name: hello
version: 1.0.1
architectures: ["i386", "amd64"]
integration:
 app:
  apparmor-profile: meta/hello.apparmor
`)

	resultSnap, err := snaptest.BuildSquashfsSnap(sourceDir, "")
	c.Assert(err, IsNil)

	// check that there is result
	_, err = os.Stat(resultSnap)
	c.Assert(err, IsNil)
	c.Assert(resultSnap, Equals, "hello_1.0.1_multi.snap")

	// check that the content looks sane
	output, err := exec.Command("unsquashfs", "-ll", "hello_1.0.1_multi.snap").CombinedOutput()
	c.Assert(err, IsNil)
	for _, needle := range []string{
		"meta/snap.yaml",
		"bin/hello-world",
		"symlink -> bin/hello-world",
	} {
		expr := fmt.Sprintf(`(?ms).*%s.*`, regexp.QuoteMeta(needle))
		c.Assert(string(output), Matches, expr)
	}
}

func (s *BuildTestSuite) TestBuildSimpleOutputDir(c *C) {
	sourceDir := makeExampleSnapSourceDir(c, `name: hello
version: 1.0.1
architectures: ["i386", "amd64"]
integration:
 app:
  apparmor-profile: meta/hello.apparmor
`)

	outputDir := filepath.Join(c.MkDir(), "output")
	snapOutput := filepath.Join(outputDir, "hello_1.0.1_multi.snap")
	resultSnap, err := snaptest.BuildSquashfsSnap(sourceDir, outputDir)
	c.Assert(err, IsNil)

	// check that there is result
	_, err = os.Stat(resultSnap)
	c.Assert(err, IsNil)
	c.Assert(resultSnap, Equals, snapOutput)

	// check that the content looks sane
	output, err := exec.Command("unsquashfs", "-ll", resultSnap).CombinedOutput()
	c.Assert(err, IsNil)
	for _, needle := range []string{
		"meta/snap.yaml",
		"bin/hello-world",
		"symlink -> bin/hello-world",
	} {
		expr := fmt.Sprintf(`(?ms).*%s.*`, regexp.QuoteMeta(needle))
		c.Assert(string(output), Matches, expr)
	}
}
