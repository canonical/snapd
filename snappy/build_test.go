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

package snappy

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"launchpad.net/snappy/helpers"

	. "gopkg.in/check.v1"
)

func makeFakeDuCommand(c *C) string {
	tempdir := c.MkDir()
	duCmdPath := filepath.Join(tempdir, "du")
	fakeDuContent := `#!/bin/sh
echo 17 some-dir`
	err := ioutil.WriteFile(duCmdPath, []byte(fakeDuContent), 0755)
	c.Assert(err, IsNil)

	return duCmdPath
}

func makeExampleSnapSourceDir(c *C, packageYaml string) string {
	tempdir := c.MkDir()

	// use meta/package.yaml
	metaDir := filepath.Join(tempdir, "meta")
	err := os.Mkdir(metaDir, 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(metaDir, "package.yaml"), []byte(packageYaml), 0644)
	c.Assert(err, IsNil)

	// meta/readme.md
	readme := `some title

some description`
	err = ioutil.WriteFile(filepath.Join(metaDir, "readme.md"), []byte(readme), 0644)
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

	// an example symlink
	err = os.Symlink("bin/hello-world", filepath.Join(tempdir, "symlink"))
	c.Assert(err, IsNil)

	return tempdir
}

func (s *SnapTestSuite) TestBuildSimple(c *C) {
	sourceDir := makeExampleSnapSourceDir(c, `name: hello
version: 1.0.1
vendor: Foo <foo@example.com>
architecture: ["i386", "amd64"]
integration:
 app:
  apparmor-profile: meta/hello.apparmor
`)

	resultSnap, err := BuildLegacySnap(sourceDir, "")
	c.Assert(err, IsNil)
	defer os.Remove(resultSnap)

	// check that there is result
	_, err = os.Stat(resultSnap)
	c.Assert(err, IsNil)
	c.Assert(resultSnap, Equals, "hello_1.0.1_multi.snap")

	// check that the json looks valid
	const expectedJSON = `{
 "name": "hello",
 "version": "1.0.1",
 "architecture": [
  "i386",
  "amd64"
 ],
 "framework": "ubuntu-core-15.04-dev1",
 "description": "some description",
 "installed-size": "17",
 "maintainer": "Foo \u003cfoo@example.com\u003e",
 "title": "some title",
 "hooks": {
  "app": {
   "apparmor-profile": "meta/hello.apparmor"
  }
 }
}`
	readJSON, err := exec.Command("dpkg-deb", "-I", "hello_1.0.1_multi.snap", "manifest").Output()
	c.Assert(err, IsNil)
	c.Assert(string(readJSON), Equals, expectedJSON)

	// check that the content looks sane
	readFiles, err := exec.Command("dpkg-deb", "-c", "hello_1.0.1_multi.snap").Output()
	c.Assert(err, IsNil)
	for _, needle := range []string{"./meta/package.yaml", "./meta/readme.md", "./bin/hello-world", "./symlink -> bin/hello-world"} {
		expr := fmt.Sprintf(`(?ms).*%s.*`, regexp.QuoteMeta(needle))
		c.Assert(string(readFiles), Matches, expr)
	}
}

func (s *SnapTestSuite) TestBuildAutoGenerateIntegrationHooksBinaries(c *C) {
	sourceDir := makeExampleSnapSourceDir(c, `name: hello
version: 2.0.1
vendor: Foo <foo@example.com>
architectures:
 - i386
binaries:
 - name: bin/hello-world
`)

	resultSnap, err := BuildLegacySnap(sourceDir, "")
	c.Assert(err, IsNil)
	defer os.Remove(resultSnap)

	// check that there is result
	_, err = os.Stat(resultSnap)
	c.Assert(err, IsNil)
	c.Assert(resultSnap, Equals, "hello_2.0.1_i386.snap")

	// check that the json looks valid
	const expectedJSON = `{
 "name": "hello",
 "version": "2.0.1",
 "architecture": [
  "i386"
 ],
 "framework": "ubuntu-core-15.04-dev1",
 "description": "some description",
 "installed-size": "17",
 "maintainer": "Foo \u003cfoo@example.com\u003e",
 "title": "some title",
 "hooks": {
  "hello-world": {
   "bin-path": "bin/hello-world"
  }
 }
}`
	readJSON, err := exec.Command("dpkg-deb", "-I", "hello_2.0.1_i386.snap", "manifest").Output()
	c.Assert(err, IsNil)
	c.Assert(string(readJSON), Equals, expectedJSON)
}

