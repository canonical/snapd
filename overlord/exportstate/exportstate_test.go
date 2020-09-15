// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package exportstate_test

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/exportstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type manifestSuite struct {
	testutil.BaseTest
	m *exportstate.Manifest
}

var _ = Suite(&manifestSuite{})

func (s *manifestSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })
	s.m = sampleAbstractManifest.Materialize()
}

func (s *manifestSuite) TestNewManifest(c *C) {
	info := snaptest.MockInfo(c, "name: foo\nversion: 1\n",
		&snap.SideInfo{Revision: snap.Revision{N: 42}})
	m := exportstate.NewManifest(info)
	c.Check(m.Symlinks, HasLen, 0)
}

func (s *manifestSuite) TestIsEmpty(c *C) {
	m := exportstate.Manifest{}
	c.Check(m.IsEmpty(), Equals, true)

	m = exportstate.Manifest{
		Symlinks: []exportstate.SymlinkExport{{
			PrimaryKey: "primary-key",
			SubKey:     "sub-key",
			ExportSet:  "export-set",
			Name:       "symlink-name",
			Target:     "symlink-target",
		},
		},
	}
	c.Check(m.IsEmpty(), Equals, false)
}

func (s *manifestSuite) TestCreateExportedFiles(c *C) {
	err := s.m.CreateExportedFiles()
	c.Assert(err, IsNil)
	checkFiles := func() {
		// Creating symlinks creates the prerequisite directories.
		// The symbolic links point from export set name to a path that is valid in
		// either the host or snap mount namespace.
		c.Check(filepath.Join(
			exportstate.ExportDir, "primary", "sub", "for-snaps", "local-path"),
			testutil.SymlinkTargetEquals, "snap-path")
		c.Check(filepath.Join(
			exportstate.ExportDir, "primary", "sub", "for-snaps", "local-path-2"),
			testutil.SymlinkTargetEquals, "snap-path-2")
		c.Check(filepath.Join(exportstate.ExportDir, "primary", "sub", "for-host", "local-path"),
			testutil.SymlinkTargetEquals, "host-path")
	}
	checkFiles()

	// Calling this over and over is safe.
	err = s.m.CreateExportedFiles()
	c.Assert(err, IsNil)
	checkFiles()
}

func (s *manifestSuite) TestCreateClashSymlinkDifferentTarget(c *C) {
	// If the file system contains symlinks with different targets that clash
	// with the exported content then the operation fails.
	fname := filepath.Join(exportstate.ExportDir, "primary", "sub", "for-snaps", "local-path")
	err := os.MkdirAll(filepath.Dir(fname), 0755)
	c.Assert(err, IsNil)
	err = os.Symlink("wrong-target", fname)
	c.Assert(err, IsNil)
	err = s.m.CreateExportedFiles()
	c.Check(err, ErrorMatches, "symlink snap-path .*/var/lib/snapd/export/primary/sub/for-snaps/local-path: file exists")
}

