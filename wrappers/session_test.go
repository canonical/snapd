// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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

package wrappers_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/wrappers"
)

type sessionSuite struct {
	testutil.BaseTest
	tempdir string
}

var _ = Suite(&sessionSuite{})

func (s *sessionSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.BaseTest.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))
	s.tempdir = c.MkDir()
	dirs.SetRootDir(s.tempdir)
}

func (s *sessionSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
	dirs.SetRootDir("")
}

var sessionSnapYaml = `
name: foo
version: 1.0
slots:
  desktop:
    iface: desktop
`

var mockSessionFile = []byte(`
[Desktop Entry]
Name=foo
Exec=snap-session
`)

var mockSessionBinary = []byte(`
#!/bin/sh

echo "Hello World"
`)

func (s *sessionSuite) TestEnsurePackageSessionFiles(c *C) {
	reset := release.MockOnCoreDesktop(true)
	defer reset()

	expectedSessionFilePath := filepath.Join(dirs.SnapWaylandSessionsDir, "foo_foobar.desktop")
	c.Assert(osutil.FileExists(expectedSessionFilePath), Equals, false)

	info := snaptest.MockSnap(c, sessionSnapYaml, &snap.SideInfo{Revision: snap.R(11)})

	// generate .desktop file in the package baseDir
	baseDir := info.MountDir()
	err := os.MkdirAll(filepath.Join(baseDir, "usr/share/wayland-sessions"), 0755)
	c.Assert(err, IsNil)

	err = ioutil.WriteFile(filepath.Join(baseDir, "usr/share/wayland-sessions", "foobar.desktop"), mockSessionFile, 0644)
	c.Assert(err, IsNil)

	err = wrappers.EnsureSnapSessionFiles(info)
	c.Assert(err, IsNil)
	c.Assert(osutil.FileExists(expectedSessionFilePath), Equals, true)
}

func (s *sessionSuite) TestRemovePackageSessionFiles(c *C) {
	reset := release.MockOnCoreDesktop(true)
	defer reset()

	mockSessionFilePath := filepath.Join(dirs.SnapWaylandSessionsDir, "foo_foobar.desktop")

	err := os.MkdirAll(dirs.SnapWaylandSessionsDir, 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(mockSessionFilePath, mockSessionFile, 0644)
	c.Assert(err, IsNil)
	info, err := snap.InfoFromSnapYaml([]byte(sessionSnapYaml))
	c.Assert(err, IsNil)

	err = wrappers.RemoveSnapSessionFiles(info)
	c.Assert(err, IsNil)
	c.Assert(osutil.FileExists(mockSessionFilePath), Equals, false)
}

func (s *sessionSuite) TestParallelInstancesRemovePackageSessionFiles(c *C) {
	reset := release.MockOnCoreDesktop(true)
	defer reset()

	mockSessionFilePath := filepath.Join(dirs.SnapWaylandSessionsDir, "foo_foobar.desktop")
	mockSessionInstanceFilePath := filepath.Join(dirs.SnapWaylandSessionsDir, "foo+instance_foobar.desktop")

	err := os.MkdirAll(dirs.SnapWaylandSessionsDir, 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(mockSessionFilePath, mockSessionFile, 0644)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(mockSessionInstanceFilePath, mockSessionFile, 0644)
	c.Assert(err, IsNil)
	info, err := snap.InfoFromSnapYaml([]byte(sessionSnapYaml))
	c.Assert(err, IsNil)

	err = wrappers.RemoveSnapSessionFiles(info)
	c.Assert(err, IsNil)
	c.Assert(osutil.FileExists(mockSessionFilePath), Equals, false)
	// foo+instance file is still there
	c.Assert(osutil.FileExists(mockSessionInstanceFilePath), Equals, true)

	// restore the non-instance file
	err = ioutil.WriteFile(mockSessionFilePath, mockSessionFile, 0644)
	c.Assert(err, IsNil)

	info.InstanceKey = "instance"
	err = wrappers.RemoveSnapSessionFiles(info)
	c.Assert(err, IsNil)
	c.Assert(osutil.FileExists(mockSessionInstanceFilePath), Equals, false)
	// foo file is still there
	c.Assert(osutil.FileExists(mockSessionFilePath), Equals, true)
}

func (s *sessionSuite) TestEnsurePackageSessionFilesCleanup(c *C) {
	reset := release.MockOnCoreDesktop(true)
	defer reset()

	mockSessionFilePath := filepath.Join(dirs.SnapWaylandSessionsDir, "foo_foobar1.desktop")
	c.Assert(osutil.FileExists(mockSessionFilePath), Equals, false)

	err := os.MkdirAll(dirs.SnapWaylandSessionsDir, 0755)
	c.Assert(err, IsNil)

	mockSessionInstanceFilePath := filepath.Join(dirs.SnapWaylandSessionsDir, "foo+instance_foobar.desktop")
	err = ioutil.WriteFile(mockSessionInstanceFilePath, mockSessionFile, 0644)
	c.Assert(err, IsNil)

	err = os.MkdirAll(filepath.Join(dirs.SnapWaylandSessionsDir, "foo_foobar2.desktop", "potato"), 0755)
	c.Assert(err, IsNil)

	info := snaptest.MockSnap(c, sessionSnapYaml, &snap.SideInfo{Revision: snap.R(11)})

	// generate .desktop file in the package baseDir
	baseDir := info.MountDir()
	err = os.MkdirAll(filepath.Join(baseDir, "usr/share/wayland-sessions"), 0755)
	c.Assert(err, IsNil)

	err = ioutil.WriteFile(filepath.Join(baseDir, "usr/share/wayland-sessions", "foobar1.desktop"), mockSessionFile, 0644)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(baseDir, "usr/share/wayland-sessions", "foobar2.desktop"), mockSessionFile, 0644)
	c.Assert(err, IsNil)

	err = wrappers.EnsureSnapSessionFiles(info)
	c.Check(err, NotNil)
	c.Check(osutil.FileExists(mockSessionFilePath), Equals, false)
	// foo+instance file was not removed by cleanup
	c.Check(osutil.FileExists(mockSessionInstanceFilePath), Equals, true)
}

func (s *sessionSuite) TestRewriteExec(c *C) {
	reset := release.MockOnCoreDesktop(true)
	defer reset()

	expectedSessionFilePath := filepath.Join(dirs.SnapWaylandSessionsDir, "foo_foobar.desktop")
	c.Assert(osutil.FileExists(expectedSessionFilePath), Equals, false)

	info := snaptest.MockSnap(c, sessionSnapYaml, &snap.SideInfo{Revision: snap.R(11)})

	// generate .desktop file in the package baseDir
	baseDir := info.MountDir()
	err := os.MkdirAll(filepath.Join(baseDir, "usr/share/wayland-sessions"), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(baseDir, "usr/share/wayland-sessions", "foobar.desktop"), []byte(`
[Desktop Entry]
Name=foo
Exec=snap-session
`), 0644)
	c.Assert(err, IsNil)

	err = os.MkdirAll(filepath.Join(baseDir, "usr/bin"), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(baseDir, "usr/bin", "snap-session"), mockSessionBinary, 0644)
	c.Assert(err, IsNil)

	err = wrappers.EnsureSnapSessionFiles(info)
	c.Assert(err, IsNil)

	content, err := ioutil.ReadFile(expectedSessionFilePath)
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, fmt.Sprintf(`
[Desktop Entry]
X-SnapInstanceName=foo
Name=foo
Exec=ubuntu-core-desktop-session-wrapper %s/usr/bin/snap-session
`, baseDir))
}

