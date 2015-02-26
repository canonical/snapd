package snappy

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"

	. "launchpad.net/gocheck"
)

// makeInstalledMockSnap creates a installed mock snap without any
// content other than the meta data
func makeInstalledMockSnap(tempdir string) (yamlFile string, err error) {
	const packageHello = `name: hello-app
version: 1.10
vendor: Michael Vogt <mvo@ubuntu.com>
icon: meta/hello.svg
binaries:
 - name: bin/hello
`

	metaDir := filepath.Join(tempdir, "apps", "hello-app", "1.10", "meta")
	if err := os.MkdirAll(metaDir, 0775); err != nil {
		return "", err
	}
	yamlFile = filepath.Join(metaDir, "package.yaml")
	if err := ioutil.WriteFile(yamlFile, []byte(packageHello), 0644); err != nil {
		return "", err
	}

	const securityJSON = `{
  "policy_vendor": "ubuntu-snappy"
  "policy_version": 1.3
}`
	appArmorDir := filepath.Join(tempdir, "var", "lib", "apparmor", "clicks")
	if err := os.MkdirAll(appArmorDir, 0775); err != nil {
		return "", err
	}
	apparmorFile := filepath.Join(appArmorDir, "hello-app_hello_1.10.json")
	if err := ioutil.WriteFile(apparmorFile, []byte(securityJSON), 0644); err != nil {
		return "", err
	}

	return yamlFile, nil
}

// makeTestSnapPackage creates a real snap package that can be installed on
// disk using packageYaml as its meta/package.yaml
func makeTestSnapPackage(c *C, packageYamlContent string) (snapFile string) {
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
	err := chDir(tmpdir, func() {
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

	snapFile := makeTestSnapPackage(c, packageYaml+"version: 1.0")
	c.Assert(installClick(snapFile, AllowUnauthenticated), IsNil)

	snapFile = makeTestSnapPackage(c, packageYaml+"version: 2.0")
	c.Assert(installClick(snapFile, AllowUnauthenticated), IsNil)

	m := NewMetaRepository()
	installed, err := m.Installed()
	c.Assert(err, IsNil)
	c.Assert(len(installed), Equals, 2)
}
