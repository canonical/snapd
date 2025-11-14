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

package wrappers_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/wrappers"
)

type desktopSuite struct {
	testutil.BaseTest
	tempdir string

	mockUpdateDesktopDatabase *testutil.MockCmd
}

var _ = Suite(&desktopSuite{})

func (s *desktopSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.BaseTest.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))
	s.tempdir = c.MkDir()
	dirs.SetRootDir(s.tempdir)

	s.mockUpdateDesktopDatabase = testutil.MockCommand(c, "update-desktop-database", "")
}

func (s *desktopSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
	s.mockUpdateDesktopDatabase.Restore()
	dirs.SetRootDir("")
}

var desktopAppYaml = `
name: foo
version: 1.0
apps:
    foobar:
`

var mockDesktopFile = []byte(`
[Desktop Entry]
Name=foo
Icon=${SNAP}/foo.png`)

func (s *desktopSuite) TestEnsurePackageDesktopFiles(c *C) {
	expectedDesktopFilePath := filepath.Join(dirs.SnapDesktopFilesDir, "foo_foobar.desktop")
	c.Assert(osutil.FileExists(expectedDesktopFilePath), Equals, false)

	oldDesktopFilePath := filepath.Join(dirs.SnapDesktopFilesDir, "foo_foobar2.desktop")
	c.Assert(os.MkdirAll(dirs.SnapDesktopFilesDir, 0755), IsNil)
	c.Assert(os.WriteFile(oldDesktopFilePath, mockDesktopFile, 0644), IsNil)
	c.Assert(osutil.FileExists(oldDesktopFilePath), Equals, true)

	info := snaptest.MockSnap(c, desktopAppYaml, &snap.SideInfo{Revision: snap.R(11)})

	// generate .desktop file in the package baseDir
	baseDir := info.MountDir()
	c.Assert(os.MkdirAll(filepath.Join(baseDir, "meta", "gui"), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(baseDir, "meta", "gui", "foobar.desktop"), mockDesktopFile, 0644), IsNil)

	err := wrappers.EnsureSnapDesktopFiles([]*snap.Info{info})
	c.Assert(err, IsNil)
	c.Assert(osutil.FileExists(expectedDesktopFilePath), Equals, true)
	stat, err := os.Stat(expectedDesktopFilePath)
	c.Assert(err, IsNil)
	c.Assert(stat.Mode().Perm(), Equals, os.FileMode(0644))
	c.Assert(s.mockUpdateDesktopDatabase.Calls(), DeepEquals, [][]string{
		{"update-desktop-database", dirs.SnapDesktopFilesDir},
	})
	sanitizedDesktopFileContent := wrappers.SanitizeDesktopFile(info, expectedDesktopFilePath, mockDesktopFile)
	c.Check(expectedDesktopFilePath, testutil.FileContains, sanitizedDesktopFileContent)

	// Old desktop file should be removed because it follows the
	// <desktop-prefix>_*.desktop pattern.
	c.Assert(osutil.FileExists(oldDesktopFilePath), Equals, false)
}

func (s *desktopSuite) TestEnsurePackageDesktopFilesMangledDuplicate(c *C) {
	expectedDesktopFilePath := filepath.Join(dirs.SnapDesktopFilesDir, "foo_foobar._.desktop")
	c.Assert(osutil.FileExists(expectedDesktopFilePath), Equals, false)

	info := snaptest.MockSnap(c, desktopAppYaml, &snap.SideInfo{Revision: snap.R(11)})
	baseDir := info.MountDir()
	c.Assert(os.MkdirAll(filepath.Join(baseDir, "meta", "gui"), 0755), IsNil)
	// When mangled, both files will be foo_foobar._.desktop and should error
	c.Assert(os.WriteFile(filepath.Join(baseDir, "meta", "gui", "foobar.*.desktop"), mockDesktopFile, 0644), IsNil)
	c.Assert(os.WriteFile(filepath.Join(baseDir, "meta", "gui", "foobar.$.desktop"), mockDesktopFile, 0644), IsNil)

	err := wrappers.EnsureSnapDesktopFiles([]*snap.Info{info})
	c.Assert(err, Equals, nil)

	// Only one will be written, duplicates will be skipped
	c.Assert(osutil.FileExists(expectedDesktopFilePath), Equals, true)
	files, err := os.ReadDir(dirs.SnapDesktopFilesDir)
	c.Assert(err, IsNil)
	c.Assert(files, HasLen, 1)
}