func (s *sessionSuite) TestNonExistantExec(c *C) {
	reset := release.MockOnCoreDesktop(true)
	defer reset()

	expectedSessionFilePath := filepath.Join(dirs.SnapWaylandSessionsDir, "foo_foobar.desktop")
	info := snaptest.MockSnap(c, sessionSnapYaml, &snap.SideInfo{Revision: snap.R(11)})

	// generate .desktop file in the package baseDir
	baseDir := info.MountDir()
	err := os.MkdirAll(filepath.Join(baseDir, "usr/share/wayland-sessions"), 0755)
	c.Assert(err, IsNil)

	err = ioutil.WriteFile(filepath.Join(baseDir, "usr/share/wayland-sessions", "foobar.desktop"), []byte(`
[Desktop Entry]
Name=foo
Exec=snap-session
`), 0644)
	c.Assert(err, IsNil)

	err = wrappers.EnsureSnapSessionFiles(info)
	c.Assert(err, IsNil)

	content, err := ioutil.ReadFile(expectedSessionFilePath)
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, `
[Desktop Entry]
X-SnapInstanceName=foo
Name=foo
`)
}

func (s *sessionSuite) TestAbsoluteExec(c *C) {
	reset := release.MockOnCoreDesktop(true)
	defer reset()

	expectedSessionFilePath := filepath.Join(dirs.SnapWaylandSessionsDir, "foo_foobar.desktop")
	info := snaptest.MockSnap(c, sessionSnapYaml, &snap.SideInfo{Revision: snap.R(11)})

	// generate .desktop file in the package baseDir
	baseDir := info.MountDir()
	err := os.MkdirAll(filepath.Join(baseDir, "usr/share/wayland-sessions"), 0755)
	c.Assert(err, IsNil)

	err = ioutil.WriteFile(filepath.Join(baseDir, "usr/share/wayland-sessions", "foobar.desktop"), []byte(`
[Desktop Entry]
Name=foo
Exec=/bin/rm
`), 0644)
	c.Assert(err, IsNil)

	err = wrappers.EnsureSnapSessionFiles(info)
	c.Assert(err, IsNil)

	content, err := ioutil.ReadFile(expectedSessionFilePath)
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, `
[Desktop Entry]
X-SnapInstanceName=foo
Name=foo
`)
}