func (s *manifestSuite) TestCreateSymlinksClashNonSymlink(c *C) {
	// If the file system contains non-symlinks that clash with the exported
	// content then the operation fails.
	fname := filepath.Join(exportstate.ExportDir, "primary", "sub", "for-snaps", "local-path")
	err := os.MkdirAll(filepath.Dir(fname), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(fname, nil, 0644)
	c.Assert(err, IsNil)
	err = s.m.CreateExportedFiles()
	c.Check(err, ErrorMatches, "symlink snap-path .*/var/lib/snapd/export/primary/sub/for-snaps/local-path: file exists")
}

func (s *manifestSuite) TestRemoveExportedFiles(c *C) {
	// Creating and then removing exported files completes successfully.
	err := s.m.CreateExportedFiles()
	c.Assert(err, IsNil)
	err = s.m.RemoveExportedFiles()
	c.Assert(err, IsNil)
	// The symbolic links are removed.
	c.Check(filepath.Join(exportstate.ExportDir,
		"primary", "sub", "for-snaps", "local-path"),
		testutil.FileAbsent)
	c.Check(filepath.Join(exportstate.ExportDir,
		"primary", "sub", "for-snaps", "local-path-2"),
		testutil.FileAbsent)
	c.Check(filepath.Join(exportstate.ExportDir,
		"primary", "sub", "for-host", "local-path"),
		testutil.FileAbsent)

	// The empty directories are pruned.
	c.Check(filepath.Join(exportstate.ExportDir, "primary", "sub", "for-snaps"), testutil.FileAbsent)
	c.Check(filepath.Join(exportstate.ExportDir, "primary", "sub", "for-host"), testutil.FileAbsent)
	c.Check(filepath.Join(exportstate.ExportDir, "primary", "sub"), testutil.FileAbsent)
	c.Check(filepath.Join(exportstate.ExportDir, "primary"), testutil.FileAbsent)

	// Removing exported files doesn't fail if they are no longer present.
	err = s.m.RemoveExportedFiles()
	c.Assert(err, IsNil)

	// Removing exported files does not remove unrelated files and does not stop on
	// subsequent failures to remove non-empty directories.
	err = s.m.CreateExportedFiles()
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(exportstate.ExportDir,
		"primary", "sub", "for-snaps", "unrelated"), nil, 0644)
	c.Assert(err, IsNil)
	err = os.MkdirAll(filepath.Join(exportstate.ExportDir,
		"primary", "sub-2"), 755)
	c.Assert(err, IsNil)

	err = s.m.RemoveExportedFiles()
	c.Assert(err, IsNil)
	c.Check(filepath.Join(exportstate.ExportDir,
		"primary", "sub", "for-snaps", "unrelated"), testutil.FilePresent)
}

type exportstateSuite struct {
	testutil.BaseTest
	st *state.State
	am exportstate.AbstractManifest
	m  exportstate.Manifest
}

var _ = Suite(&exportstateSuite{
	am: exportstate.AbstractManifest{
		PrimaryKey: "primary",
		SubKey:     "sub",
		ExportSets: map[exportstate.ExportSetName][]exportstate.ExportEntry{
			"export-set": {
				&testExportEntry{
					pathInExportSet:                  "local-path",
					pathInHostMountNS:                "host-path",
					pathInSnapMountNS:                "snap-path",
					isExportedPathValidInHostMountNS: false,
				},
			},
		},
	},
	m: exportstate.Manifest{
		Symlinks: []exportstate.SymlinkExport{
			{
				PrimaryKey: "primary",
				SubKey:     "sub",
				ExportSet:  "export-set",
				Name:       "local-path",
				Target:     "snap-path",
			},
		},
	},
})

func (s *exportstateSuite) SetUpTest(c *C) {
	s.st = state.New(nil)
	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })
}

func (s *exportstateSuite) TestSetAddingState(c *C) {
	st := s.st
	st.Lock()
	defer st.Unlock()

	// Set associates snap revision with a manifest.
	exportstate.Set(st, "snap-name", snap.R(42), &s.m)

	var exportsRaw map[string]interface{}
	st.Get("exports", &exportsRaw)
	expected := map[string]interface{}{
		"snap-name/42": map[string]interface{}{
			"symlinks": []interface{}{
				map[string]interface{}{
					"primary-key": "primary",
					"sub-key":     "sub",
					"export-set":  "export-set",
					"name":        "local-path",
					"target":      "snap-path",
				},
			},
		},
	}
	c.Check(exportsRaw, DeepEquals, expected)

	// Set copes with "exports" key being present.
	exportstate.Set(st, "snap-name", snap.R(42), &s.m)
	st.Get("exports", &exportsRaw)
	c.Check(exportsRaw, DeepEquals, expected)
}

func (s *exportstateSuite) TestSetRemovingState(c *C) {
	st := s.st
	st.Lock()
	defer st.Unlock()

	// Set used with a nil SnapRevisionExportState removes the export
	// state of the specific snap instance name and revision, without
	// altering state of other snaps or other revisions.
	st.Set("exports", map[string]interface{}{
		"other-snap/42": map[string]interface{}{
			"unrelated": "stuff",
		},
		"snap-name/41": map[string]interface{}{
			"unrelated": "stuff",
		},
		"snap-name/42": map[string]interface{}{
			"symlinks": []interface{}{
				map[string]interface{}{
					"primary-key": "primary",
					"sub-key":     "sub",
					"export-set":  "export-set",
					"name":        "local-path",
					"target":      "snap-path",
				},
			},
		},
	})
	exportstate.Set(st, "snap-name", snap.R(42), nil)

	var exportsRaw map[string]interface{}
	st.Get("exports", &exportsRaw)
	c.Check(exportsRaw, DeepEquals, map[string]interface{}{
		"other-snap/42": map[string]interface{}{
			"unrelated": "stuff",
		},
		"snap-name/41": map[string]interface{}{
			"unrelated": "stuff",
		},
	})
}