func (s *desktopSuite) testEnsurePackageDesktopFilesWithDesktopInterface(c *C, hasDesktopFileIDs bool) {
	var desktopAppYaml = `
name: foo
version: 1.0
plugs:
  desktop:
`
	if hasDesktopFileIDs {
		desktopAppYaml += "\n    desktop-file-ids: [org.example.Foo]"
	}
	info := snaptest.MockSnap(c, desktopAppYaml, &snap.SideInfo{Revision: snap.R(11)})
	c.Assert(info.Plugs["desktop"], NotNil)

	expectedDesktopFilePath1 := filepath.Join(dirs.SnapDesktopFilesDir, "foo_org.example.Foo.desktop")
	if hasDesktopFileIDs {
		expectedDesktopFilePath1 = filepath.Join(dirs.SnapDesktopFilesDir, "org.example.Foo.desktop")
	}
	c.Assert(osutil.FileExists(expectedDesktopFilePath1), Equals, false)
	expectedDesktopFilePath2 := filepath.Join(dirs.SnapDesktopFilesDir, "foo_foobar.desktop")
	c.Assert(osutil.FileExists(expectedDesktopFilePath2), Equals, false)

	// generate .desktop file in the package baseDir
	baseDir := info.MountDir()
	c.Assert(os.MkdirAll(filepath.Join(baseDir, "meta", "gui"), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(baseDir, "meta", "gui", "org.example.Foo.desktop"), mockDesktopFile, 0644), IsNil)
	c.Assert(os.WriteFile(filepath.Join(baseDir, "meta", "gui", "foobar.desktop"), mockDesktopFile, 0644), IsNil)

	err := wrappers.EnsureSnapDesktopFiles([]*snap.Info{info})
	c.Assert(err, IsNil)

	for _, expectedDesktopFilePath := range []string{expectedDesktopFilePath1, expectedDesktopFilePath2} {
		stat, err := os.Stat(expectedDesktopFilePath)
		c.Assert(err, IsNil)
		c.Assert(stat.Mode().Perm(), Equals, os.FileMode(0644))
		c.Assert(s.mockUpdateDesktopDatabase.Calls(), DeepEquals, [][]string{
			{"update-desktop-database", dirs.SnapDesktopFilesDir},
		})

		sanitizedDesktopFileContent := wrappers.SanitizeDesktopFile(info, expectedDesktopFilePath, mockDesktopFile)
		c.Check(expectedDesktopFilePath, testutil.FileEquals, sanitizedDesktopFileContent)
	}
}

func (s *desktopSuite) TestEnsurePackageDesktopFilesWithDesktopInterface(c *C) {
	const hasDesktopFileIDs = false
	s.testEnsurePackageDesktopFilesWithDesktopInterface(c, hasDesktopFileIDs)
}

func (s *desktopSuite) TestEnsurePackageDesktopFilesWithDesktopFileIDs(c *C) {
	const hasDesktopFileIDs = true
	s.testEnsurePackageDesktopFilesWithDesktopInterface(c, hasDesktopFileIDs)
}

func (s *desktopSuite) TestEnsurePackageDesktopFilesWithBadDesktopFileIDs(c *C) {
	const desktopAppYamlTemplate = `
name: foo
version: 1.0
plugs:
  desktop:
    desktop-file-ids: %s
`

	for _, tc := range []string{
		"not-a-list-of-strings",
		"1",
		"true",
		"[[string],1]",
	} {
		desktopAppYaml := fmt.Sprintf(desktopAppYamlTemplate, tc)
		info := snaptest.MockSnap(c, desktopAppYaml, &snap.SideInfo{Revision: snap.R(11)})
		c.Assert(info.Plugs["desktop"], NotNil)

		err := wrappers.EnsureSnapDesktopFiles([]*snap.Info{info})
		c.Assert(err, ErrorMatches, `internal error: "desktop-file-ids" must be a list of strings`)
	}
}

func (s *desktopSuite) TestEnsurePackageDesktopFilesMultiple(c *C) {
	info1 := snaptest.MockSnap(c, desktopAppYaml, &snap.SideInfo{Revision: snap.R(11)})
	baseDir := info1.MountDir()
	c.Assert(os.MkdirAll(filepath.Join(baseDir, "meta", "gui"), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(baseDir, "meta", "gui", "foobar.desktop"), mockDesktopFile, 0644), IsNil)

	info2 := snaptest.MockSnap(c, strings.Replace(desktopAppYaml, "name: foo", "name: bar", 1), &snap.SideInfo{Revision: snap.R(12)})
	baseDir = info2.MountDir()
	c.Assert(os.MkdirAll(filepath.Join(baseDir, "meta", "gui"), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(baseDir, "meta", "gui", "foobar.desktop"), mockDesktopFile, 0644), IsNil)

	err := wrappers.EnsureSnapDesktopFiles([]*snap.Info{info1, info2})
	c.Assert(err, IsNil)

	// Desktop files for both snaps were installed
	desktopFilePath := filepath.Join(dirs.SnapDesktopFilesDir, "foo_foobar.desktop")
	c.Assert(desktopFilePath, testutil.FilePresent)
	desktopFilePath = filepath.Join(dirs.SnapDesktopFilesDir, "bar_foobar.desktop")
	c.Assert(desktopFilePath, testutil.FilePresent)
}

func (s *iconsTestSuite) TestEnsurePackageDesktopFilesNilSnapInfo(c *C) {
	c.Assert(wrappers.EnsureSnapDesktopFiles([]*snap.Info{nil}), ErrorMatches, "internal error: snap info cannot be nil")
}