func (s *SnapTestSuite) TestBuildAutoGenerateIntegrationHooksServices(c *C) {
	sourceDir := makeExampleSnapSourceDir(c, `name: hello
version: 3.0.1
vendor: Foo <foo@example.com>
services:
 - name: foo
   start: bin/hello-world
`)

	resultSnap, err := BuildLegacySnap(sourceDir, "")
	c.Assert(err, IsNil)
	defer os.Remove(resultSnap)

	// check that there is result
	_, err = os.Stat(resultSnap)
	c.Assert(err, IsNil)
	c.Assert(resultSnap, Equals, "hello_3.0.1_all.snap")

	// check that the json looks valid
	const expectedJSON = `{
 "name": "hello",
 "version": "3.0.1",
 "architecture": [
  "all"
 ],
 "framework": "ubuntu-core-15.04-dev1",
 "description": "some description",
 "installed-size": "17",
 "maintainer": "Foo \u003cfoo@example.com\u003e",
 "title": "some title",
 "hooks": {
  "foo": {}
 }
}`
	readJSON, err := exec.Command("dpkg-deb", "-I", "hello_3.0.1_all.snap", "manifest").Output()
	c.Assert(err, IsNil)
	c.Assert(string(readJSON), Equals, expectedJSON)
}

func (s *SnapTestSuite) TestBuildAutoGenerateConfigAppArmor(c *C) {
	sourceDir := makeExampleSnapSourceDir(c, `name: hello
version: 4.0.1
vendor: Foo <foo@example.com>
`)
	hooksDir := filepath.Join(sourceDir, "meta", "hooks")
	os.MkdirAll(hooksDir, 0755)
	err := ioutil.WriteFile(filepath.Join(hooksDir, "config"), []byte(""), 0755)
	c.Assert(err, IsNil)

	resultSnap, err := BuildLegacySnap(sourceDir, "")
	c.Assert(err, IsNil)
	defer os.Remove(resultSnap)

	// check that there is result
	_, err = os.Stat(resultSnap)
	c.Assert(err, IsNil)
	c.Assert(resultSnap, Equals, "hello_4.0.1_all.snap")

	// check that the json looks valid
	const expectedJSON = `{
 "name": "hello",
 "version": "4.0.1",
 "framework": "ubuntu-core-15.04-dev1",
 "description": "fixme-description",
 "installed-size": "17",
 "maintainer": "Foo \u003cfoo@example.com\u003e",
 "title": "some title",
 "hooks": {
  "snappy-config": {
   "apparmor": "meta/snappy-config.apparmor",
  }
 }
}`
}

func (s *SnapTestSuite) TestBuildNoManifestFails(c *C) {
	sourceDir := makeExampleSnapSourceDir(c, "")
	c.Assert(os.Remove(filepath.Join(sourceDir, "meta", "package.yaml")), IsNil)
	_, err := BuildLegacySnap(sourceDir, "")
	c.Assert(err, NotNil) // XXX maybe make the error more explicit
}

func (s *SnapTestSuite) TestBuildManifestRequiresMissingLicense(c *C) {
	sourceDir := makeExampleSnapSourceDir(c, `name: hello
version: 1.0.1
vendor: Foo <foo@example.com>
architecture: ["i386", "amd64"]
integration:
 app:
  apparmor-profile: meta/hello.apparmor
explicit-license-agreement: Y
`)
	_, err := BuildLegacySnap(sourceDir, "")
	c.Assert(err, NotNil) // XXX maybe make the error more explicit
}

