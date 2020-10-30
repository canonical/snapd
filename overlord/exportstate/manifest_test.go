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
		snaptest.MockInfo(c, "name: foo\nversion: 1\n", &snap.SideInfo{Revision: snap.R(42)}))
	c.Check(m.SnapInstanceName, Equals, "foo")
	c.Check(m.SnapRevision, Equals, snap.R(42))
	c.Check(m.ExportedForSnapdAsVersion, Equals, "")
	c.Check(m.Sets, HasLen, 0)
}

const snapdYaml = `
name: snapd
version: 1
type: snapd
`

func (s *manifestSuite) TestNewManifestForSnapdSnap(c *C) {
	m := exportstate.NewManifestForSnap(
		snaptest.MockInfo(c, snapdYaml, &snap.SideInfo{Revision: snap.R(1)}))
	c.Check(m.SnapInstanceName, Equals, "snapd")
	c.Check(m.SnapRevision, Equals, snap.R(1))
	c.Check(m.ExportedForSnapdAsVersion, Equals, "")
	c.Check(m.SourceIsHost, Equals, false)
	c.Check(len(m.Sets) > 0, Equals, true)
	// Details checked in special_test.go
}

const coreYaml = `
name: core
version: 1
type: os
`

func (s *manifestSuite) TestNewManifestForCoreSnap(c *C) {
	m := exportstate.NewManifestForSnap(
		snaptest.MockInfo(c, coreYaml, &snap.SideInfo{Revision: snap.R(2)}))
	c.Check(m.SnapInstanceName, Equals, "core")
	c.Check(m.SnapRevision, Equals, snap.R(2))
	c.Check(m.SourceIsHost, Equals, false)
	c.Check(m.ExportedForSnapdAsVersion, Equals, "core_2")
	c.Check(len(m.Sets) > 0, Equals, true)
	// Details checked in special_test.go
}

func (s *manifestSuite) TestNewManifestForHost(c *C) {
	s.AddCleanup(release.MockOnClassic(true))
	m := exportstate.NewManifestForHost()
	c.Check(m.SnapInstanceName, Equals, "")
	c.Check(m.SnapRevision, Equals, snap.R(0))
	c.Check(m.SourceIsHost, Equals, true)
	c.Check(m.ExportedForSnapdAsVersion, Equals, "host")
	c.Check(len(m.Sets) > 0, Equals, true)
	// Details checked in special_test.go

	s.AddCleanup(release.MockOnClassic(false))
	m = exportstate.NewManifestForHost()
	c.Check(m.SnapInstanceName, Equals, "")
	c.Check(m.SnapRevision, Equals, snap.R(0))
	c.Check(m.SourceIsHost, Equals, true)
	c.Check(m.ExportedForSnapdAsVersion, Equals, "host")
	c.Check(m.Sets, HasLen, 0)
}

func (s *manifestSuite) TestIsEmpty(c *C) {
	m := exportstate.Manifest{}
	c.Check(m.IsEmpty(), Equals, true)

	m = exportstate.Manifest{
		Sets: map[string]exportstate.ExportSet{
			"set-name": {},
		},
	}
	c.Check(m.IsEmpty(), Equals, true)

	m = exportstate.Manifest{
		Sets: map[string]exportstate.ExportSet{
			"set-name": {
				Exports: map[string]exportstate.ExportedFile{
					"name": {
						Name:       "file-name",
						SourcePath: "source-path",
					},
				},
			},
		},
	}
	c.Check(m.IsEmpty(), Equals, false)
}