func (s *desktopSuite) TestEnsurePackageDesktopFilesExistingFileError(c *C) {
	const desktopAppYaml = `
name: foo
version: 1.0
plugs:
  desktop:
    desktop-file-ids: [org.example.Foo]
`
	info := snaptest.MockSnap(c, desktopAppYaml, &snap.SideInfo{Revision: snap.R(11)})
	c.Assert(info.Plugs["desktop"], NotNil)

	// Mock existing desktop file with same name for another snap
	var mockBadDesktopFile = []byte(`
[Desktop Entry]
Name=foo
X-SnapInstanceName=bar`)
	badDesktopFilePath := filepath.Join(dirs.SnapDesktopFilesDir, "org.example.Foo.desktop")
	c.Assert(os.MkdirAll(dirs.SnapDesktopFilesDir, 0755), IsNil)
	c.Assert(os.WriteFile(badDesktopFilePath, mockBadDesktopFile, 0644), IsNil)

	// generate .desktop file in the package baseDir
	baseDir := info.MountDir()
	c.Assert(os.MkdirAll(filepath.Join(baseDir, "meta", "gui"), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(baseDir, "meta", "gui", "org.example.Foo.desktop"), mockDesktopFile, 0644), IsNil)

	err := wrappers.EnsureSnapDesktopFiles([]*snap.Info{info})
	c.Assert(err, ErrorMatches, `cannot install "org.example.Foo.desktop": ".*/var/lib/snapd/desktop/applications/org.example.Foo.desktop" already exists for another snap`)

	c.Check(badDesktopFilePath, testutil.FileEquals, mockBadDesktopFile)
}

func (s *desktopSuite) testRemovePackageDesktopFiles(c *C, triggerErr bool) {
	const desktopFileTemplate = `
[Desktop Entry]
X-SnapInstanceName=%s
Name=Test`
	desktopFileToSnapName := map[string]string{
		"org.example.desktop":     "foo",
		"org.example.Foo.desktop": "foo",
		"foo_app.desktop":         "foo",
		"org.example.Bar.desktop": "bar",
		"bar_app.desktop":         "bar",
	}
	c.Assert(os.MkdirAll(dirs.SnapDesktopFilesDir, 0755), IsNil)
	for desktopFile, snapName := range desktopFileToSnapName {
		mockDesktopFile := fmt.Sprintf(desktopFileTemplate, snapName)
		c.Assert(os.WriteFile(filepath.Join(dirs.SnapDesktopFilesDir, desktopFile), []byte(mockDesktopFile), 0644), IsNil)
	}

	if triggerErr {
		c.Assert(os.MkdirAll(filepath.Join(dirs.SnapDesktopFilesDir, "foo_foobar2.desktop", "potato"), 0755), IsNil)
	}

	info, err := snap.InfoFromSnapYaml([]byte(desktopAppYaml))
	c.Assert(err, IsNil)

	err = wrappers.RemoveSnapDesktopFiles(info)
	if triggerErr {
		c.Assert(err, ErrorMatches, ".*directory not empty")
	} else {
		c.Assert(err, IsNil)
	}

	for desktopFile, snapName := range desktopFileToSnapName {
		desktopFilePath := filepath.Join(dirs.SnapDesktopFilesDir, desktopFile)
		if snapName == "foo" {
			c.Check(desktopFilePath, testutil.FileAbsent, Commentf(desktopFile))
		} else {
			c.Check(desktopFilePath, testutil.FilePresent, Commentf(desktopFile))
		}
	}

	expectedUpdateDesktopDatabase := [][]string{
		{"update-desktop-database", dirs.SnapDesktopFilesDir},
	}
	if triggerErr {
		expectedUpdateDesktopDatabase = nil
	}
	c.Assert(s.mockUpdateDesktopDatabase.Calls(), DeepEquals, expectedUpdateDesktopDatabase)
}

func (s *desktopSuite) TestRemovePackageDesktopFiles(c *C) {
	const triggerErr = false
	s.testRemovePackageDesktopFiles(c, triggerErr)
}

func (s *desktopSuite) TestRemovePackageDesktopFilesError(c *C) {
	const triggerErr = true
	s.testRemovePackageDesktopFiles(c, triggerErr)
}

