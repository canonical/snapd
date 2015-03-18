package snappy

import (
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	. "launchpad.net/gocheck"
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
architecture: ["i386", "amd64"]
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
	c.Assert(resultSnap, Equals, "hello_1.0.1_multi.snap")

	// check that the json looks valid
	const expectedJSON = `{
 "name": "hello",
 "version": "1.0.1",
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
	for _, needle := range []string{"./meta/package.yaml", "./meta/readme.md", "./bin/hello-world"} {
		c.Assert(strings.Contains(string(readFiles), needle), Equals, true)
	}
}

func (s *SnapTestSuite) TestBuildAutoGenerateIntegrationHooksBinaries(c *C) {
	sourceDir := makeExampleSnapSourceDir(c, `name: hello
version: 2.0.1
vendor: Foo <foo@example.com>
binaries:
 - name: bin/hello-world
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
 "framework": "ubuntu-core-15.04-dev1",
 "description": "some description",
 "installed-size": "17",
 "maintainer": "Foo \u003cfoo@example.com\u003e",
 "title": "some title",
 "hooks": {
  "hello-world": {
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
 - name: foo
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
 "framework": "ubuntu-core-15.04-dev1",
 "description": "some description",
 "installed-size": "17",
 "maintainer": "Foo \u003cfoo@example.com\u003e",
 "title": "some title",
 "hooks": {
  "foo": {
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
 "name": "foo",
 "description": "some description",
 "start": "bin/hello-world"
}`)
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

	resultSnap, err := Build(sourceDir)
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

func (s *SnapTestSuite) TestBuildCreateDebianMd5sumSimple(c *C) {
	tempdir := c.MkDir()

	// debian dir is ignored
	err := os.MkdirAll(filepath.Join(tempdir, "DEBIAN"), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(tempdir, "DEBIAN", "bar"), []byte(""), 0644)

	// regular files are looked at
	err = ioutil.WriteFile(filepath.Join(tempdir, "foo"), []byte(""), 0644)
	c.Assert(err, IsNil)

	// normal subdirs are supported
	err = os.MkdirAll(filepath.Join(tempdir, "bin"), 0755)
	err = ioutil.WriteFile(filepath.Join(tempdir, "bin", "bar"), []byte("bar\n"), 0644)
	c.Assert(err, IsNil)

	// symlinks are supported
	err = os.Symlink("/dsafdsafsadf", filepath.Join(tempdir, "broken-link"))
	c.Assert(err, IsNil)

	// data.tar.gz
	dataTar := filepath.Join(c.MkDir(), "data.tar.gz")
	err = ioutil.WriteFile(dataTar, []byte(""), 0644)

	// TEST write the hashes
	err = writeHashes(tempdir, dataTar)
	c.Assert(err, IsNil)

	// check content
	content, err := ioutil.ReadFile(filepath.Join(tempdir, "DEBIAN", "hashes.yaml"))
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, `archive-sha512: cf83e1357eefb8bdf1542850d66d8007d620e4050b5715dc83f4a921d36ce9ce47d0d13c5d85f2b0ff8318d2877eec2f63b931bd47417a81a538327af927da3e
files:
- name: bin
  mode: 2147484141
- name: bin/bar
  size: 4
  sha512: cc06808cbbee0510331aa97974132e8dc296aeb795be229d064bae784b0a87a5cf4281d82e8c99271b75db2148f08a026c1a60ed9cabdb8cac6d24242dac4063
  mode: 420
- name: broken-link
  mode: 134218239
- name: foo
  size: 0
  sha512: cf83e1357eefb8bdf1542850d66d8007d620e4050b5715dc83f4a921d36ce9ce47d0d13c5d85f2b0ff8318d2877eec2f63b931bd47417a81a538327af927da3e
  mode: 420
`)
}