func (s *manifestSuite) TestCreateExportedFiles(c *C) {
	m := &exportstate.Manifest{
		SnapInstanceName: "snap-instance-name",
		SnapRevision:     snap.R(42),
		Sets: map[string]exportstate.ExportSet{
			"export-set-a": {
				Name: "export-set-a",
				Exports: map[string]exportstate.ExportedFile{
					"symlink-name-1": {
						Name:       "symlink-name-1",
						SourcePath: "source-path-1",
					},
				},
			},
			"export-set-b": {
				Name: "export-set-b",
				Exports: map[string]exportstate.ExportedFile{
					"symlink-name-2": {
						Name:       "symlink-name-2",
						SourcePath: "source-path-2",
					},
					"symlink-name-3": {
						Name:       "symlink-name-3",
						SourcePath: "source-path-3",
					},
				},
			},
		},
	}
	err := exportstate.CreateExportedFiles(m)
	c.Assert(err, IsNil)
	checkFiles := func() {
		// Creating symlinks creates the prerequisite directories.
		// The symbolic links point from export set name to a path that is valid in
		// either the host or exported mount namespace.
		c.Check(filepath.Join(
			dirs.ExportDir, "snap-instance-name", "42", "export-set-a", "symlink-name-1"),
			testutil.SymlinkTargetEquals, "/snap/snap-instance-name/42/source-path-1")
		c.Check(filepath.Join(
			dirs.ExportDir, "snap-instance-name", "42", "export-set-b", "symlink-name-2"),
			testutil.SymlinkTargetEquals, "/snap/snap-instance-name/42/source-path-2")
		c.Check(filepath.Join(
			dirs.ExportDir, "snap-instance-name", "42", "export-set-b", "symlink-name-3"),
			testutil.SymlinkTargetEquals, "/snap/snap-instance-name/42/source-path-3")
	}
	checkFiles()

	// Calling this over and over is safe.
	err = exportstate.CreateExportedFiles(m)
	c.Assert(err, IsNil)
	checkFiles()
}

func (s *manifestSuite) TestCreateClashSymlinkDifferentTarget(c *C) {
	// If the file system contains symlinks with different targets that clash
	// with the exported content then the operation fails.
	pathName := filepath.Join(dirs.ExportDir, "snap-instance-name", "42", "export-set", "symlink-name")
	err := os.MkdirAll(filepath.Dir(pathName), 0755)
	c.Assert(err, IsNil)
	err = os.Symlink("wrong-target", pathName)
	c.Assert(err, IsNil)

	m := &exportstate.Manifest{
		SnapInstanceName: "snap-instance-name",
		SnapRevision:     snap.R(42),
		Sets: map[string]exportstate.ExportSet{
			"export-set": {
				Name: "export-set",
				Exports: map[string]exportstate.ExportedFile{
					"symlink-name": {
						Name:       "symlink-name",
						SourcePath: "source-path",
					},
				},
			},
		},
	}
	err = exportstate.CreateExportedFiles(m)
	c.Check(err, ErrorMatches, "symlink /snap/snap-instance-name/42/source-path .*/var/lib/snapd/export/snap-instance-name/42/export-set/symlink-name: file exists")
}