func (s *desktopSuite) TestParallelInstancesRemovePackageDesktopFiles(c *C) {
	err := os.MkdirAll(dirs.SnapDesktopFilesDir, 0755)
	c.Assert(err, IsNil)

	const desktopFileTemplate = `
[Desktop Entry]
Name=Test
X-SnapInstanceName=%s`

	mockDesktopFilePath := filepath.Join(dirs.SnapDesktopFilesDir, "foo_foobar.desktop")
	err = os.WriteFile(mockDesktopFilePath, []byte(fmt.Sprintf(desktopFileTemplate, "foo")), 0644)
	c.Assert(err, IsNil)

	mockDesktopInstanceFilePath := filepath.Join(dirs.SnapDesktopFilesDir, "foo+instance_foobar.desktop")
	err = os.WriteFile(mockDesktopInstanceFilePath, []byte(fmt.Sprintf(desktopFileTemplate, "foo_instance")), 0644)
	c.Assert(err, IsNil)

	info, err := snap.InfoFromSnapYaml([]byte(desktopAppYaml))
	c.Assert(err, IsNil)

	err = wrappers.RemoveSnapDesktopFiles(info)
	c.Assert(err, IsNil)
	c.Assert(osutil.FileExists(mockDesktopFilePath), Equals, false)
	c.Assert(s.mockUpdateDesktopDatabase.Calls(), DeepEquals, [][]string{
		{"update-desktop-database", dirs.SnapDesktopFilesDir},
	})
	// foo+instance file is still there
	c.Assert(osutil.FileExists(mockDesktopInstanceFilePath), Equals, true)

	// restore the non-instance file
	err = os.WriteFile(mockDesktopFilePath, []byte(fmt.Sprintf(desktopFileTemplate, "foo")), 0644)
	c.Assert(err, IsNil)

	s.mockUpdateDesktopDatabase.ForgetCalls()

	info.InstanceKey = "instance"
	err = wrappers.RemoveSnapDesktopFiles(info)
	c.Assert(err, IsNil)
	c.Assert(osutil.FileExists(mockDesktopInstanceFilePath), Equals, false)
	c.Assert(s.mockUpdateDesktopDatabase.Calls(), DeepEquals, [][]string{
		{"update-desktop-database", dirs.SnapDesktopFilesDir},
	})
	// foo file is still there
	c.Assert(osutil.FileExists(mockDesktopFilePath), Equals, true)
}

func (s *desktopSuite) TestEnsurePackageDesktopFilesCleanupOnError(c *C) {
	mockDesktopFilePath := filepath.Join(dirs.SnapDesktopFilesDir, "foo_foobar1.desktop")
	c.Assert(osutil.FileExists(mockDesktopFilePath), Equals, false)

	err := os.MkdirAll(dirs.SnapDesktopFilesDir, 0755)
	c.Assert(err, IsNil)

	mockDesktopInstanceFilePath := filepath.Join(dirs.SnapDesktopFilesDir, "foo+instance_foobar.desktop")
	err = os.WriteFile(mockDesktopInstanceFilePath, mockDesktopFile, 0644)
	c.Assert(err, IsNil)

	err = os.MkdirAll(filepath.Join(dirs.SnapDesktopFilesDir, "foo_foobar2.desktop", "potato"), 0755)
	c.Assert(err, IsNil)

	info := snaptest.MockSnap(c, desktopAppYaml, &snap.SideInfo{Revision: snap.R(11)})

	// generate .desktop file in the package baseDir
	baseDir := info.MountDir()
	err = os.MkdirAll(filepath.Join(baseDir, "meta", "gui"), 0755)
	c.Assert(err, IsNil)

	err = os.WriteFile(filepath.Join(baseDir, "meta", "gui", "foobar1.desktop"), mockDesktopFile, 0644)
	c.Assert(err, IsNil)
	err = os.WriteFile(filepath.Join(baseDir, "meta", "gui", "foobar2.desktop"), mockDesktopFile, 0644)
	c.Assert(err, IsNil)

	err = wrappers.EnsureSnapDesktopFiles([]*snap.Info{info})
	c.Check(err, ErrorMatches, "internal error: only regular files are supported.*")
	c.Check(osutil.FileExists(mockDesktopFilePath), Equals, false)
	c.Check(s.mockUpdateDesktopDatabase.Calls(), HasLen, 0)
	// foo+instance file was not removed by cleanup
	c.Check(osutil.FileExists(mockDesktopInstanceFilePath), Equals, true)
}

func (s *desktopSuite) TestEnsurePackageDesktopFilesCleansOldFiles(c *C) {
	info := snaptest.MockSnap(c, desktopAppYaml, &snap.SideInfo{Revision: snap.R(11)})

	const desktopFileTemplate = `
[Desktop Entry]
Name=Test
X-SnapInstanceName=%s`
	desktopFileToSnapName := map[string]string{
		"org.example.desktop":         "foo",
		"org.example.Foo.desktop":     "foo",
		"foo_old_app.desktop":         "foo",
		"foo_app.desktop":             "foo",
		"foo+instance_foobar.desktop": "foo_instance",
		"org.example.Bar.desktop":     "bar",
		"bar_app.desktop":             "bar",
	}
	// Mock already installed desktop files
	c.Assert(os.MkdirAll(dirs.SnapDesktopFilesDir, 0755), IsNil)
	for desktopFile, snapName := range desktopFileToSnapName {
		mockDesktopFile := fmt.Sprintf(desktopFileTemplate, snapName)
		c.Assert(os.WriteFile(filepath.Join(dirs.SnapDesktopFilesDir, desktopFile), []byte(mockDesktopFile), 0644), IsNil)
	}

	// generate .desktop file in the package baseDir
	baseDir := info.MountDir()
	c.Assert(os.MkdirAll(filepath.Join(baseDir, "meta", "gui"), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(baseDir, "meta", "gui", "app.desktop"), mockDesktopFile, 0644), IsNil)

	err := wrappers.EnsureSnapDesktopFiles([]*snap.Info{info})
	c.Assert(err, IsNil)

	// Check that old desktop files for "foo" snap were removed
	for desktopFile, snapName := range desktopFileToSnapName {
		desktopFilePath := filepath.Join(dirs.SnapDesktopFilesDir, desktopFile)
		if snapName == "foo" && desktopFile != "foo_app.desktop" {
			c.Check(desktopFilePath, testutil.FileAbsent, Commentf(desktopFile))
		} else {
			c.Check(desktopFilePath, testutil.FilePresent)
		}
	}
	// Check that foo_app.desktop was rewritten from snap
	expectedDesktopFilePath := filepath.Join(dirs.SnapDesktopFilesDir, "foo_app.desktop")
	sanitizedDesktopFileContent := wrappers.SanitizeDesktopFile(info, expectedDesktopFilePath, mockDesktopFile)
	c.Check(expectedDesktopFilePath, testutil.FileEquals, sanitizedDesktopFileContent)
}