func (s *SnapTestSuite) TestBuildManifestRequiresBlankLicense(c *C) {
	sourceDir := makeExampleSnapSourceDir(c, `name: hello
version: 1.0.1
vendor: Foo <foo@example.com>
architecture: ["i386", "amd64"]
integration:
 app:
  apparmor-profile: meta/hello.apparmor
explicit-license-agreement: Y
`)
	lic := filepath.Join(sourceDir, "meta", "license.txt")
	ioutil.WriteFile(lic, []byte("\n"), 0755)
	_, err := BuildLegacySnap(sourceDir, "")
	c.Assert(err, Equals, ErrLicenseBlank)
}

func (s *SnapTestSuite) TestCopyCopies(c *C) {
	sourceDir := makeExampleSnapSourceDir(c, "name: hello")
	// actually this'll be on /tmp so it'll be a link
	target := c.MkDir()
	c.Assert(copyToBuildDir(sourceDir, target), IsNil)
	out, err := exec.Command("diff", "-qrN", sourceDir, target).Output()
	c.Check(err, IsNil)
	c.Check(out, DeepEquals, []byte{})
}

func (s *SnapTestSuite) TestCopyActuallyCopies(c *C) {
	sourceDir := makeExampleSnapSourceDir(c, "name: hello")

	// hoping to get the non-linking behaviour via /dev/shm
	target, err := ioutil.TempDir("/dev/shm", "copy")
	// sbuild environments won't allow writing to /dev/shm, so its
	// ok to skip there
	if os.IsPermission(err) {
		c.Skip("/dev/shm is not writable for us")
	}
	c.Assert(err, IsNil)

	defer os.Remove(target)
	c.Assert(copyToBuildDir(sourceDir, target), IsNil)
	out, err := exec.Command("diff", "-qrN", sourceDir, target).Output()
	c.Check(err, IsNil)
	c.Check(out, DeepEquals, []byte{})
}

func (s *SnapTestSuite) TestCopyExcludesBackups(c *C) {
	sourceDir := makeExampleSnapSourceDir(c, "name: hello")
	target := c.MkDir()
	// add a backup file
	c.Assert(ioutil.WriteFile(filepath.Join(sourceDir, "foo~"), []byte("hi"), 0755), IsNil)
	c.Assert(copyToBuildDir(sourceDir, target), IsNil)
	cmd := exec.Command("diff", "-qr", sourceDir, target)
	cmd.Env = append(cmd.Env, "LANG=C")
	out, err := cmd.Output()
	c.Check(err, NotNil)
	c.Check(string(out), Matches, `(?m)Only in \S+: foo~`)
}

func (s *SnapTestSuite) TestCopyExcludesTopLevelDEBIAN(c *C) {
	sourceDir := makeExampleSnapSourceDir(c, "name: hello")
	target := c.MkDir()
	// add a toplevel DEBIAN
	c.Assert(os.MkdirAll(filepath.Join(sourceDir, "DEBIAN", "foo"), 0755), IsNil)
	// and a non-toplevel DEBIAN
	c.Assert(os.MkdirAll(filepath.Join(sourceDir, "bar", "DEBIAN", "baz"), 0755), IsNil)
	c.Assert(copyToBuildDir(sourceDir, target), IsNil)
	cmd := exec.Command("diff", "-qr", sourceDir, target)
	cmd.Env = append(cmd.Env, "LANG=C")
	out, err := cmd.Output()
	c.Check(err, NotNil)
	c.Check(string(out), Matches, `(?m)Only in \S+: DEBIAN`)
	// but *only one* DEBIAN is skipped
	c.Check(strings.Count(string(out), "Only in"), Equals, 1)
}