func (s *exportstateSuite) TestGetWithoutState(c *C) {
	st := s.st
	st.Lock()
	defer st.Unlock()

	// Get fails with ErrNoState when "exports" are not in the state.
	var m exportstate.Manifest
	err := exportstate.Get(st, "snap-name", snap.R(42), &m)
	c.Assert(err, Equals, state.ErrNoState)
}

func (s *exportstateSuite) TestGetWithoutStateRevisionState(c *C) {
	st := s.st
	st.Lock()
	defer st.Unlock()

	// Get fails with ErrNoState when "exports" does not contain any data
	// for the given snap instance name and revision.
	st.Set("exports", map[string]interface{}{})
	var m exportstate.Manifest
	err := exportstate.Get(st, "snap-name", snap.R(42), &m)
	c.Assert(err, Equals, state.ErrNoState)
}

func (s *exportstateSuite) TestGetReadingRevisionState(c *C) {
	st := s.st
	st.Lock()
	defer st.Unlock()

	// Get returns the stored snap manifest for given snap revision.
	st.Set("exports", map[string]interface{}{
		"snap-name/42": map[string]interface{}{
			"symlinks": []interface{}{
				map[string]interface{}{
					"primary-key": "primary",
					"sub-key":     "sub",
					"export-set":  "export-set",
					"name":        "local-path",
					"target":      "snap-path",
				},
			},
		},
	})
	var m exportstate.Manifest
	err := exportstate.Get(st, "snap-name", snap.R(42), &m)
	c.Assert(err, IsNil)
	c.Check(m, DeepEquals, s.m)
}

func (s *exportstateSuite) TestCurrentSubKeySymlinkPath(c *C) {
	path := exportstate.CurrentSubKeySymlinkPath("primary")
	c.Check(path, Equals, dirs.GlobalRootDir+"/var/lib/snapd/export/primary/current")
}

func (s *exportstateSuite) TestRemoveCurrentSubKey(c *C) {
	// It is not an error to remove the current subkey link
	// if it does not exist.
	err := exportstate.RemoveCurrentSubKey(s.am.PrimaryKey)
	c.Assert(err, IsNil)

	// Removing the current subkey symlink works correctly.
	err = s.am.Materialize().CreateExportedFiles()
	c.Assert(err, IsNil)
	err = exportstate.SetCurrentSubKey(s.am.PrimaryKey, s.am.SubKey)
	c.Assert(err, IsNil)
	c.Check(filepath.Join(exportstate.ExportDir, s.am.PrimaryKey, "current"),
		testutil.SymlinkTargetEquals, s.am.SubKey)
	err = exportstate.RemoveCurrentSubKey(s.am.PrimaryKey)
	c.Assert(err, IsNil)
	c.Check(filepath.Join(exportstate.ExportDir, s.am.PrimaryKey, "current"),
		testutil.FileAbsent)
}

func (s *exportstateSuite) TestSetCurrentSubKey(c *C) {
	// Current subkey cannot be selected without exporting the content first
	// but the ENOENT error is silently ignored.
	err := exportstate.SetCurrentSubKey(s.am.PrimaryKey, s.am.SubKey)
	c.Check(err, IsNil)
	c.Check(filepath.Join(exportstate.ExportDir, s.am.PrimaryKey, "current"), testutil.FileAbsent)

	// With a manifest in place, we can set the current subkey at will.
	err = s.am.Materialize().CreateExportedFiles()
	c.Assert(err, IsNil)
	err = exportstate.SetCurrentSubKey(s.am.PrimaryKey, s.am.SubKey)
	c.Assert(err, IsNil)
	c.Check(filepath.Join(exportstate.ExportDir, s.am.PrimaryKey, "current"),
		testutil.SymlinkTargetEquals, s.am.SubKey)

	// The current subkey can be replaced to point to another value.
	err = exportstate.SetCurrentSubKey(s.am.PrimaryKey, "other-"+s.am.SubKey)
	c.Assert(err, IsNil)
	c.Check(filepath.Join(exportstate.ExportDir, s.am.PrimaryKey, "current"),
		testutil.SymlinkTargetEquals, "other-"+s.am.SubKey)
}