// sanitize

type sanitizeDesktopFileSuite struct {
	testutil.BaseTest
}

var _ = Suite(&sanitizeDesktopFileSuite{})

func (s *sanitizeDesktopFileSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.BaseTest.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))
}

func (s *sanitizeDesktopFileSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
}

func (s *sanitizeDesktopFileSuite) TestSanitizeIgnoreNotWhitelisted(c *C) {
	snap := &snap.Info{SideInfo: snap.SideInfo{RealName: "foo", Revision: snap.R(12)}}
	desktopContent := []byte(`[Desktop Entry]
Name=foo
UnknownKey=baz
nonsense
Icon=${SNAP}/meep

# the empty line above is fine`)

	e := wrappers.SanitizeDesktopFile(snap, "foo.desktop", desktopContent)
	c.Assert(string(e), Equals, fmt.Sprintf(`[Desktop Entry]
X-SnapInstanceName=foo
Name=foo
Icon=%s/foo/current/meep

# the empty line above is fine
`, dirs.SnapMountDir))
}

func (s *sanitizeDesktopFileSuite) TestSanitizeFiltersExec(c *C) {
	snap, err := snap.InfoFromSnapYaml([]byte(`
name: snap
version: 1.0
apps:
 app:
  command: cmd
`))
	c.Assert(err, IsNil)
	desktopContent := []byte(`[Desktop Entry]
Name=foo
Exec=baz
`)

	e := wrappers.SanitizeDesktopFile(snap, "foo.desktop", desktopContent)
	c.Assert(string(e), Equals, `[Desktop Entry]
X-SnapInstanceName=snap
Name=foo
`)
}

func (s *sanitizeDesktopFileSuite) TestSanitizeFiltersExecPrefix(c *C) {
	snap, err := snap.InfoFromSnapYaml([]byte(`
name: snap
version: 1.0
apps:
 app:
  command: cmd
`))
	c.Assert(err, IsNil)
	desktopContent := []byte(`[Desktop Entry]
X-SnapInstanceName=snap
Name=foo
Exec=snap.app.evil.evil
`)

	e := wrappers.SanitizeDesktopFile(snap, "foo.desktop", desktopContent)
	c.Assert(string(e), Equals, `[Desktop Entry]
X-SnapInstanceName=snap
Name=foo
`)
}

func (s *sanitizeDesktopFileSuite) TestSanitizeFiltersExecRewriteFromDesktop(c *C) {
	snap, err := snap.InfoFromSnapYaml([]byte(`
name: snap
version: 1.0
apps:
 app:
  command: cmd
`))
	c.Assert(err, IsNil)
	desktopContent := []byte(`[Desktop Entry]
X-SnapInstanceName=snap
Name=foo
Exec=snap.app.evil.evil
`)

	e := wrappers.SanitizeDesktopFile(snap, "app.desktop", desktopContent)
	c.Assert(string(e), Equals, fmt.Sprintf(`[Desktop Entry]
X-SnapInstanceName=snap
Name=foo
X-SnapAppName=app
Exec=%s/bin/snap.app
`, dirs.SnapMountDir))
}

func (s *sanitizeDesktopFileSuite) TestSanitizeFiltersExecRewriteFromDesktopWithCommonID(c *C) {
	snap, err := snap.InfoFromSnapYaml([]byte(`
name: snap
version: 1.0
apps:
 app:
  command: cmd
  common-id: io.snapcraft.app
`))
	c.Assert(err, IsNil)
	desktopContent := []byte(`[Desktop Entry]
X-SnapInstanceName=snap
Name=foo
Exec=snap.app.evil.evil
`)

	e := wrappers.SanitizeDesktopFile(snap, "app.desktop", desktopContent)
	c.Assert(string(e), Equals, fmt.Sprintf(`[Desktop Entry]
X-SnapInstanceName=snap
Name=foo
X-SnapAppName=app
X-SnapCommonID=io.snapcraft.app
Exec=%s/bin/snap.app
`, dirs.SnapMountDir))
}

