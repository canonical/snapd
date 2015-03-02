package snappy

import (
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	. "launchpad.net/gocheck"
)

func makeExampleSnapSourceDir(c *C, packageYaml string) string {
	tempdir := c.MkDir()

	// use meta/package.yaml
	metaDir := filepath.Join(tempdir, "meta")
	err := os.Mkdir(metaDir, 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(metaDir, "package.yaml"), []byte(packageYaml), 0644)
	c.Assert(err, IsNil)

	// meta/readme.md
	readme := `some text

with some more text`
	err = ioutil.WriteFile(filepath.Join(metaDir, "readme.md"), []byte(readme), 0644)
	c.Assert(err, IsNil)

	const helloBinContent = `#!/bin/sh
printf "hello world"
`

	// a example binary
	binDir := filepath.Join(tempdir, "bin")
	err = os.Mkdir(binDir, 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(binDir, "hello-world"), []byte(helloBinContent), 0755)
	c.Assert(err, IsNil)

	return tempdir
}

func (s *SnapTestSuite) TestBuildSimple(c *C) {
	sourceDir := makeExampleSnapSourceDir(c, `name: hello
version: 1.0.1
vendor: Foo <foo@example.com>
architecture: all
integration:
 app:
  apparmor-profile: meta/hello.apparmor
`)

	resultSnap, err := Build(sourceDir)
	c.Assert(err, IsNil)
	defer os.Remove(resultSnap)

	// check that there is result
	_, err = os.Stat(resultSnap)
	c.Assert(err, IsNil)
	c.Assert(resultSnap, Equals, "hello_1.0.1_all.snap")

	// check that the json looks valid
	const expectedJSON = `{
 "name": "hello",
 "version": "1.0.1",
 "description": "fixme-description",
 "installed-size": "fixme-999",
 "title": "fixme-title",
 "hooks": {
  "app": {
   "apparmor-profile": "meta/hello.apparmor"
  }
 }
}`
	readJSON, err := exec.Command("dpkg-deb", "-I", "hello_1.0.1_all.snap", "manifest").Output()
	c.Assert(err, IsNil)
	c.Assert(string(readJSON), Equals, expectedJSON)

	// check that the content looks sane
	readFiles, err := exec.Command("dpkg-deb", "-c", "hello_1.0.1_all.snap").Output()
	c.Assert(err, IsNil)
	for _, needle := range []string{"./meta/package.yaml", "./meta/readme.md", "./bin/hello-world"} {
		c.Assert(strings.Contains(string(readFiles), needle), Equals, true)
	}
}

func (s *SnapTestSuite) TestBuildAutoGenerateIntegrationHooksBinaries(c *C) {
	sourceDir := makeExampleSnapSourceDir(c, `name: hello
version: 2.0.1
vendor: Foo <foo@example.com>
binaries:
 app:
  name: bin/hello-world
`)

	resultSnap, err := Build(sourceDir)
	c.Assert(err, IsNil)
	defer os.Remove(resultSnap)

	// check that there is result
	_, err = os.Stat(resultSnap)
	c.Assert(err, IsNil)
	c.Assert(resultSnap, Equals, "hello_2.0.1_all.snap")

	// check that the json looks valid
	const expectedJSON = `{
 "name": "hello",
 "version": "2.0.1",
 "description": "fixme-description",
 "installed-size": "fixme-999",
 "title": "fixme-title",
 "hooks": {
  "app": {
   "apparmor": "meta/hello-world.apparmor",
   "bin-path": "bin/hello-world"
  }
 }
}`
	readJSON, err := exec.Command("dpkg-deb", "-I", "hello_2.0.1_all.snap", "manifest").Output()
	c.Assert(err, IsNil)
	c.Assert(string(readJSON), Equals, expectedJSON)
}

func (s *SnapTestSuite) TestBuildAutoGenerateIntegrationHooksServices(c *C) {
	sourceDir := makeExampleSnapSourceDir(c, `name: hello
version: 3.0.1
vendor: Foo <foo@example.com>
services:
 app:
  name: foo
  start: bin/hello-world
`)

	resultSnap, err := Build(sourceDir)
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
 "description": "fixme-description",
 "installed-size": "fixme-999",
 "title": "fixme-title",
 "hooks": {
  "app": {
   "apparmor": "meta/foo.apparmor",
   "snappy-systemd": "meta/foo.snappy-systemd"
  }
 }
}`
	readJSON, err := exec.Command("dpkg-deb", "-I", "hello_3.0.1_all.snap", "manifest").Output()
	c.Assert(err, IsNil)
	c.Assert(string(readJSON), Equals, expectedJSON)

	// check the generated meta file
	unpackDir := c.MkDir()
	err = exec.Command("dpkg-deb", "-x", "hello_3.0.1_all.snap", unpackDir).Run()
	c.Assert(err, IsNil)

	snappySystemdContent, err := ioutil.ReadFile(filepath.Join(unpackDir, "meta/foo.snappy-systemd"))
	c.Assert(err, IsNil)
	c.Assert(string(snappySystemdContent), Equals, `{
 "description": "fixme-description",
 "name": "foo",
 "start": "bin/hello-world"
}`)
}
