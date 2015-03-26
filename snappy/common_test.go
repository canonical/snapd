package snappy

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"

	"launchpad.net/snappy/helpers"

	"gopkg.in/yaml.v2"
	. "launchpad.net/gocheck"
)

// makeInstalledMockSnap creates a installed mock snap without any
// content other than the meta data
func makeInstalledMockSnap(tempdir, packageYamlContent string) (yamlFile string, err error) {
	const packageHello = `name: hello-app
version: 1.10
vendor: Michael Vogt <mvo@ubuntu.com>
icon: meta/hello.svg
binaries:
 - name: bin/hello
services:
 - name: svc1
   start: bin/hello
`
	if packageYamlContent == "" {
		packageYamlContent = packageHello
	}

	var m packageYaml
	if err := yaml.Unmarshal([]byte(packageYamlContent), &m); err != nil {
		return "", err
	}

	metaDir := filepath.Join(tempdir, "apps", m.Name, m.Version, "meta")
	if err := os.MkdirAll(metaDir, 0775); err != nil {
		return "", err
	}
	yamlFile = filepath.Join(metaDir, "package.yaml")
	if err := ioutil.WriteFile(yamlFile, []byte(packageYamlContent), 0644); err != nil {
		return "", err
	}

	if err := addDefaultApparmorJSON(tempdir, "hello-app_hello_1.10.json"); err != nil {
		return "", err
	}

	return yamlFile, nil
}

func addDefaultApparmorJSON(tempdir, apparmorJSONPath string) error {
	appArmorDir := filepath.Join(tempdir, "var", "lib", "apparmor", "clicks")
	if err := os.MkdirAll(appArmorDir, 0775); err != nil {
		return err
	}

	const securityJSON = `{
  "policy_vendor": "ubuntu-snappy"
  "policy_version": 1.3
}`

	apparmorFile := filepath.Join(appArmorDir, apparmorJSONPath)
	return ioutil.WriteFile(apparmorFile, []byte(securityJSON), 0644)
}

// makeTestSnapPackage creates a real snap package that can be installed on
// disk using packageYaml as its meta/package.yaml
func makeTestSnapPackage(c *C, packageYamlContent string) (snapFile string) {
	return makeTestSnapPackageFull(c, packageYamlContent, true)
}

func makeTestSnapPackageFull(c *C, packageYamlContent string, makeLicense bool) (snapFile string) {
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
	if makeLicense {
		license := path.Join(tmpdir, "meta", "license.txt")
		content = "WTFPL"
		ioutil.WriteFile(license, []byte(content), 0644)
	}
	// build it
	err := helpers.ChDir(tmpdir, func() {
		var err error
		snapFile, err = Build(tmpdir)
		c.Assert(err, IsNil)
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
	c.Assert(installClick(snapFile, AllowUnauthenticated, nil), IsNil)

	snapFile = makeTestSnapPackage(c, packageYaml+"version: 2.0")
	c.Assert(installClick(snapFile, AllowUnauthenticated, nil), IsNil)

	m := NewMetaRepository()
	installed, err := m.Installed()
	c.Assert(err, IsNil)
	c.Assert(installed, HasLen, 2)
}

type MockProgressMeter struct {
	total    float64
	progress []float64
	finished bool
	spin     bool
	spinMsg  string
	written  int
}

func (m *MockProgressMeter) Start(total float64) {
	m.total = total
}
func (m *MockProgressMeter) Set(current float64) {
	m.progress = append(m.progress, current)
}
func (m *MockProgressMeter) SetTotal(total float64) {
	m.total = total
}
func (m *MockProgressMeter) Spin(msg string) {
	m.spin = true
	m.spinMsg = msg
}
func (m *MockProgressMeter) Write(buf []byte) (n int, err error) {
	m.written += len(buf)
	return len(buf), err
}
func (m *MockProgressMeter) Finished() {
	m.finished = true
}
func (m *MockProgressMeter) Agreed(string, string) bool {
	return false
}