func (s *sanitizeDesktopFileSuite) TestSanitizeFiltersExecOk(c *C) {
	snap, err := snap.InfoFromSnapYaml([]byte(`
name: snap
version: 1.0
apps:
 app:
  command: cmd
`))
	c.Assert(err, IsNil)
	desktopContent := []byte(`[Desktop Entry]
Name=foo
Exec=snap.app %U
`)

	e := wrappers.SanitizeDesktopFile(snap, "foo.desktop", desktopContent)
	c.Assert(string(e), Equals, fmt.Sprintf(`[Desktop Entry]
X-SnapInstanceName=snap
Name=foo
X-SnapAppName=app
Exec=%s/bin/snap.app %%U
`, dirs.SnapMountDir))
}

// we do not support TryExec (even if its a valid line), this test ensures
// we do not accidentally enable it
func (s *sanitizeDesktopFileSuite) TestSanitizeFiltersTryExecIgnored(c *C) {
	snap, err := snap.InfoFromSnapYaml([]byte(`
name: snap
version: 1.0
apps:
 app:
  command: cmd
`))
	c.Assert(err, IsNil)
	desktopContent := []byte(`[Desktop Entry]
Name=foo
TryExec=snap.app %U
`)

	e := wrappers.SanitizeDesktopFile(snap, "foo.desktop", desktopContent)
	c.Assert(string(e), Equals, `[Desktop Entry]
X-SnapInstanceName=snap
Name=foo
`)
}

func (s *sanitizeDesktopFileSuite) TestSanitizeWorthWithI18n(c *C) {
	snap := &snap.Info{SideInfo: snap.SideInfo{RealName: "snap"}}
	desktopContent := []byte(`[Desktop Entry]
Name=foo
GenericName=bar
GenericName[de]=einsehrlangeszusammengesetzteswort
GenericName[tlh_TLH]=Qapla'
GenericName[ca@valencia]=Hola!
Invalid=key
Invalid[i18n]=key
`)

	e := wrappers.SanitizeDesktopFile(snap, "foo.desktop", desktopContent)
	c.Assert(string(e), Equals, `[Desktop Entry]
X-SnapInstanceName=snap
Name=foo
GenericName=bar
GenericName[de]=einsehrlangeszusammengesetzteswort
GenericName[tlh_TLH]=Qapla'
GenericName[ca@valencia]=Hola!
`)
}

func (s *sanitizeDesktopFileSuite) TestSanitizeDesktopActionsOk(c *C) {
	snap := &snap.Info{}
	desktopContent := []byte("[Desktop Action is-ok]\n")

	e := wrappers.SanitizeDesktopFile(snap, "foo.desktop", desktopContent)
	c.Assert(string(e), Equals, string(desktopContent))
}

func (s *sanitizeDesktopFileSuite) TestSanitizeDesktopFileIcon(c *C) {
	snap := &snap.Info{SideInfo: snap.SideInfo{RealName: "snap"}}

	desktopContent := []byte(`[Desktop Entry]
X-SnapInstanceName=snap
Icon=${SNAP}/icon.png
`)

	desktopExpected := append(
		[]byte(`[Desktop Entry]
X-SnapInstanceName=snap
Icon=`), []byte(dirs.SnapMountDir)...)

	desktopExpected = append(desktopExpected, []byte(`/snap/current/icon.png
`)...)

	e := wrappers.SanitizeDesktopFile(snap, "foo.desktop", desktopContent)
	c.Assert(string(e), Equals, string(desktopExpected))
}

func (s *sanitizeDesktopFileSuite) TestSanitizeDesktopFileAyatana(c *C) {
	snap := &snap.Info{SideInfo: snap.SideInfo{RealName: "snap"}}

	desktopContent := []byte(`[Desktop Entry]
X-SnapInstanceName=snap
Version=1.0
Name=Firefox Web Browser
X-Ayatana-Desktop-Shortcuts=NewWindow;Private

[NewWindow Shortcut Group]
Name=Open a New Window
TargetEnvironment=Unity

[Private Shortcut Group]
Name=Private Mode
TargetEnvironment=Unity
`)

	e := wrappers.SanitizeDesktopFile(snap, "foo.desktop", desktopContent)
	c.Assert(string(e), Equals, string(desktopContent))
}

func (s *sanitizeDesktopFileSuite) TestSanitizeParallelInstancesPlain(c *C) {
	snap, err := snap.InfoFromSnapYaml([]byte(`
name: snap
version: 1.0
apps:
 app:
  command: cmd
`))
	snap.InstanceKey = "bar"
	c.Assert(err, IsNil)
	desktopContent := []byte(`[Desktop Entry]
Name=foo
Exec=snap.app
`)
	df := filepath.Base(snap.Apps["app"].DesktopFile())
	e := wrappers.SanitizeDesktopFile(snap, df, desktopContent)
	c.Assert(string(e), Equals, fmt.Sprintf(`[Desktop Entry]
X-SnapInstanceName=snap_bar
Name=foo
X-SnapAppName=app
Exec=%s/bin/snap_bar.app
`, dirs.SnapMountDir))
}

