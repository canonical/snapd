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

type exportstateSuite struct {
	testutil.BaseTest
	st *state.State
	m  *exportstate.Manifest
}

var _ = Suite(&exportstateSuite{
	m: &exportstate.Manifest{
		SnapInstanceName: "snap-instance-name",
		SnapRevision:     snap.R(42),
		Sets: map[string]exportstate.ExportSet{
			"set-name": {
				Name: "set-name",
				Exports: map[string]exportstate.ExportedFile{
					"file-name": {
						Name:       "file-name",
						SourcePath: "source-path",
					},
				},
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
	// The manifest below is artificial but models all the fields.
	m := &exportstate.Manifest{
		SnapInstanceName:          "snap-instance-name",
		SnapRevision:              snap.R(42),
		ExportedForSnapdAsVersion: "exported-for-snapd-as-version",
		SourceIsHost:              true,
		Sets: map[string]exportstate.ExportSet{
			"set-name": {
				Name:           "set-name",
				ConsumerIsHost: true,
				Exports: map[string]exportstate.ExportedFile{
					"file-name": {
						Name:       "file-name",
						SourcePath: "source-path",
					},
				},
			},
		},
	}
	exportstate.Set(st, "snap-instance-name", snap.R(42), m)

	var exportsRaw map[string]interface{}
	st.Get("exports", &exportsRaw)
	expected := map[string]interface{}{
		"snap-instance-name/42": map[string]interface{}{
			"snap-instance-name":            "snap-instance-name",
			"snap-revision":                 "42",
			"exported-for-snapd-as-version": "exported-for-snapd-as-version",
			"source-is-host":                true,
			"sets": map[string]interface{}{
				"set-name": map[string]interface{}{
					"name":             "set-name",
					"consumer-is-host": true,
					"exports": map[string]interface{}{
						"file-name": map[string]interface{}{
							"name":        "file-name",
							"source-path": "source-path",
						},
					},
				},
			},
		},
	}
	c.Check(exportsRaw, DeepEquals, expected)

	// Set copes with "exports" key being present.
	exportstate.Set(st, "snap-instance-name", snap.R(42), m)
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
		"snap-instance-name/42": map[string]interface{}{
			"snap-instance-name": "snap-instance-name",
			"snap-revision":      "snap-revision",
			"sets": map[string]interface{}{
				"set-name": map[string]interface{}{
					"name": "set-name",
					"exports": map[string]interface{}{
						"file-name": map[string]interface{}{
							"name":        "file-name",
							"source-path": "source-path",
						},
					},
				},
			},
		},
	})
	exportstate.Set(st, "snap-instance-name", snap.R(42), nil)

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
	err := exportstate.Get(st, "snap-instance-name", snap.R(42), &m)
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
	err := exportstate.Get(st, "snap-instance-name", snap.R(42), &m)
	c.Assert(err, Equals, state.ErrNoState)
}

func (s *exportstateSuite) TestGetReadingRevisionState(c *C) {
	st := s.st
	st.Lock()
	defer st.Unlock()

	// Get returns the stored snap manifest for given snap revision.
	// The manifest below is artificial but models all the fields.
	expected := exportstate.Manifest{
		SnapInstanceName:          "snap-instance-name",
		SnapRevision:              snap.R(42),
		ExportedForSnapdAsVersion: "exported-for-snapd-as-version",
		SourceIsHost:              true,
		Sets: map[string]exportstate.ExportSet{
			"set-name": {
				Name:           "set-name",
				ConsumerIsHost: true,
				Exports: map[string]exportstate.ExportedFile{
					"file-name": {
						Name:       "file-name",
						SourcePath: "source-path",
					},
				},
			},
		},
	}
	st.Set("exports", map[string]interface{}{
		"snap-name/42": map[string]interface{}{
			"snap-instance-name":            "snap-instance-name",
			"snap-revision":                 "42",
			"exported-for-snapd-as-version": "exported-for-snapd-as-version",
			"source-is-host":                true,
			"sets": map[string]interface{}{
				"set-name": map[string]interface{}{
					"name":             "set-name",
					"consumer-is-host": true,
					"exports": map[string]interface{}{
						"file-name": map[string]interface{}{
							"name":        "file-name",
							"source-path": "source-path",
						},
					},
				},
			},
		},
	})
	var m exportstate.Manifest
	err := exportstate.Get(st, "snap-name", snap.R(42), &m)
	c.Assert(err, IsNil)
	c.Check(m, DeepEquals, expected)
}

func (s *exportstateSuite) TestCurrentExportedVersionSymlinkPath(c *C) {
	path := exportstate.ExportedVersionSymlinkPath("snap-instance-name")
	c.Check(path, Equals, dirs.GlobalRootDir+"/var/lib/snapd/export/snap-instance-name/current")
}

func (s *exportstateSuite) TestRemoveCurrentExportedVersion(c *C) {
	// It is not an error to remove the current version link
	// if it does not exist.
	err := exportstate.UpdateExportedVersion(s.m.SnapInstanceName, "")
	c.Assert(err, IsNil)

	// Removing the current version symlink works correctly.
	err = exportstate.CreateExportedFiles(s.m)
	c.Assert(err, IsNil)
	err = exportstate.UpdateExportedVersion(s.m.SnapInstanceName, s.m.SnapRevision.String())
	c.Assert(err, IsNil)
	c.Check(filepath.Join(dirs.ExportDir, s.m.SnapInstanceName, "current"),
		testutil.SymlinkTargetEquals, s.m.SnapRevision.String())
	err = exportstate.UpdateExportedVersion(s.m.SnapInstanceName, "")
	c.Assert(err, IsNil)
	c.Check(filepath.Join(dirs.ExportDir, s.m.SnapInstanceName, "current"),
		testutil.FileAbsent)
}

func (s *exportstateSuite) TestSetCurrentExportedVersion(c *C) {
	// Current version cannot be selected without exporting the content first
	// but the ENOENT error is silently ignored.
	err := exportstate.UpdateExportedVersion(s.m.SnapInstanceName, s.m.SnapRevision.String())
	c.Check(err, IsNil)
	c.Check(filepath.Join(dirs.ExportDir, s.m.SnapInstanceName, "current"), testutil.FileAbsent)

	// With a manifest in place, we can set the current version at will.
	err = exportstate.CreateExportedFiles(s.m)
	c.Assert(err, IsNil)
	err = exportstate.UpdateExportedVersion(s.m.SnapInstanceName, s.m.SnapRevision.String())
	c.Assert(err, IsNil)
	c.Check(filepath.Join(dirs.ExportDir, s.m.SnapInstanceName, "current"),
		testutil.SymlinkTargetEquals, s.m.SnapRevision.String())

	// The current version can be replaced to point to another value.
	err = exportstate.UpdateExportedVersion(s.m.SnapInstanceName, "other-"+s.m.SnapRevision.String())
	c.Assert(err, IsNil)
	c.Check(filepath.Join(dirs.ExportDir, s.m.SnapInstanceName, "current"),
		testutil.SymlinkTargetEquals, "other-"+s.m.SnapRevision.String())
}

func (s *exportstateSuite) TestExportedNameVersion(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	coreInfo := snaptest.MockInfo(c, "name: core\nversion: 1\ntype: os\n",
		&snap.SideInfo{Revision: snap.R(1)})
	snapdInfo := snaptest.MockInfo(c, "name: snapd\nversion: 1\ntype: snapd\n",
		&snap.SideInfo{Revision: snap.R(2)})

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
	exportedName, exportedVersion, err := exportstate.ExportedNameVersion(s.st, "core")
	c.Assert(err, IsNil)
	c.Check(exportedName, Equals, "snapd")
	c.Check(exportedVersion, Equals, "2")

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
	exportedName, exportedVersion, err = exportstate.ExportedNameVersion(s.st, "core")
	c.Assert(err, IsNil)
	c.Check(exportedName, Equals, "snapd")
	c.Check(exportedVersion, Equals, "core_1")

	// Non-special snaps just use their revision as exported-version.
	s.AddCleanup(exportstate.MockSnapStateCurrentInfo(func(givenState *state.State, snapName string) (*snap.Info, error) {
		return snaptest.MockInfo(c, "name: foo\nversion: 1\n",
			&snap.SideInfo{Revision: snap.R(42)}), nil
	}))
	exportedName, exportedVersion, err = exportstate.ExportedNameVersion(s.st, "foo")
	c.Assert(err, IsNil)
	c.Check(exportedName, Equals, "foo")
	c.Check(exportedVersion, Equals, "42")

	// TODO: test broken snapd/core

	// Non-special snaps that are installed as an instance combine the instance
	// key and the revision.
	s.AddCleanup(exportstate.MockSnapStateCurrentInfo(func(givenState *state.State, snapName string) (*snap.Info, error) {
		info := snaptest.MockInfo(c, "name: foo\nversion: 1\n",
			&snap.SideInfo{Revision: snap.R(42)})
		info.InstanceKey = "instance"
		return info, nil
	}))
	exportedName, exportedVersion, err = exportstate.ExportedNameVersion(s.st, "foo")
	c.Assert(err, IsNil)
	c.Check(exportedName, Equals, "foo_instance")
	c.Check(exportedVersion, Equals, "42")
}

func (s *exportstateSuite) TestSelectExportedVersionForSnapdTools(c *C) {
	// When both snapd and core are present, snapd dominates.
	exportedVersion := exportstate.SelectExportedVersionForSnapdTools("snapd_version", "core_version")
	c.Check(exportedVersion, Equals, "snapd_version")

	// When either only snapd or core is present, it is used.
	exportedVersion = exportstate.SelectExportedVersionForSnapdTools("snapd_version", "")
	c.Check(exportedVersion, Equals, "snapd_version")
	exportedVersion = exportstate.SelectExportedVersionForSnapdTools("", "core_version")
	c.Check(exportedVersion, Equals, "core_version")

	// On classic systems when neither snap is present, host tools are used.
	s.AddCleanup(release.MockOnClassic(true))
	exportedVersion = exportstate.SelectExportedVersionForSnapdTools("", "")
	c.Check(exportedVersion, Equals, "host")

	// On core systems when neither snap is present, no tool provider is used.
	s.AddCleanup(release.MockOnClassic(false))
	exportedVersion = exportstate.SelectExportedVersionForSnapdTools("", "")
	c.Check(exportedVersion, Equals, "")

	// On classic systems with disabled re-exec host wins over snaps.
	s.AddCleanup(release.MockOnClassic(true))
	os.Setenv("SNAP_REEXEC", "0")
	s.AddCleanup(func() { os.Unsetenv("SNAP_REEXEC") })
	exportedVersion = exportstate.SelectExportedVersionForSnapdTools("snapd_version", "core_version")
	c.Check(exportedVersion, Equals, "host")

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
