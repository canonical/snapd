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
	"path/filepath"
	"strings"

	. "gopkg.in/check.v1"
	"gopkg.in/yaml.v2"

	"launchpad.net/snappy/dirs"
	"launchpad.net/snappy/helpers"
	"launchpad.net/snappy/pkg"
	"launchpad.net/snappy/pkg/remote"
)

const (
	testOrigin           = "testspacethename"
	fooComposedName      = "foo.testspacethename"
	helloAppComposedName = "hello-app.testspacethename"
)

// here to make it easy to switch in tests to "BuildSnapfsSnap"
var snapBuilderFunc = BuildLegacySnap

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
   stop: bin/goodbye
   poststop: bin/missya
`
	if packageYamlContent == "" {
		packageYamlContent = packageHello
	}

	var m packageYaml
	if err := yaml.Unmarshal([]byte(packageYamlContent), &m); err != nil {
		return "", err
	}

	dirName := m.qualifiedName(testOrigin)
	metaDir := filepath.Join(tempdir, "apps", dirName, m.Version, "meta")
	if err := os.MkdirAll(metaDir, 0775); err != nil {
		return "", err
	}
	yamlFile = filepath.Join(metaDir, "package.yaml")
	if err := ioutil.WriteFile(yamlFile, []byte(packageYamlContent), 0644); err != nil {
		return "", err
	}

	readmeMd := filepath.Join(metaDir, "readme.md")
	if err := ioutil.WriteFile(readmeMd, []byte("Hello\nApp"), 0644); err != nil {
		return "", err
	}

	if err := addDefaultApparmorJSON(tempdir, "hello-app_hello_1.10.json"); err != nil {
		return "", err
	}

	hashFile := filepath.Join(metaDir, "hashes.yaml")
	if err := ioutil.WriteFile(hashFile, []byte("{}"), 0644); err != nil {
		return "", err
	}

	if err := storeMinimalRemoteManifest(dirName, m.Name, testOrigin, m.Version, "Hello"); err != nil {
		return "", err
	}

	return yamlFile, nil
}

func storeMinimalRemoteManifest(qn, name, origin, version, desc string) error {
	if origin == SideloadedOrigin {
		panic("store remote manifest for sideloaded package")
	}
	content, err := yaml.Marshal(remote.Snap{
		Name:        name,
		Origin:      origin,
		Version:     version,
		Description: desc,
	})
	if err != nil {
		return err
	}

	if err := ioutil.WriteFile(filepath.Join(dirs.SnapMetaDir, fmt.Sprintf("%s_%s.manifest", qn, version)), content, 0644); err != nil {
		return err
	}

	return nil
}

func addDefaultApparmorJSON(tempdir, apparmorJSONPath string) error {
	appArmorDir := filepath.Join(tempdir, "var", "lib", "apparmor", "clicks")
	if err := os.MkdirAll(appArmorDir, 0775); err != nil {
		return err
	}

	const securityJSON = `{
  "policy_vendor": "ubuntu-core"
  "policy_version": 15.04
}`

	apparmorFile := filepath.Join(appArmorDir, apparmorJSONPath)
	return ioutil.WriteFile(apparmorFile, []byte(securityJSON), 0644)
}

// makeTestSnapPackage creates a real snap package that can be installed on
// disk using packageYaml as its meta/package.yaml
func makeTestSnapPackage(c *C, packageYamlContent string) (snapFile string) {
	return makeTestSnapPackageFull(c, packageYamlContent, true)
}

func makeTestSnapPackageWithFiles(c *C, packageYamlContent string, files [][]string) (snapFile string) {
	return makeTestSnapPackageFullWithFiles(c, packageYamlContent, true, files)
}

func makeTestSnapPackageFull(c *C, packageYamlContent string, makeLicense bool) (snapFile string) {
	return makeTestSnapPackageFullWithFiles(c, packageYamlContent, makeLicense, [][]string{})
}

func makeTestSnapPackageFullWithFiles(c *C, packageYamlContent string, makeLicense bool, files [][]string) (snapFile string) {
	tmpdir := c.MkDir()
	// content
	os.MkdirAll(filepath.Join(tmpdir, "bin"), 0755)
	content := `#!/bin/sh