func (s *manifestSuite) TestCreateSymlinksClashNonSymlink(c *C) {
	// If the file system contains non-symlinks that clash with the exported
	// content then the operation fails.
	pathName := filepath.Join(dirs.ExportDir, "snap-instance-name", "42", "export-set", "symlink-name")
	err := os.MkdirAll(filepath.Dir(pathName), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(pathName, nil, 0644)
	c.Assert(err, IsNil)

	m := &exportstate.Manifest{
		SnapInstanceName: "snap-instance-name",
		SnapRevision:     snap.R(42),
		Sets: map[string]exportstate.ExportSet{
			"export-set": {
				Name: "export-set",
				Exports: map[string]exportstate.ExportedFile{
					"symlink-name": {
						Name:       "symlink-name",
						SourcePath: "source-path",
					},
				},
			},
		},
	}
	err = exportstate.CreateExportedFiles(m)
	c.Check(err, ErrorMatches, "symlink /snap/snap-instance-name/42/source-path .*/var/lib/snapd/export/snap-instance-name/42/export-set/symlink-name: file exists")
}

func (s *manifestSuite) TestRemoveExportedFiles(c *C) {
	m := &exportstate.Manifest{
		SnapInstanceName: "snap-instance-name",
		SnapRevision:     snap.R(42),
		Sets: map[string]exportstate.ExportSet{
			"export-set-a": {
				Name: "export-set-a",
				Exports: map[string]exportstate.ExportedFile{
					"symlink-name-1": {
						Name:       "symlink-name-1",
						SourcePath: "source-path-1",
					},
				},
			},
			"export-set-b": {
				Name: "export-set-b",
				Exports: map[string]exportstate.ExportedFile{
					"symlink-name-2": {
						Name:       "symlink-name-2",
						SourcePath: "source-path-2",
					},
					"symlink-name-3": {
						Name:       "symlink-name-3",
						SourcePath: "source-path-3",
					},
				},
			},
		},
	} // Creating and then removing exported files completes successfully.
	err := exportstate.CreateExportedFiles(m)
	c.Assert(err, IsNil)
	err = exportstate.RemoveExportedFiles(m)
	c.Assert(err, IsNil)
	// The symbolic links are removed.
	c.Check(filepath.Join(dirs.ExportDir, "snap-instance-name", "42", "export-set-a", "symlink-name-1"),
		testutil.FileAbsent)
	c.Check(filepath.Join(dirs.ExportDir, "snap-instance-name", "42", "export-set-b", "symlink-name-2"),
		testutil.FileAbsent)
	c.Check(filepath.Join(dirs.ExportDir, "snap-instance-name", "42", "export-set-b", "symlink-name-3"),
		testutil.FileAbsent)

	// The empty directories are pruned.
	c.Check(filepath.Join(dirs.ExportDir, "snap-instance-name", "42", "export-set-a"), testutil.FileAbsent)
	c.Check(filepath.Join(dirs.ExportDir, "snap-instance-name", "42", "export-set-b"), testutil.FileAbsent)
	c.Check(filepath.Join(dirs.ExportDir, "snap-instance-name", "42"), testutil.FileAbsent)
	c.Check(filepath.Join(dirs.ExportDir, "snap-instance-name"), testutil.FileAbsent)

	// Removing exported files doesn't fail if they are no longer present.
	err = exportstate.RemoveExportedFiles(m)
	c.Assert(err, IsNil)

	// Removing exported files does not remove unrelated files and does not stop on
	// subsequent failures to remove non-empty directories.
	err = exportstate.CreateExportedFiles(m)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(dirs.ExportDir,
		"snap-instance-name", "42", "export-set-a", "unrelated"), nil, 0644)
	c.Assert(err, IsNil)

	err = exportstate.RemoveExportedFiles(m)
	c.Assert(err, IsNil)
	c.Check(filepath.Join(dirs.ExportDir,
		"snap-instance-name", "42", "export-set-a", "unrelated"), testutil.FilePresent)
}

func (s *manifestSuite) TestRemoveExportedFilesSpecialCaseForSnapd(c *C) {
	m := &exportstate.Manifest{
		SnapInstanceName:          "snap-instance-name",
		SnapRevision:              snap.R(42),
		ExportedForSnapdAsVersion: "special",
		Sets: map[string]exportstate.ExportSet{
			"export-set": {
				Name: "export-set",
				Exports: map[string]exportstate.ExportedFile{
					"symlink-name": {
						Name:       "symlink-name",
						SourcePath: "source-path",
					},
				},
			},
		},
	} // Creating and then removing exported files completes successfully.
	err := exportstate.CreateExportedFiles(m)
	c.Assert(err, IsNil)
	c.Check(filepath.Join(dirs.ExportDir, "snapd", "special", "export-set", "symlink-name"),
		testutil.SymlinkTargetMatches, "source-path")

	err = exportstate.RemoveExportedFiles(m)
	c.Assert(err, IsNil)
	// The symbolic links are removed.
	c.Check(filepath.Join(dirs.ExportDir, "snapd", "special", "export-set", "symlink-name"),
		testutil.FileAbsent)

	// The empty directories are pruned.
	c.Check(filepath.Join(dirs.ExportDir, "snapd", "special", "export-set"), testutil.FileAbsent)
	c.Check(filepath.Join(dirs.ExportDir, "snapd", "special", "export-set"), testutil.FileAbsent)
	c.Check(filepath.Join(dirs.ExportDir, "snapd", "special"), testutil.FileAbsent)
	c.Check(filepath.Join(dirs.ExportDir, "snapd"), testutil.FileAbsent)
}