func (s *sanitizeDesktopFileSuite) TestSanitizeParallelInstancesWithArgs(c *C) {
	snap, err := snap.InfoFromSnapYaml([]byte(`
name: snap
version: 1.0
apps:
 app:
  command: cmd
`))
	snap.InstanceKey = "bar"
	c.Assert(err, IsNil)
	desktopContent := []byte(`[Desktop Entry]
Name=foo
Exec=snap.app %U
`)

	df := filepath.Base(snap.Apps["app"].DesktopFile())
	e := wrappers.SanitizeDesktopFile(snap, df, desktopContent)
	c.Assert(string(e), Equals, fmt.Sprintf(`[Desktop Entry]
X-SnapInstanceName=snap_bar
Name=foo
X-SnapAppName=app
Exec=%s/bin/snap_bar.app %%U
`, dirs.SnapMountDir))
}

func (s *sanitizeDesktopFileSuite) TestDetectAppAndRewriteExecLineInvalid(c *C) {
	snap := &snap.Info{}
	_, _, err := wrappers.DetectAppAndRewriteExecLine(snap, "foo.desktop", "Exec=invalid")
	c.Assert(err, ErrorMatches, `invalid exec command: "invalid"`)
}

func (s *sanitizeDesktopFileSuite) TestDetectAppAndRewriteExecLineOk(c *C) {
	snap, err := snap.InfoFromSnapYaml([]byte(`
name: snap
version: 1.0
apps:
 app:
  command: cmd
`))
	c.Assert(err, IsNil)

	app, newl, err := wrappers.DetectAppAndRewriteExecLine(snap, "foo.desktop", "Exec=snap.app")
	c.Assert(err, IsNil)
	c.Assert(app.Name, Equals, "app")
	c.Assert(newl, Equals, fmt.Sprintf("Exec=%s/bin/snap.app", dirs.SnapMountDir))
}

func (s *sanitizeDesktopFileSuite) TestDetectAppAndRewriteExecLineOkWithCommonID(c *C) {
	snap, err := snap.InfoFromSnapYaml([]byte(`
name: snap
version: 1.0
apps:
 app:
  command: cmd
  common-id: io.snapcraft.app
`))
	c.Assert(err, IsNil)

	app, newl, err := wrappers.DetectAppAndRewriteExecLine(snap, "foo.desktop", "Exec=snap.app")
	c.Assert(err, IsNil)
	c.Assert(app.Name, Equals, "app")
	c.Assert(app.CommonID, Equals, "io.snapcraft.app")
	c.Assert(newl, Equals, fmt.Sprintf("Exec=%s/bin/snap.app", dirs.SnapMountDir))
}

func (s *sanitizeDesktopFileSuite) TestLangLang(c *C) {
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
		// other localised entries
		{"GenericName[xx]=a", true},
		{"Comment[xx]=b", true},
		{"Keywords[xx]=b", true},
		// bad ones
		{"Name[foo=bar", false},
		{"Icon[xx]=bar", false},
	}
	for _, t := range langs {
		c.Assert(wrappers.IsValidDesktopFileLine([]byte(t.line)), Equals, t.isValid)
	}
}

func (s *sanitizeDesktopFileSuite) TestRewriteIconLine(c *C) {
	snap, err := snap.InfoFromSnapYaml([]byte(`
name: snap
version: 1.0
`))
	c.Assert(err, IsNil)

	newl, err := wrappers.RewriteIconLine(snap, "Icon=${SNAP}/icon.png")
	c.Check(err, IsNil)
	c.Check(newl, Equals, "Icon=${SNAP}/icon.png")

	newl, err = wrappers.RewriteIconLine(snap, "Icon=snap.snap.icon")
	c.Check(err, IsNil)
	c.Check(newl, Equals, "Icon=snap.snap.icon")

	newl, err = wrappers.RewriteIconLine(snap, "Icon=other-icon")
	c.Check(err, IsNil)
	c.Check(newl, Equals, "Icon=other-icon")

	snap.InstanceKey = "bar"
	newl, err = wrappers.RewriteIconLine(snap, "Icon=snap.snap.icon")
	c.Check(err, IsNil)
	c.Check(newl, Equals, "Icon=snap.snap_bar.icon")

	_, err = wrappers.RewriteIconLine(snap, "Icon=snap.othersnap.icon")
	c.Check(err, ErrorMatches, `invalid icon name: "snap.othersnap.icon", must start with "snap.snap."`)

	_, err = wrappers.RewriteIconLine(snap, "Icon=/etc/passwd")
	c.Check(err, ErrorMatches, `icon path "/etc/passwd" is not part of the snap`)

	_, err = wrappers.RewriteIconLine(snap, "Icon=${SNAP}/./icon.png")
	c.Check(err, ErrorMatches, `icon path "\${SNAP}/./icon.png" is not canonicalized, did you mean "\${SNAP}/icon.png"\?`)

	_, err = wrappers.RewriteIconLine(snap, "Icon=${SNAP}/../outside/icon.png")
	c.Check(err, ErrorMatches, `icon path "\${SNAP}/../outside/icon.png" is not canonicalized, did you mean "outside/icon.png"\?`)
}

