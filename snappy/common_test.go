package snappy

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"

	"launchpad.net/snappy/helpers"

	. "launchpad.net/gocheck"
)

// makeMockSnap creates a mock snap that doesn't really exist on disk
func makeMockSnap(tempdir string) (yamlFile string, err error) {
	const packageHello = `name: hello-app
version: 1.10
vendor: Michael Vogt <mvo@ubuntu.com>
icon: meta/hello.svg
binaries:
 - name: bin/hello
`

	metaDir := filepath.Join(tempdir, "apps", "hello-app", "1.10", "meta")
	err = os.MkdirAll(metaDir, 0777)
	if err != nil {
		return "", err
	}
	yamlFile = filepath.Join(metaDir, "package.yaml")
	ioutil.WriteFile(yamlFile, []byte(packageHello), 0666)

	return yamlFile, err
}

// makeTestSnap creates a real snap package installed on disk
// using packageYaml as its meta/package.yaml
func makeTestSnap(c *C, packageYamlContent string) (snapFile string) {
	tmpdir := c.MkDir()
	// content
	os.MkdirAll(path.Join(tmpdir, "bin"), 0755)
	content := `#!/bin/sh
echo "hello"`
	exampleBinary := path.Join(tmpdir, "bin", "foo")
	ioutil.WriteFile(exampleBinary, []byte(content), 0755)
	// meta
	os.MkdirAll(path.Join(tmpdir, "meta"), 0755)
	packageYaml := path.Join(tmpdir, "meta", "package.yaml")
	if packageYamlContent == "" {
		packageYamlContent = `
name: foo
version: 1.0
icon: foo.svg
vendor: Foo Bar <foo@example.com>
`
	}
	ioutil.WriteFile(packageYaml, []byte(packageYamlContent), 0644)
	readmeMd := path.Join(tmpdir, "meta", "readme.md")
	content = "Random\nExample"
	ioutil.WriteFile(readmeMd, []byte(content), 0644)
	// build it
	err := helpers.ChDir(tmpdir, func() {
		cmd := exec.Command("snappy", "build", tmpdir)
		output, err := cmd.CombinedOutput()
		if err != nil {
			fmt.Println(string(output))
		}
		c.Assert(err, IsNil)
		allSnapFiles, err := filepath.Glob("*.snap")
		c.Assert(err, IsNil)
		c.Assert(len(allSnapFiles), Equals, 1)
		snapFile = allSnapFiles[0]
	})
	c.Assert(err, IsNil)
	return path.Join(tmpdir, snapFile)
}

// makeTwoTestSnaps creates two real snaps of SnapType of name
// "foo", with version "1.0" and "2.0", "2.0" being marked as the
// active snap.
func makeTwoTestSnaps(c *C, snapType SnapType) {
	packageYaml := `name: foo
icon: foo.svg
vendor: Foo Bar <foo@example.com>
`

	if snapType != SnapTypeApp {
		packageYaml += fmt.Sprintf("type: %s\n", snapType)
	}

	snapFile := makeTestSnap(c, packageYaml+"version: 1.0")
	c.Assert(installClick(snapFile, AllowUnauthenticated), IsNil)

	snapFile = makeTestSnap(c, packageYaml+"version: 2.0")
	c.Assert(installClick(snapFile, AllowUnauthenticated), IsNil)

	m := NewMetaRepository()
	installed, err := m.Installed()
	c.Assert(err, IsNil)
	c.Assert(len(installed), Equals, 2)
}