func (s *manifestSuite) TestExportedFilePathsNormalCase(c *C) {
	// Pretend to be on Fedora with the alternate snap mount directory to ensure
	// we observe the right paths that look identical in primary snap mount
	// directory.
	s.AddCleanup(release.MockReleaseInfo(&release.OS{ID: "fedora"}))
	s.AddCleanup(release.MockOnClassic(true))

	typicalManifest := &exportstate.Manifest{
		SnapInstanceName: "snap-instance-name",
		SnapRevision:     snap.R(42),
		Sets: map[string]exportstate.ExportSet{
			"export-set-for-snaps": {
				Name: "export-set-for-snaps",
				Exports: map[string]exportstate.ExportedFile{
					"exported-name": {
						Name:       "exported-name",
						SourcePath: "source-path",
					},
				},
			},
			"export-set-for-host": {
				Name:           "export-set-for-host",
				ConsumerIsHost: true,
				Exports: map[string]exportstate.ExportedFile{
					"exported-name": {
						Name:       "exported-name",
						SourcePath: "source-path",
					},
				},
			},
		},
	}
	manifest := typicalManifest
	// Snap sharing to other snaps.
	set := manifest.Sets["export-set-for-snaps"]
	export := set.Exports["exported-name"]
	path := exportstate.ExportedFilePath(manifest, &set, &export)
	c.Check(path, Equals, filepath.Join(dirs.ExportDir,
		"/snap-instance-name/42/export-set-for-snaps/exported-name"))
	source := exportstate.ExportedFileSourcePath(manifest, &set, &export)
	c.Check(source, Equals, filepath.Join(dirs.CoreSnapMountDir,
		// Shared file uses core snap mount directory, ensuring the link is
		// traversable from the snap mount namespace.
		"/snap-instance-name/42/source-path"))

	// Snap sharing to the host
	set = manifest.Sets["export-set-for-host"]
	export = set.Exports["exported-name"]
	path = exportstate.ExportedFilePath(manifest, &set, &export)
	c.Check(path, Equals, filepath.Join(dirs.ExportDir,
		"/snap-instance-name/42/export-set-for-host/exported-name"))
	source = exportstate.ExportedFileSourcePath(manifest, &set, &export)
	c.Check(source, Equals, filepath.Join(dirs.SnapMountDir,
		// Shared file uses system snap mount directory, ensuring the link is
		// traversable from the host mount namespace.
		"/snap-instance-name/42/source-path"))

}

