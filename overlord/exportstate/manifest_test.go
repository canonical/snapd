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

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/exportstate"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"

	. "gopkg.in/check.v1"
)

type manifestSuite struct {
	testutil.BaseTest
}

var _ = Suite(&manifestSuite{})

func (s *manifestSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })
}

func (s *manifestSuite) TestNewManifestForRegularSnap(c *C) {
	m := exportstate.NewManifestForSnap(
		snaptest.MockInfo(c, "name: foo\nversion: 1\n",
			&snap.SideInfo{Revision: snap.Revision{N: 42}}))
	c.Check(m.PrimaryKey, Equals, "foo")
	c.Check(m.SubKey, Equals, "42")
	c.Check(m.Symlinks, HasLen, 0)
}

const snapdYaml = `
name: snapd
version: 1
type: snapd
`

func (s *manifestSuite) TestNewManifestForSnapdSnap(c *C) {
	m := exportstate.NewManifestForSnap(
		snaptest.MockInfo(c, snapdYaml, &snap.SideInfo{
			Revision: snap.Revision{N: 1}}))
	c.Check(m.PrimaryKey, Equals, "snapd")
	c.Check(m.SubKey, Equals, "1")
	c.Check(len(m.Symlinks) > 0, Equals, true)
	// Details checked in special_test.go
}

const coreYaml = `
name: core
version: 1
type: os
`

func (s *manifestSuite) TestNewManifestForCoreSnap(c *C) {
	m := exportstate.NewManifestForSnap(
		snaptest.MockInfo(c, coreYaml, &snap.SideInfo{
			Revision: snap.Revision{N: 2}}))
	c.Check(m.PrimaryKey, Equals, "snapd")
	c.Check(m.SubKey, Equals, "core_2")
	c.Check(len(m.Symlinks) > 0, Equals, true)
	// Details checked in special_test.go
}

func (s *manifestSuite) TestNewManifestForHost(c *C) {
	s.AddCleanup(release.MockOnClassic(true))
	m := exportstate.NewManifestForHost()
	c.Check(m.PrimaryKey, Equals, "snapd")
	c.Check(m.SubKey, Equals, "host")
	c.Check(len(m.Symlinks) > 0, Equals, true)
	// Details checked in special_test.go

	s.AddCleanup(release.MockOnClassic(false))
	m = exportstate.NewManifestForHost()
	c.Check(m.PrimaryKey, Equals, "snapd")
	c.Check(m.SubKey, Equals, "host")
	c.Check(m.Symlinks, HasLen, 0)
}

func (s *manifestSuite) TestIsEmpty(c *C) {
	m := exportstate.Manifest{}
	c.Check(m.IsEmpty(), Equals, true)

	m = exportstate.Manifest{
		PrimaryKey: "primary-key",
		SubKey:     "sub-key",
		Symlinks: []exportstate.SymlinkExport{{
			PrimaryKey: "primary-key",
			SubKey:     "sub-key",
			ExportSet:  "export-set",
			Name:       "symlink-name",
			Target:     "symlink-target",
		}},
	}
	c.Check(m.IsEmpty(), Equals, false)
}

func (s *manifestSuite) TestCreateExportedFiles(c *C) {
	m := exportstate.Manifest{
		PrimaryKey: "primary-key",
		SubKey:     "sub-key",
		Symlinks: []exportstate.SymlinkExport{{
			// Export set A, only entry
			PrimaryKey: "primary-key",
			SubKey:     "sub-key",
			ExportSet:  "export-set-a",
			Name:       "symlink-name-1",
			Target:     "symlink-target-1",
		}, {
			// Export set B, first entry
			PrimaryKey: "primary-key",
			SubKey:     "sub-key",
			ExportSet:  "export-set-b",
			Name:       "symlink-name-2",
			Target:     "symlink-target-2",
		}, {
			// Export set B, second entry
			PrimaryKey: "primary-key",
			SubKey:     "sub-key",
			ExportSet:  "export-set-b",
			Name:       "symlink-name-3",
			Target:     "symlink-target-3",
		}},
	}
	err := m.CreateExportedFiles()
	c.Assert(err, IsNil)
	checkFiles := func() {
		// Creating symlinks creates the prerequisite directories.
		// The symbolic links point from export set name to a path that is valid in
		// either the host or snap mount namespace.
		c.Check(filepath.Join(
			exportstate.ExportDir, "primary-key", "sub-key", "export-set-a", "symlink-name-1"),
			testutil.SymlinkTargetEquals, "symlink-target-1")
		c.Check(filepath.Join(
			exportstate.ExportDir, "primary-key", "sub-key", "export-set-b", "symlink-name-2"),
			testutil.SymlinkTargetEquals, "symlink-target-2")
		c.Check(filepath.Join(exportstate.ExportDir, "primary-key", "sub-key", "export-set-b", "symlink-name-3"),
			testutil.SymlinkTargetEquals, "symlink-target-3")
	}
	checkFiles()

	// Calling this over and over is safe.
	err = m.CreateExportedFiles()
	c.Assert(err, IsNil)
	checkFiles()
}