func (s *SnapTestSuite) TestCopyExcludesWholeDirs(c *C) {
	sourceDir := makeExampleSnapSourceDir(c, "name: hello")
	target := c.MkDir()
	// add a file inside a skipped dir
	c.Assert(os.Mkdir(filepath.Join(sourceDir, ".bzr"), 0755), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(sourceDir, ".bzr", "foo"), []byte("hi"), 0755), IsNil)
	c.Assert(copyToBuildDir(sourceDir, target), IsNil)
	out, _ := exec.Command("find", sourceDir).Output()
	cmd := exec.Command("diff", "-qr", sourceDir, target)
	cmd.Env = append(cmd.Env, "LANG=C")
	out, err := cmd.Output()
	c.Check(err, NotNil)
	c.Check(string(out), Matches, `(?m)Only in \S+: \.bzr`)
}

func (s *SnapTestSuite) TestExcludeDynamicFalseIfNoSnapignore(c *C) {
	basedir := c.MkDir()
	c.Check(shouldExcludeDynamic(basedir, "foo"), Equals, false)
}

func (s *SnapTestSuite) TestExcludeDynamicWorksIfSnapignore(c *C) {
	basedir := c.MkDir()
	c.Assert(ioutil.WriteFile(filepath.Join(basedir, ".snapignore"), []byte("foo\nb.r\n"), 0644), IsNil)
	c.Check(shouldExcludeDynamic(basedir, "foo"), Equals, true)
	c.Check(shouldExcludeDynamic(basedir, "bar"), Equals, true)
	c.Check(shouldExcludeDynamic(basedir, "bzr"), Equals, true)
	c.Check(shouldExcludeDynamic(basedir, "baz"), Equals, false)
}

func (s *SnapTestSuite) TestExcludeDynamicWeirdRegexps(c *C) {
	basedir := c.MkDir()
	c.Assert(ioutil.WriteFile(filepath.Join(basedir, ".snapignore"), []byte("*hello\n"), 0644), IsNil)
	// note “*hello” is not a valid regexp, so will be taken literally (not globbed!)
	c.Check(shouldExcludeDynamic(basedir, "ahello"), Equals, false)
	c.Check(shouldExcludeDynamic(basedir, "*hello"), Equals, true)
}

func (s *SnapTestSuite) TestBuildSimpleOutputDir(c *C) {
	sourceDir := makeExampleSnapSourceDir(c, `name: hello
version: 1.0.1
vendor: Foo <foo@example.com>
architecture: ["i386", "amd64"]
integration:
 app:
  apparmor-profile: meta/hello.apparmor
`)

	outputDir := filepath.Join(c.MkDir(), "output")
	snapOutput := filepath.Join(outputDir, "hello_1.0.1_multi.snap")
	resultSnap, err := BuildLegacySnap(sourceDir, outputDir)
	c.Assert(err, IsNil)

	// check that there is result
	_, err = os.Stat(resultSnap)
	c.Assert(err, IsNil)
	c.Assert(resultSnap, Equals, snapOutput)

	// check that the json looks valid
	const expectedJSON = `{
 "name": "hello",
 "version": "1.0.1",
 "architecture": [
  "i386",
  "amd64"
 ],
 "framework": "ubuntu-core-15.04-dev1",
 "description": "some description",
 "installed-size": "17",
 "maintainer": "Foo \u003cfoo@example.com\u003e",
 "title": "some title",
 "hooks": {
  "app": {
   "apparmor-profile": "meta/hello.apparmor"
  }
 }
}`
	readJSON, err := exec.Command("dpkg-deb", "-I", snapOutput, "manifest").Output()
	c.Assert(err, IsNil)
	c.Assert(string(readJSON), Equals, expectedJSON)

	// check that the content looks sane
	readFiles, err := exec.Command("dpkg-deb", "-c", snapOutput).Output()
	c.Assert(err, IsNil)
	for _, needle := range []string{"./meta/package.yaml", "./meta/readme.md", "./bin/hello-world"} {
		expr := fmt.Sprintf(`(?ms).*%s.*`, regexp.QuoteMeta(needle))
		c.Assert(string(readFiles), Matches, expr)
	}
}