func (s *sessionSuite) TestRelativeExec(c *C) {
	reset := release.MockOnCoreDesktop(true)
	defer reset()

	expectedSessionFilePath := filepath.Join(dirs.SnapWaylandSessionsDir, "foo_foobar.desktop")
	info := snaptest.MockSnap(c, sessionSnapYaml, &snap.SideInfo{Revision: snap.R(11)})

	// generate .desktop file in the package baseDir
	baseDir := info.MountDir()
	err := os.MkdirAll(filepath.Join(baseDir, "usr/share/wayland-sessions"), 0755)
	c.Assert(err, IsNil)

	err = ioutil.WriteFile(filepath.Join(baseDir, "usr/share/wayland-sessions", "foobar.desktop"), []byte(`
[Desktop Entry]
Name=foo
Exec=../../../../../../bin/rm
`), 0644)
	c.Assert(err, IsNil)

	err = wrappers.EnsureSnapSessionFiles(info)
	c.Assert(err, IsNil)

	content, err := ioutil.ReadFile(expectedSessionFilePath)
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, `
[Desktop Entry]
X-SnapInstanceName=foo
Name=foo
`)
}

func (s *sessionSuite) TestNotCoreDesktop(c *C) {
	reset := release.MockOnCoreDesktop(false)
	defer reset()

	expectedSessionFilePath := filepath.Join(dirs.SnapWaylandSessionsDir, "foo_foobar.desktop")
	info := snaptest.MockSnap(c, sessionSnapYaml, &snap.SideInfo{Revision: snap.R(11)})

	// generate .desktop file in the package baseDir
	baseDir := info.MountDir()
	err := os.MkdirAll(filepath.Join(baseDir, "usr/share/wayland-sessions"), 0755)
	c.Assert(err, IsNil)

	err = ioutil.WriteFile(filepath.Join(baseDir, "usr/share/wayland-sessions", "foobar.desktop"), []byte(`
[Desktop Entry]
Name=foo
Exec=snap-session
`), 0644)
	c.Assert(err, IsNil)

	err = wrappers.EnsureSnapSessionFiles(info)
	c.Assert(err, IsNil)

	_, err = os.Stat(expectedSessionFilePath)
	c.Assert(os.IsNotExist(err), Equals, true)
}

func (s *sessionSuite) TestNoDesktopSlot(c *C) {
	reset := release.MockOnCoreDesktop(true)
	defer reset()

	expectedSessionFilePath := filepath.Join(dirs.SnapWaylandSessionsDir, "foo_foobar.desktop")
	info := snaptest.MockSnap(c, `
name: foo
version: 1.0
`, &snap.SideInfo{Revision: snap.R(11)})

	// generate .desktop file in the package baseDir
	baseDir := info.MountDir()
	err := os.MkdirAll(filepath.Join(baseDir, "usr/share/wayland-sessions"), 0755)
	c.Assert(err, IsNil)

	err = ioutil.WriteFile(filepath.Join(baseDir, "usr/share/wayland-sessions", "foobar.desktop"), []byte(`
[Desktop Entry]
Name=foo
Exec=snap-session
`), 0644)
	c.Assert(err, IsNil)

	err = wrappers.EnsureSnapSessionFiles(info)
	c.Assert(err, IsNil)

	_, err = os.Stat(expectedSessionFilePath)
	c.Assert(os.IsNotExist(err), Equals, true)
}

// sanitize

type sanitizeSessionFileSuite struct {
	testutil.BaseTest
}

var _ = Suite(&sanitizeSessionFileSuite{})

func (s *sanitizeSessionFileSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.BaseTest.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))
}

func (s *sanitizeSessionFileSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
}

func (s *sanitizeSessionFileSuite) TestSanitizeIgnoreUnknownKey(c *C) {
	snap := &snap.Info{SideInfo: snap.SideInfo{RealName: "foo", Revision: snap.R(12)}}
	desktopContent := []byte(`[Desktop Entry]
Name=foo
UnknownKey=baz
nonsense

# the empty line above is fine`)

	e := wrappers.SanitizeSessionFile(snap, "foo.desktop", desktopContent)
	c.Assert(string(e), Equals, `[Desktop Entry]
X-SnapInstanceName=foo
Name=foo

# the empty line above is fine
`)
}