func (s *exportstateSuite) TestCurrentSubKeyForSnap(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	coreInfo := snaptest.MockInfo(c, "name: core\nversion: 1\ntype: os\n",
		&snap.SideInfo{Revision: snap.Revision{N: 1}})
	snapdInfo := snaptest.MockInfo(c, "name: snapd\nversion: 1\ntype: snapd\n",
		&snap.SideInfo{Revision: snap.Revision{N: 2}})

	// Because we have both core and snapd installed, snapd with revision 1 wins.
	s.AddCleanup(exportstate.MockSnapStateCurrentInfo(func(givenState *state.State, snapName string) (*snap.Info, error) {
		switch snapName {
		case "core":
			return coreInfo, nil
		case "snapd":
			return snapdInfo, nil
		default:
			panic("unexpected")
		}
	}))
	primaryKey, subKey, err := exportstate.ManifestKeys(s.st, "core")
	c.Assert(err, IsNil)
	c.Check(primaryKey, Equals, "snapd")
	c.Check(subKey, Equals, "2")

	// Because we have both core and snapd installed, core with revision 1 wins.
	s.AddCleanup(exportstate.MockSnapStateCurrentInfo(func(givenState *state.State, snapName string) (*snap.Info, error) {
		switch snapName {
		case "core":
			return coreInfo, nil
		case "snapd":
			return nil, &snap.NotInstalledError{}
		default:
			panic("unexpected")
		}
	}))
	primaryKey, subKey, err = exportstate.ManifestKeys(s.st, "core")
	c.Assert(err, IsNil)
	c.Check(primaryKey, Equals, "snapd")
	c.Check(subKey, Equals, "core_1")

	// Non-special snaps just use their revision as sub-key.
	s.AddCleanup(exportstate.MockSnapStateCurrentInfo(func(givenState *state.State, snapName string) (*snap.Info, error) {
		return snaptest.MockInfo(c, "name: foo\nversion: 1\n",
			&snap.SideInfo{Revision: snap.Revision{N: 42}}), nil
	}))
	primaryKey, subKey, err = exportstate.ManifestKeys(s.st, "foo")
	c.Assert(err, IsNil)
	c.Check(primaryKey, Equals, "foo")
	c.Check(subKey, Equals, "42")

	// TODO: test broken snapd/core

	// Non-special snaps that are installed as an instance combine the instance
	// key and the revision.
	s.AddCleanup(exportstate.MockSnapStateCurrentInfo(func(givenState *state.State, snapName string) (*snap.Info, error) {
		info := snaptest.MockInfo(c, "name: foo\nversion: 1\n",
			&snap.SideInfo{Revision: snap.Revision{N: 42}})
		info.InstanceKey = "instance"
		return info, nil
	}))
	primaryKey, subKey, err = exportstate.ManifestKeys(s.st, "foo")
	c.Assert(err, IsNil)
	c.Check(primaryKey, Equals, "foo")
	c.Check(subKey, Equals, "42_instance")
}

func (s *exportstateSuite) TestElectSubKeyForSnapdTools(c *C) {
	// When both snapd and core are present, snapd dominates.
	subKey := exportstate.ElectSubKeyForSnapdTools("snapd_subkey", "core_subkey")
	c.Check(subKey, Equals, "snapd_subkey")

	// When either only snapd or core is present, it is used.
	subKey = exportstate.ElectSubKeyForSnapdTools("snapd_subkey", "")
	c.Check(subKey, Equals, "snapd_subkey")
	subKey = exportstate.ElectSubKeyForSnapdTools("", "core_subkey")
	c.Check(subKey, Equals, "core_subkey")

	// On classic systems when neither snap is present, host tools are used.
	s.AddCleanup(release.MockOnClassic(true))
	subKey = exportstate.ElectSubKeyForSnapdTools("", "")
	c.Check(subKey, Equals, "host")

	// On core systems when neither snap is present, no tool provider is used.
	s.AddCleanup(release.MockOnClassic(false))
	subKey = exportstate.ElectSubKeyForSnapdTools("", "")
	c.Check(subKey, Equals, "")
}