func (s *sanitizeDesktopFileSuite) TestSanitizeParallelInstancesIconName(c *C) {
	snap, err := snap.InfoFromSnapYaml([]byte(`
name: snap
version: 1.0
apps:
 app:
  command: cmd
`))
	snap.InstanceKey = "bar"
	c.Assert(err, IsNil)
	desktopContent := []byte(`[Desktop Entry]
Name=foo
Icon=snap.snap.icon
Exec=snap.app
`)
	df := filepath.Base(snap.Apps["app"].DesktopFile())
	e := wrappers.SanitizeDesktopFile(snap, df, desktopContent)
	c.Assert(string(e), Equals, fmt.Sprintf(`[Desktop Entry]
X-SnapInstanceName=snap_bar
Name=foo
Icon=snap.snap_bar.icon
X-SnapAppName=app
Exec=%s/bin/snap_bar.app
`, dirs.SnapMountDir))
}

func (s *desktopSuite) TestAddRemoveDesktopFiles(c *C) {
	var tests = []struct {
		instance                string
		upstreamDesktopFileName string

		expectedDesktopFileName string
	}{
		// normal cases
		{"", "upstream.desktop", "foo_upstream.desktop"},
		{"instance", "upstream.desktop", "foo+instance_upstream.desktop"},
		// pathological cases are handled
		{"", "instance.desktop", "foo_instance.desktop"},
		{"instance", "instance.desktop", "foo+instance_instance.desktop"},
	}

	for _, t := range tests {
		expectedDesktopFilePath := filepath.Join(dirs.SnapDesktopFilesDir, t.expectedDesktopFileName)
		c.Assert(osutil.FileExists(expectedDesktopFilePath), Equals, false)

		info := snaptest.MockSnap(c, desktopAppYaml, &snap.SideInfo{Revision: snap.R(11)})
		info.InstanceKey = t.instance

		// generate .desktop file in the package baseDir
		baseDir := info.MountDir()
		err := os.MkdirAll(filepath.Join(baseDir, "meta", "gui"), 0755)
		c.Assert(err, IsNil)

		err = os.WriteFile(filepath.Join(baseDir, "meta", "gui", t.upstreamDesktopFileName), mockDesktopFile, 0644)
		c.Assert(err, IsNil)

		err = wrappers.EnsureSnapDesktopFiles([]*snap.Info{info})
		c.Assert(err, IsNil)
		c.Assert(osutil.FileExists(expectedDesktopFilePath), Equals, true)

		// Ensure that the old-style parallel install desktop file was
		// not created.
		if t.instance != "" {
			unexpectedOldStyleDesktopFilePath := strings.Replace(expectedDesktopFilePath, "+", "_", 1)
			c.Assert(osutil.FileExists(unexpectedOldStyleDesktopFilePath), Equals, false)
		}

		// remove it again
		err = wrappers.RemoveSnapDesktopFiles(info)
		c.Assert(err, IsNil)
		c.Assert(osutil.FileExists(expectedDesktopFilePath), Equals, false)
	}
}

func (s *desktopSuite) TestForAllDesktopFilesSkipsSnapdDesktopFiles(c *C) {
	c.Assert(os.MkdirAll(dirs.SnapDesktopFilesDir, 0755), IsNil)

	var mockDesktopFile = []byte(`
[Desktop Entry]
Name=foo
X-SnapInstanceName=foo`)

	desktopFiles := wrappers.SnapdDesktopFileNames
	desktopFiles = append(desktopFiles, "foo_foo.desktop", "foo_bar.desktop")
	for _, df := range desktopFiles {
		c.Assert(os.WriteFile(filepath.Join(dirs.SnapDesktopFilesDir, df), mockDesktopFile, 0644), IsNil)
	}

	found := make(map[string]string, 2)
	err := wrappers.ForAllDesktopFiles(func(base, instanceName string) error {
		found[base] = instanceName
		return nil
	})
	c.Assert(err, IsNil)

	c.Check(found, HasLen, 2)
	c.Check(found["foo_foo.desktop"], Equals, "foo")
	c.Check(found["foo_bar.desktop"], Equals, "foo")
}

func (s *desktopSuite) TestForAllDesktopFilesSkipsBadDesktopFiles(c *C) {
	c.Assert(os.MkdirAll(dirs.SnapDesktopFilesDir, 0755), IsNil)

	var mockEmpty []byte
	var mockNoInstanceName = []byte(`
[Desktop Entry]
Name=foo`)
	var mockInvalid = []byte(`
[Desktop Entry]
[Desktop Entry]`)

	c.Assert(os.WriteFile(filepath.Join(dirs.SnapDesktopFilesDir, "empty.desktop"), mockEmpty, 0644), IsNil)
	c.Assert(os.WriteFile(filepath.Join(dirs.SnapDesktopFilesDir, "no-instance-name.desktop"), mockNoInstanceName, 0644), IsNil)
	c.Assert(os.WriteFile(filepath.Join(dirs.SnapDesktopFilesDir, "invalid.desktop"), mockInvalid, 0644), IsNil)

	err := wrappers.ForAllDesktopFiles(func(base, instanceName string) error {
		return errors.New("unexpected call")
	})
	c.Assert(err, IsNil)
}
