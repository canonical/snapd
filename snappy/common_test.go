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

package snappy

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
)

const (
	testDeveloper         = "testspacethename"
	fooComposedName       = "foo"
	helloSnapComposedName = "hello-snap"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

func init() {
	os.Setenv("SNAPPY_SQUASHFS_UNPACK_FOR_TESTS", "1")
}

// makeInstalledMockSnap creates a installed mock snap without any
// content other than the meta data
func makeInstalledMockSnap(snapYamlContent string, revno int) (yamlFile string, err error) {
	const packageHello = `name: hello-snap
version: 1.10
summary: hello
description: Hello...
apps:
 hello:
  command: bin/hello
 svc1:
   command: bin/hello
   stop-command: bin/goodbye
   post-stop-command: bin/missya
   daemon: forking
`
	if snapYamlContent == "" {
		snapYamlContent = packageHello
	}

	info, err := snap.InfoFromSnapYaml([]byte(snapYamlContent))
	if err != nil {
		return "", err
	}
	info.SideInfo = snap.SideInfo{Revision: snap.R(revno)}

	metaDir := filepath.Join(info.MountDir(), "meta")
	if err := os.MkdirAll(metaDir, 0775); err != nil {
		return "", err
	}
	yamlFile = filepath.Join(metaDir, "snap.yaml")
	if err := ioutil.WriteFile(yamlFile, []byte(snapYamlContent), 0644); err != nil {
		return "", err
	}

	si := snap.SideInfo{
		OfficialName:      info.Name(),
		Revision:          snap.R(revno),
		Developer:         testDeveloper,
		Channel:           "remote-channel",
		EditedSummary:     "hello in summary",
		EditedDescription: "Hello...",
	}
	err = SaveManifest(&snap.Info{SideInfo: si, Version: info.Version})
	if err != nil {
		return "", err
	}

	return yamlFile, nil
}

// makeTestSnapPackage creates a real snap package that can be installed on
// disk using snapYamlContent as its meta/snap.yaml
func makeTestSnapPackage(c *C, snapYamlContent string) (snapPath string) {
	return makeTestSnapPackageFull(c, snapYamlContent, true)
}

func makeTestSnapPackageWithFiles(c *C, snapYamlContent string, files [][]string) (snapPath string) {
	return makeTestSnapPackageFullWithFiles(c, snapYamlContent, true, files)
}

func makeTestSnapPackageFull(c *C, snapYamlContent string, makeLicense bool) (snapPath string) {
	return makeTestSnapPackageFullWithFiles(c, snapYamlContent, makeLicense, [][]string{})
}

func makeTestSnapPackageFullWithFiles(c *C, snapYamlContent string, makeLicense bool, files [][]string) (snapPath string) {
	tmpdir := c.MkDir()
	// content
	os.MkdirAll(filepath.Join(tmpdir, "bin"), 0755)
	content := `#!/bin/sh
echo "hello"`
	exampleBinary := filepath.Join(tmpdir, "bin", "foo")
	ioutil.WriteFile(exampleBinary, []byte(content), 0755)
	// meta
	os.MkdirAll(filepath.Join(tmpdir, "meta"), 0755)
	if snapYamlContent == "" {
		snapYamlContent = `
name: foo
version: 1.0
`
	}
	snapYamlFn := filepath.Join(tmpdir, "meta", "snap.yaml")
	ioutil.WriteFile(snapYamlFn, []byte(snapYamlContent), 0644)
	if makeLicense {
		license := filepath.Join(tmpdir, "meta", "license.txt")
		content = "WTFPL"
		ioutil.WriteFile(license, []byte(content), 0644)
	}

	for _, filenameAndContent := range files {
		filename := filenameAndContent[0]
		content := filenameAndContent[1]
		basedir := filepath.Dir(filepath.Join(tmpdir, filename))
		err := os.MkdirAll(basedir, 0755)
		c.Assert(err, IsNil)
		err = ioutil.WriteFile(filepath.Join(tmpdir, filename), []byte(content), 0644)
		c.Assert(err, IsNil)
	}

	// build it
	err := osutil.ChDir(tmpdir, func() error {
		var err error
		snapPath, err = snaptest.BuildSquashfsSnap(tmpdir, "")
		c.Assert(err, IsNil)
		return err
	})
	c.Assert(err, IsNil)
	return filepath.Join(tmpdir, snapPath)
}

// makeTwoTestSnaps creates two real snaps of snap.Type of name
// "foo", with version "1.0" and "2.0", "2.0" being marked as the
// active snap.
func makeTwoTestSnaps(c *C, snapType snap.Type, extra ...string) (*snap.Info, *snap.Info) {
	inter := &MockProgressMeter{}

	snapYamlContent := `name: foo
`
	if len(extra) > 0 {
		snapYamlContent += strings.Join(extra, "\n") + "\n"
	}

	if snapType != snap.TypeApp {
		snapYamlContent += fmt.Sprintf("type: %s\n", snapType)
	}

	snapPath := makeTestSnapPackage(c, snapYamlContent+"version: 1.0")
	foo10 := &snap.SideInfo{
		OfficialName: "foo",
		Developer:    testDeveloper,
		Revision:     snap.R(100),
		Channel:      "remote-channel",
	}
	info1, err := (&Overlord{}).InstallWithSideInfo(snapPath, foo10, AllowUnauthenticated|AllowGadget, inter)
	c.Assert(err, IsNil)

	snapPath = makeTestSnapPackage(c, snapYamlContent+"version: 2.0")
	foo20 := &snap.SideInfo{
		OfficialName: "foo",
		Developer:    testDeveloper,
		Revision:     snap.R(200),
		Channel:      "remote-channel",
	}
	info2, err := (&Overlord{}).InstallWithSideInfo(snapPath, foo20, AllowUnauthenticated|AllowGadget, inter)
	c.Assert(err, IsNil)

	installed, err := (&Overlord{}).Installed()
	c.Assert(err, IsNil)
	c.Assert(installed, HasLen, 2)

	return info1, info2
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

func (m *MockProgressMeter) Notify(msg string) {
	m.notified = append(m.notified, msg)
}

// apparmor_parser mocks
func mockRunAppArmorParser(argv ...string) ([]byte, error) {
	return nil, nil
}