// we do not support TryExec (even if its a valid line), this test ensures
// we do not accidentally enable it
func (s *sanitizeSessionFileSuite) TestSanitizeFiltersTryExecIgnored(c *C) {
	snap, err := snap.InfoFromSnapYaml([]byte(`
name: snap
version: 1.0
`))
	c.Assert(err, IsNil)
	desktopContent := []byte(`[Desktop Entry]
Name=foo
TryExec=snap-session-test
`)

	e := wrappers.SanitizeSessionFile(snap, "foo.desktop", desktopContent)
	c.Assert(string(e), Equals, `[Desktop Entry]
X-SnapInstanceName=snap
Name=foo
`)
}

func (s *sanitizeSessionFileSuite) TestSanitizeWorthWithI18n(c *C) {
	snap := &snap.Info{SideInfo: snap.SideInfo{RealName: "snap"}}
	desktopContent := []byte(`[Desktop Entry]
Name=foo
Invalid=key
Invalid[i18n]=key
`)

	e := wrappers.SanitizeSessionFile(snap, "foo.desktop", desktopContent)
	c.Assert(string(e), Equals, `[Desktop Entry]
X-SnapInstanceName=snap
Name=foo
`)
}

func (s *sanitizeSessionFileSuite) TestSanitizeParallelInstancesPlain(c *C) {
	snap, err := snap.InfoFromSnapYaml([]byte(`
name: snap
version: 1.0
`))
	snap.InstanceKey = "bar"
	c.Assert(err, IsNil)
	desktopContent := []byte(`[Desktop Entry]
Name=foo
`)
	e := wrappers.SanitizeSessionFile(snap, "", desktopContent)
	c.Assert(string(e), Equals, `[Desktop Entry]
X-SnapInstanceName=snap_bar
Name=foo
`)
}
func (s *sanitizeSessionFileSuite) TestLangLang(c *C) {
	langs := []struct {
		line    string
		isValid bool
	}{
		// langCodes
		{"Name[lang]=lang-alone", true},
		{"Name[_COUNTRY]=country-alone", false},
		{"Name[.ENC-0DING]=encoding-alone", false},
		{"Name[@modifier]=modifier-alone", false},
		{"Name[lang_COUNTRY]=lang+country", true},
		{"Name[lang.ENC-0DING]=lang+encoding", true},
		{"Name[lang@modifier]=lang+modifier", true},
		// could also test all bad combos of 2, and all combos of 3...
		{"Name[lang_COUNTRY.ENC-0DING@modifier]=all", true},
		// bad ones
		{"Name[foo=bar", false},
	}
	for _, t := range langs {
		c.Assert(wrappers.IsValidSessionFileLine([]byte(t.line)), Equals, t.isValid)
	}
}

func (s *sessionSuite) TestAddRemoveSessionFiles(c *C) {
	reset := release.MockOnCoreDesktop(true)
	defer reset()

	var tests = []struct {
		instance                string
		upstreamSessionFileName string

		expectedSessionFileName string
	}{
		// normal cases
		{"", "upstream.desktop", "foo_upstream.desktop"},
		{"instance", "upstream.desktop", "foo+instance_upstream.desktop"},
		// pathological cases are handled
		{"", "instance.desktop", "foo_instance.desktop"},
		{"instance", "instance.desktop", "foo+instance_instance.desktop"},
	}

	for _, t := range tests {
		expectedSessionFilePath := filepath.Join(dirs.SnapWaylandSessionsDir, t.expectedSessionFileName)
		c.Assert(osutil.FileExists(expectedSessionFilePath), Equals, false)

		info := snaptest.MockSnap(c, sessionSnapYaml, &snap.SideInfo{Revision: snap.R(11)})
		info.InstanceKey = t.instance

		// generate .desktop file in the package baseDir
		baseDir := info.MountDir()
		err := os.MkdirAll(filepath.Join(baseDir, "usr/share/wayland-sessions"), 0755)
		c.Assert(err, IsNil)

		err = ioutil.WriteFile(filepath.Join(baseDir, "usr/share/wayland-sessions", t.upstreamSessionFileName), mockSessionFile, 0644)
		c.Assert(err, IsNil)

		err = wrappers.EnsureSnapSessionFiles(info)
		c.Assert(err, IsNil)
		c.Assert(osutil.FileExists(expectedSessionFilePath), Equals, true)

		// Ensure that the old-style parallel install desktop file was
		// not created.
		if t.instance != "" {
			unexpectedOldStyleSessionFilePath := strings.Replace(expectedSessionFilePath, "+", "_", 1)
			c.Assert(osutil.FileExists(unexpectedOldStyleSessionFilePath), Equals, false)
		}

		// remove it again
		err = wrappers.RemoveSnapSessionFiles(info)
		c.Assert(err, IsNil)
		c.Assert(osutil.FileExists(expectedSessionFilePath), Equals, false)
	}
}