func (s *manifestSuite) TestCreateClashSymlinkDifferentTarget(c *C) {
	// If the file system contains symlinks with different targets that clash
	// with the exported content then the operation fails.
	pathName := filepath.Join(exportstate.ExportDir, "primary-key", "sub-key", "export-set", "symlink-name")
	err := os.MkdirAll(filepath.Dir(pathName), 0755)
	c.Assert(err, IsNil)
	err = os.Symlink("wrong-target", pathName)
	c.Assert(err, IsNil)

	m := exportstate.Manifest{
		PrimaryKey: "primary-key",
		SubKey:     "sub-key",
		Symlinks: []exportstate.SymlinkExport{{
			PrimaryKey: "primary-key",
			SubKey:     "sub-key",
			ExportSet:  "export-set",
			Name:       "symlink-name",
			Target:     "symlink-target",
		}},
	}
	err = m.CreateExportedFiles()
	c.Check(err, ErrorMatches, "symlink symlink-target .*/var/lib/snapd/export/primary-key/sub-key/export-set/symlink-name: file exists")
}

func (s *manifestSuite) TestCreateSymlinksClashNonSymlink(c *C) {
	// If the file system contains non-symlinks that clash with the exported
	// content then the operation fails.
	pathName := filepath.Join(exportstate.ExportDir, "primary-key", "sub-key", "export-set", "symlink-name")
	err := os.MkdirAll(filepath.Dir(pathName), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(pathName, nil, 0644)
	c.Assert(err, IsNil)

	m := exportstate.Manifest{
		PrimaryKey: "primary-key",
		SubKey:     "sub-key",
		Symlinks: []exportstate.SymlinkExport{{
			PrimaryKey: "primary-key",
			SubKey:     "sub-key",
			ExportSet:  "export-set",
			Name:       "symlink-name",
			Target:     "symlink-target",
		}},
	}
	err = m.CreateExportedFiles()
	c.Check(err, ErrorMatches, "symlink symlink-target .*/var/lib/snapd/export/primary-key/sub-key/export-set/symlink-name: file exists")
}

func (s *manifestSuite) TestRemoveExportedFiles(c *C) {
	m := exportstate.Manifest{
		PrimaryKey: "primary-key",
		SubKey:     "sub-key",
		Symlinks: []exportstate.SymlinkExport{{
			// Export set A, only entry
			PrimaryKey: "primary-key",
			SubKey:     "sub-key",
			ExportSet:  "export-set-a",
			Name:       "symlink-name-1",
			Target:     "symlink-target-1",
		}, {
			// Export set B, first entry
			PrimaryKey: "primary-key",
			SubKey:     "sub-key",
			ExportSet:  "export-set-b",
			Name:       "symlink-name-2",
			Target:     "symlink-target-2",
		}, {
			// Export set B, second entry
			PrimaryKey: "primary-key",
			SubKey:     "sub-key",
			ExportSet:  "export-set-b",
			Name:       "symlink-name-3",
			Target:     "symlink-target-3",
		}},
	}
	// Creating and then removing exported files completes successfully.
	err := m.CreateExportedFiles()
	c.Assert(err, IsNil)
	err = m.RemoveExportedFiles()
	c.Assert(err, IsNil)
	// The symbolic links are removed.
	c.Check(filepath.Join(exportstate.ExportDir,
		"primary-key", "sub-key", "export-set-a", "symlink-name-1"),
		testutil.FileAbsent)
	c.Check(filepath.Join(exportstate.ExportDir,
		"primary-key", "sub-key", "export-set-b", "symlink-name-2"),
		testutil.FileAbsent)
	c.Check(filepath.Join(exportstate.ExportDir,
		"primary-key", "sub-key", "export-set-b", "symlink-name-3"),
		testutil.FileAbsent)

	// The empty directories are pruned.
	c.Check(filepath.Join(exportstate.ExportDir, "primary-key", "sub-key", "export-set-a"), testutil.FileAbsent)
	c.Check(filepath.Join(exportstate.ExportDir, "primary-key", "sub-key", "export-set-b"), testutil.FileAbsent)
	c.Check(filepath.Join(exportstate.ExportDir, "primary-key", "sub-key"), testutil.FileAbsent)
	c.Check(filepath.Join(exportstate.ExportDir, "primary-key"), testutil.FileAbsent)

	// Removing exported files doesn't fail if they are no longer present.
	err = m.RemoveExportedFiles()
	c.Assert(err, IsNil)

	// Removing exported files does not remove unrelated files and does not stop on
	// subsequent failures to remove non-empty directories.
	err = m.CreateExportedFiles()
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(exportstate.ExportDir,
		"primary-key", "sub-key", "export-set-a", "unrelated"), nil, 0644)
	c.Assert(err, IsNil)

	err = m.RemoveExportedFiles()
	c.Assert(err, IsNil)
	c.Check(filepath.Join(exportstate.ExportDir,
		"primary-key", "sub-key", "export-set-a", "unrelated"), testutil.FilePresent)
}