func (s *exportstateSuite) TestCurrentSnapdAndCoreInfo(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	// Fetching current snapd and core revisions does not fail when the state is empty.
	snapdInfo, coreInfo, err := exportstate.CurrentSnapdAndCoreInfo(s.st)
	c.Assert(err, IsNil)
	c.Check(snapdInfo, IsNil)
	c.Check(coreInfo, IsNil)

	s.AddCleanup(exportstate.MockSnapStateCurrentInfo(func(st *state.State, snapName string) (*snap.Info, error) {
		c.Assert(st, Equals, s.st)
		var snapInfo *snap.Info
		switch snapName {
		case "core":
			snapInfo = snaptest.MockInfo(c, "name: core\nversion: 1\ntype: os\n", nil)
		case "snapd":
			snapInfo = snaptest.MockInfo(c, "name: snapd\nversion: 1\ntype: snapd\n", nil)
		default:
			panic("unexpected")
		}
		return snapInfo, nil
	}))
	snapdInfo, coreInfo, err = exportstate.CurrentSnapdAndCoreInfo(s.st)
	c.Assert(err, IsNil)
	c.Check(snapdInfo.SnapName(), Equals, "snapd")
	c.Check(coreInfo.SnapName(), Equals, "core")
}

type testExportEntry struct {
	pathInExportSet                  string
	pathInHostMountNS                string
	pathInSnapMountNS                string
	isExportedPathValidInHostMountNS bool
}

func (tee *testExportEntry) PathInExportSet() string {
	return tee.pathInExportSet
}

func (tee *testExportEntry) PathInHostMountNS() string {
	return tee.pathInHostMountNS
}

func (tee *testExportEntry) PathInSnapMountNS() string {
	return tee.pathInSnapMountNS
}

func (tee *testExportEntry) IsExportedPathValidInHostMountNS() bool {
	return tee.isExportedPathValidInHostMountNS
}

var sampleAbstractManifest = exportstate.AbstractManifest{
	PrimaryKey: "primary",
	SubKey:     "sub",
	ExportSets: map[exportstate.ExportSetName][]exportstate.ExportEntry{
		"for-snaps": {
			&testExportEntry{
				pathInExportSet:                  "local-path",
				pathInHostMountNS:                "host-path",
				pathInSnapMountNS:                "snap-path",
				isExportedPathValidInHostMountNS: false,
			},
			&testExportEntry{
				pathInExportSet:                  "local-path-2",
				pathInHostMountNS:                "host-path2",
				pathInSnapMountNS:                "snap-path-2",
				isExportedPathValidInHostMountNS: false,
			},
		},
		"for-host": {
			&testExportEntry{
				pathInExportSet:                  "local-path",
				pathInHostMountNS:                "host-path",
				pathInSnapMountNS:                "snap-path",
				isExportedPathValidInHostMountNS: true,
			},
		},
	},
}

type abstractManifestSuite struct {
	testutil.BaseTest
	snapdInfo *snap.Info
	coreInfo  *snap.Info
	otherInfo *snap.Info
	am        exportstate.AbstractManifest
}

var _ = Suite(&abstractManifestSuite{})

const snapdYaml = `
name: snapd
version: 1
type: snapd
`

const coreYaml = `
name: core
version: 1
type: os
`

// XXX: Do we need to handle ubuntu-core?

const otherYaml = `
name: other
version: 1
`

func (s *abstractManifestSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })

	s.snapdInfo = snaptest.MockInfo(c, snapdYaml, &snap.SideInfo{
		Revision: snap.Revision{N: 1},
	})
	s.coreInfo = snaptest.MockInfo(c, coreYaml, &snap.SideInfo{
		Revision: snap.Revision{N: 2},
	})
	s.otherInfo = snaptest.MockInfo(c, otherYaml, &snap.SideInfo{
		Revision: snap.Revision{N: 3},
	})

	s.am = sampleAbstractManifest
}