func (s *SnapTestSuite) TestBuildChecksForClashes(c *C) {
	sourceDir := makeExampleSnapSourceDir(c, `name: hello
version: 1.0.1
vendor: Foo <foo@example.com>
services:
 - name: foo
binaries:
 - name: foo
`)
	_, err := BuildLegacySnap(sourceDir, "")
	c.Assert(err, ErrorMatches, ".*binary and service both called foo.*")
}

func (s *SnapTestSuite) TestDebArchitecture(c *C) {
	c.Check(debArchitecture(&packageYaml{Architectures: []string{"foo"}}), Equals, "foo")
	c.Check(debArchitecture(&packageYaml{Architectures: []string{"foo", "bar"}}), Equals, "multi")
	c.Check(debArchitecture(&packageYaml{Architectures: nil}), Equals, "unknown")
}

func (s *SnapTestSuite) TestHashForFileForDevice(c *C) {
	// skip test
	if !helpers.FileExists("/dev/kmsg") {
		c.Skip("no /dev/kmsg")
	}

	stat, err := os.Stat("/dev/kmsg")
	c.Assert(err, IsNil)
	h, err := hashForFile("", "/dev/kmsg", stat)
	c.Assert(err, IsNil)
	c.Assert(h.Name, Equals, "/dev/kmsg")
	c.Assert(h.Device, Equals, "1,11")
	c.Assert(h.Sha512, Equals, "")
}

func (s *SnapTestSuite) TestBuildAllPermissions(c *C) {
	sourceDir := makeExampleSnapSourceDir(c, `name: hello
version: 1.0.1
vendor: Foo <foo@example.com>
`)

	resultSnap, err := BuildLegacySnap(sourceDir, "")
	c.Assert(err, IsNil)
	defer os.Remove(resultSnap)

	// check that the content looks sane
	readFiles, err := exec.Command("dpkg-deb", "-c", "hello_1.0.1_all.snap").CombinedOutput()
	c.Assert(err, IsNil)

	// check that we really have the right perms
	c.Assert(strings.Contains(string(readFiles), `drwxrwxrwx`), Equals, true)
	c.Assert(strings.Contains(string(readFiles), `-rw-rw-rw-`), Equals, true)
}

func (s *SnapTestSuite) TestBuildFailsForUnknownType(c *C) {
	sourceDir := makeExampleSnapSourceDir(c, `name: hello
version: 1.0.1
vendor: Foo <foo@example.com>
`)
	err := syscall.Mkfifo(filepath.Join(sourceDir, "fifo"), 0644)
	c.Assert(err, IsNil)

	_, err = BuildLegacySnap(sourceDir, "")
	c.Assert(err, ErrorMatches, "can not handle type of file .*")
}

func (s *SnapTestSuite) TestBuildSnapfsSimple(c *C) {
	sourceDir := makeExampleSnapSourceDir(c, `name: hello
version: 1.0.1
vendor: Foo <foo@example.com>
architecture: ["i386", "amd64"]
integration:
 app:
  apparmor-profile: meta/hello.apparmor
`)

	resultSnap, err := BuildSnapfsSnap(sourceDir, "")
	c.Assert(err, IsNil)
	defer os.Remove(resultSnap)

	// check that there is result
	_, err = os.Stat(resultSnap)
	c.Assert(err, IsNil)
	c.Assert(resultSnap, Equals, "hello_1.0.1_multi.snap")

	// check that the content looks sane
	output, err := exec.Command("unsquashfs", "-ll", "hello_1.0.1_multi.snap").CombinedOutput()
	c.Assert(err, IsNil)
	for _, needle := range []string{
		"meta/package.yaml",
		"meta/readme.md",
		"bin/hello-world",
		"symlink -> bin/hello-world",
	} {
		expr := fmt.Sprintf(`(?ms).*%s.*`, regexp.QuoteMeta(needle))
		c.Assert(string(output), Matches, expr)
	}
}