echo "hello"`
	exampleBinary := filepath.Join(tmpdir, "bin", "foo")
	ioutil.WriteFile(exampleBinary, []byte(content), 0755)
	// meta
	os.MkdirAll(filepath.Join(tmpdir, "meta"), 0755)
	packageYaml := filepath.Join(tmpdir, "meta", "package.yaml")
	if packageYamlContent == "" {
		packageYamlContent = `
name: foo
version: 1.0
icon: foo.svg
vendor: Foo Bar <foo@example.com>
`
	}
	ioutil.WriteFile(packageYaml, []byte(packageYamlContent), 0644)
	readmeMd := filepath.Join(tmpdir, "meta", "readme.md")
	content = "Random\nExample"
	err := ioutil.WriteFile(readmeMd, []byte(content), 0644)
	c.Assert(err, IsNil)
	if makeLicense {
		license := filepath.Join(tmpdir, "meta", "license.txt")
		content = "WTFPL"
		ioutil.WriteFile(license, []byte(content), 0644)
	}

	for _, filenameAndContent := range files {
		filename := filenameAndContent[0]
		content := filenameAndContent[1]
		err = ioutil.WriteFile(filepath.Join(tmpdir, filename), []byte(content), 0644)
		c.Assert(err, IsNil)
	}

	// build it
	err = helpers.ChDir(tmpdir, func() error {
		var err error
		snapFile, err = snapBuilderFunc(tmpdir, "")
		c.Assert(err, IsNil)
		return err
	})
	c.Assert(err, IsNil)
	return filepath.Join(tmpdir, snapFile)
}

// makeTwoTestSnaps creates two real snaps of pkg.Type of name
// "foo", with version "1.0" and "2.0", "2.0" being marked as the
// active snap.
func makeTwoTestSnaps(c *C, snapType pkg.Type, extra ...string) {
	inter := &MockProgressMeter{}

	packageYaml := `name: foo
icon: foo.svg
vendor: Foo Bar <foo@example.com>
`
	if len(extra) > 0 {
		packageYaml += strings.Join(extra, "\n") + "\n"
	}

	qn := "foo." + testOrigin
	if snapType != pkg.TypeApp {
		packageYaml += fmt.Sprintf("type: %s\n", snapType)
		qn = "foo"
	}

	snapFile := makeTestSnapPackage(c, packageYaml+"version: 1.0")
	n, err := installClick(snapFile, AllowUnauthenticated|AllowOEM, inter, testOrigin)
	c.Assert(err, IsNil)
	c.Assert(n, Equals, "foo")
	c.Assert(storeMinimalRemoteManifest(qn, "foo", testOrigin, "1.0", ""), IsNil)

	snapFile = makeTestSnapPackage(c, packageYaml+"version: 2.0")
	n, err = installClick(snapFile, AllowUnauthenticated|AllowOEM, inter, testOrigin)
	c.Assert(err, IsNil)
	c.Assert(n, Equals, "foo")
	c.Assert(storeMinimalRemoteManifest(qn, "foo", testOrigin, "2.0", ""), IsNil)

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
	// Notifier:
	notified []string
	// Agreer:
	intro   string
	license string
	y       bool
}

func (m *MockProgressMeter) Start(pkg string, total float64) {
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
func (m *MockProgressMeter) Agreed(intro, license string) bool {
	m.intro = intro
	m.license = license
	return m.y
}
func (m *MockProgressMeter) Notify(msg string) {
	m.notified = append(m.notified, msg)
}

// seccomp filter mocks
const scFilterGenFakeResult = `
syscall1
syscall2
`

func mockRunScFilterGen(argv ...string) ([]byte, error) {
	return []byte(scFilterGenFakeResult), nil
}
