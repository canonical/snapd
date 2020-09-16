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
	m  exportstate.Manifest
}

var _ = Suite(&exportstateSuite{
	m: exportstate.Manifest{
		PrimaryKey: "primary-key",
		SubKey:     "sub-key",
		Symlinks: []exportstate.SymlinkExport{{
			PrimaryKey: "primary-key",
			SubKey:     "sub-key",
			ExportSet:  "export-set",
			Name:       "symlink-name",
			Target:     "symlink-target",
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
			"primary-key": "primary-key",
			"sub-key":     "sub-key",
			"symlinks": []interface{}{
				map[string]interface{}{
					"primary-key": "primary-key",
					"sub-key":     "sub-key",
					"export-set":  "export-set",
					"name":        "symlink-name",
					"target":      "symlink-target",
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
			"primary-key": "primary-key",
			"sub-key":     "sub-key",
			"symlinks": []interface{}{
				map[string]interface{}{
					"primary-key": "primary-key",
					"sub-key":     "sub-key",
					"export-set":  "export-set",
					"name":        "symlink-name",
					"target":      "symlink-target",
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
			"primary-key": "primary-key",
			"sub-key":     "sub-key",
			"symlinks": []interface{}{
				map[string]interface{}{
					"primary-key": "primary-key",
					"sub-key":     "sub-key",
					"export-set":  "export-set",
					"name":        "symlink-name",
					"target":      "symlink-target",
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
	err := exportstate.RemoveCurrentSubKey(s.m.PrimaryKey)
	c.Assert(err, IsNil)

	// Removing the current subkey symlink works correctly.
	err = s.m.CreateExportedFiles()
	c.Assert(err, IsNil)
	err = exportstate.SetCurrentSubKey(s.m.PrimaryKey, s.m.SubKey)
	c.Assert(err, IsNil)
	c.Check(filepath.Join(exportstate.ExportDir, s.m.PrimaryKey, "current"),
		testutil.SymlinkTargetEquals, s.m.SubKey)
	err = exportstate.RemoveCurrentSubKey(s.m.PrimaryKey)
	c.Assert(err, IsNil)
	c.Check(filepath.Join(exportstate.ExportDir, s.m.PrimaryKey, "current"),
		testutil.FileAbsent)
}

func (s *exportstateSuite) TestSetCurrentSubKey(c *C) {
	// Current subkey cannot be selected without exporting the content first
	// but the ENOENT error is silently ignored.
	err := exportstate.SetCurrentSubKey(s.m.PrimaryKey, s.m.SubKey)
	c.Check(err, IsNil)
	c.Check(filepath.Join(exportstate.ExportDir, s.m.PrimaryKey, "current"), testutil.FileAbsent)

	// With a manifest in place, we can set the current subkey at will.
	err = s.m.CreateExportedFiles()
	c.Assert(err, IsNil)
	err = exportstate.SetCurrentSubKey(s.m.PrimaryKey, s.m.SubKey)
	c.Assert(err, IsNil)
	c.Check(filepath.Join(exportstate.ExportDir, s.m.PrimaryKey, "current"),
		testutil.SymlinkTargetEquals, s.m.SubKey)

	// The current subkey can be replaced to point to another value.
	err = exportstate.SetCurrentSubKey(s.m.PrimaryKey, "other-"+s.m.SubKey)
	c.Assert(err, IsNil)
	c.Check(filepath.Join(exportstate.ExportDir, s.m.PrimaryKey, "current"),
		testutil.SymlinkTargetEquals, "other-"+s.m.SubKey)
}

func (s *exportstateSuite) TestManifestKeys(c *C) {
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

	// On classic systems with disabled re-exec host wins over snaps.
	s.AddCleanup(release.MockOnClassic(true))
	os.Setenv("SNAP_REEXEC", "0")
	s.AddCleanup(func() { os.Unsetenv("SNAP_REEXEC") })
	subKey = exportstate.ElectSubKeyForSnapdTools("snapd_subkey", "core_subkey")
	c.Check(subKey, Equals, "host")

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