func (s *abstractManifestSuite) TestMaterialize(c *C) {
	m := s.am.Materialize()
	c.Check(m.Symlinks, HasLen, 3)
	c.Check(m.Symlinks, testutil.Contains, exportstate.SymlinkExport{
		PrimaryKey: "primary",
		SubKey:     "sub",
		ExportSet:  "for-snaps",
		Name:       "local-path",
		Target:     "snap-path",
	})
	c.Check(m.Symlinks, testutil.Contains, exportstate.SymlinkExport{
		PrimaryKey: "primary",
		SubKey:     "sub",
		ExportSet:  "for-snaps",
		Name:       "local-path-2",
		Target:     "snap-path-2",
	})
	c.Check(m.Symlinks, testutil.Contains, exportstate.SymlinkExport{
		PrimaryKey: "primary",
		SubKey:     "sub",
		ExportSet:  "for-host",
		Name:       "local-path",
		Target:     "host-path",
	})
}

func (s *abstractManifestSuite) TestSnapdManifestWithDefaultSnapMountDir(c *C) {
	s.AddCleanup(release.MockReleaseInfo(&release.OS{ID: "ubuntu"}))
	s.AddCleanup(release.MockOnClassic(true))

	am := exportstate.NewAbstractManifest(s.snapdInfo)
	c.Assert(am, NotNil)
	c.Check(am.PrimaryKey, Equals, "snapd")
	c.Check(am.SubKey, Equals, "1")

	tools, ok := am.ExportSets["tools"]
	c.Assert(ok, Equals, true)
	c.Assert(tools, HasLen, 1) // Tools are checked elsewhere.
}

func (s *abstractManifestSuite) TestSnapdManifestWithAltSnapMountDir(c *C) {
	s.AddCleanup(release.MockReleaseInfo(&release.OS{ID: "fedora"}))
	s.AddCleanup(release.MockOnClassic(true))

	am := exportstate.NewAbstractManifest(s.snapdInfo)
	c.Assert(am, NotNil)
	c.Check(am.PrimaryKey, Equals, "snapd")
	c.Check(am.SubKey, Equals, "1")
	tools, ok := am.ExportSets["tools"]
	c.Assert(ok, Equals, true)
	c.Assert(tools, HasLen, 1) // Tools are checked elsewhere.
}

func (s *abstractManifestSuite) TestCoreManifestWithDefaultSnapMountDir(c *C) {
	s.AddCleanup(release.MockReleaseInfo(&release.OS{ID: "ubuntu"}))
	s.AddCleanup(release.MockOnClassic(true))

	am := exportstate.NewAbstractManifest(s.coreInfo)
	c.Assert(am, NotNil)
	c.Check(am.PrimaryKey, Equals, "snapd")
	c.Check(am.SubKey, Equals, "core_2")
	tools, ok := am.ExportSets["tools"]
	c.Assert(ok, Equals, true)
	c.Assert(tools, HasLen, 1) // Tools are checked elsewhere.
}

func (s *abstractManifestSuite) TestCoreManifestWithAltSnapMountDir(c *C) {
	s.AddCleanup(release.MockReleaseInfo(&release.OS{ID: "fedora"}))
	s.AddCleanup(release.MockOnClassic(true))

	am := exportstate.NewAbstractManifest(s.coreInfo)
	c.Assert(am, NotNil)
	c.Check(am.PrimaryKey, Equals, "snapd")
	c.Check(am.SubKey, Equals, "core_2")
	tools, ok := am.ExportSets["tools"]
	c.Assert(ok, Equals, true)
	c.Assert(tools, HasLen, 1) // Tools are checked elsewhere.
}

func (s *abstractManifestSuite) TestOtherInfoManifest(c *C) {
	am := exportstate.NewAbstractManifest(s.otherInfo)
	c.Assert(am, NotNil)
	c.Check(am.PrimaryKey, Equals, "other")
	c.Check(am.SubKey, Equals, "3")
	c.Check(am.ExportSets, HasLen, 0)
}