func (s *manifestSuite) TestExportedFilePathsSpecialCase(c *C) {
	// Pretend to be on Fedora with the alternate snap mount directory to ensure
	// we observe the right paths that look identical in primary snap mount
	// directory.
	s.AddCleanup(release.MockReleaseInfo(&release.OS{ID: "fedora"}))
	s.AddCleanup(release.MockOnClassic(true))

	// Special cases set ExportedForSnapdAsVersion and pretend to be the snapd
	// snap.
	specialManifest := &exportstate.Manifest{
		SnapInstanceName:          "snap-instance-name",
		SnapRevision:              snap.R(42),
		ExportedForSnapdAsVersion: "exported-for-snapd-as-version",
		Sets: map[string]exportstate.ExportSet{
			"export-set-for-snaps": {
				Name: "export-set-for-snaps",
				Exports: map[string]exportstate.ExportedFile{
					"exported-name": {
						Name:       "exported-name",
						SourcePath: "source-path",
					},
				},
			},
			"export-set-for-host": {
				Name:           "export-set-for-host",
				ConsumerIsHost: true,
				Exports: map[string]exportstate.ExportedFile{
					"exported-name": {
						Name:       "exported-name",
						SourcePath: "source-path",
					},
				},
			},
		},
	}
	manifest := specialManifest
	// Snap sharing to other snaps.
	set := manifest.Sets["export-set-for-snaps"]
	export := set.Exports["exported-name"]
	path := exportstate.ExportedFilePath(manifest, &set, &export)
	c.Check(path, Equals, filepath.Join(dirs.ExportDir,
		"/snapd/exported-for-snapd-as-version/export-set-for-snaps/exported-name"))
	source := exportstate.ExportedFileSourcePath(manifest, &set, &export)
	c.Check(source, Equals, filepath.Join(dirs.CoreSnapMountDir,
		"/snap-instance-name/42/source-path"))

	// Snap sharing to the host
	set = manifest.Sets["export-set-for-host"]
	export = set.Exports["exported-name"]
	path = exportstate.ExportedFilePath(manifest, &set, &export)
	c.Check(path, Equals, filepath.Join(dirs.ExportDir,
		"/snapd/exported-for-snapd-as-version/export-set-for-host/exported-name"))
	source = exportstate.ExportedFileSourcePath(manifest, &set, &export)
	c.Check(source, Equals, filepath.Join(dirs.SnapMountDir,
		// Shared file uses system snap mount directory, ensuring the link is
		// traversable from the host mount namespace.
		"/snap-instance-name/42/source-path"))

	// The host can also pretend to be snapd.
	hostManifest := &exportstate.Manifest{
		// Note that when SourceIsHost we really must set
		// ExportedForSnapdAsVersion, at least until there are more cases that
		// warrant inclusion of another modelled special case.
		SourceIsHost:              true,
		ExportedForSnapdAsVersion: "exported-for-snapd-as-version",
		Sets: map[string]exportstate.ExportSet{
			"export-set-for-snaps": {
				Name: "export-set-for-snaps",
				Exports: map[string]exportstate.ExportedFile{
					"exported-name": {
						Name:       "exported-name",
						SourcePath: "source-path",
					},
				},
			},
			"export-set-for-host": {
				Name:           "export-set-for-host",
				ConsumerIsHost: true,
				Exports: map[string]exportstate.ExportedFile{
					"exported-name": {
						Name:       "exported-name",
						SourcePath: "source-path",
					},
				},
			},
		},
	}
	manifest = hostManifest
	// Snap sharing to other snaps.
	set = manifest.Sets["export-set-for-snaps"]
	export = set.Exports["exported-name"]
	path = exportstate.ExportedFilePath(manifest, &set, &export)
	c.Check(path, Equals, filepath.Join(dirs.ExportDir,
		"/snapd/exported-for-snapd-as-version/export-set-for-snaps/exported-name"))
	source = exportstate.ExportedFileSourcePath(manifest, &set, &export)
	// Host-to-snap are shared via hostfs mount point.
	c.Check(source, Equals, "/var/lib/snapd/hostfs/source-path")

	// Snap sharing to the host
	set = manifest.Sets["export-set-for-host"]
	export = set.Exports["exported-name"]
	path = exportstate.ExportedFilePath(manifest, &set, &export)
	c.Check(path, Equals, filepath.Join(dirs.ExportDir,
		"/snapd/exported-for-snapd-as-version/export-set-for-host/exported-name"))
	source = exportstate.ExportedFileSourcePath(manifest, &set, &export)
	// Host-to-host can be shared directly.
	c.Check(source, Equals, "source-path")
}
