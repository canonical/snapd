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

package exportstate

import (
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
)

// Manifest describes content content exported to snaps or the host.
type Manifest struct {
	SnapName        string          `json:"snap-name"`
	ExportedVersion string          `json:"exported-version"`
	Symlinks        []SymlinkExport `json:"symlinks,omitempty"`
}

// NewManifestForSnap returns the manifest of a given snap.
//
// Currently only the core and snapd snaps export any content to the system. As
// such the export manifest is not embedded into the snap meta-data but instead
// computed here.
//
// Both snapd and core snaps export content under the snap name "snapd", using
// the export set name "tools". The revision is mangled, for "snapd" it is used
// directly. For "core" it is transformed to "core_$revision".
func NewManifestForSnap(info *snap.Info) *Manifest {
	// XXX: should we use WellKnownSnapID here? Probably not as this must work for
	// unsigned snaps as well. Alternatively, should we look at snap type, as we
	// have unique values for both snapd and core.
	switch info.SnapName() {
	case "snapd":
		return manifestForSnapdSnap(info)
	case "core":
		return manifestForCoreSnap(info)
		// XXX: do we need to handle ubuntu-core?
	default:
		return manifestForRegularSnap(info)
	}
}

// NewManifestForHost returns the export manifest of the host system.
//
// Ubuntu Core systems do not have an export manifest. Classic systems have an export
// manifest which describes snapd tools from the classic package.
func NewManifestForHost() *Manifest {
	if release.OnClassic {
		return manifestForClassicSystem()
	}
	return manifestForCoreSystem()
}

// IsEmpty returns true if a manifest describes no content.
func (m *Manifest) IsEmpty() bool {
	return len(m.Symlinks) == 0
}

// CreateExportedFiles creates all the files constituting the export manifest.
//
// The directory /var/lib/snapd/export/$snapName/$exportedVersion is created
// if necessary. For each export set in the manifest, additional sub-directory
// is created and populated with symbolic links pointing to the exported files.
//
// The function is idempotent.
func (m *Manifest) CreateExportedFiles() error {
	for _, s := range m.Symlinks {
		if err := s.Create(); err != nil {
			return err
		}
	}
	return nil
}

// RemoveExportedFiles removes all the files constituting the export state.
//
// In addition the path /var/lib/snapd/export/$snapName/$exportedVersion
// is pruned, removing empty directories if possible.
//
// On failure removal continues and the first error is returned.
func (m *Manifest) RemoveExportedFiles() error {
	var firstErr error
	for _, s := range m.Symlinks {
		if err := s.Remove(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// SymlinkExport describes content exported as symbolic link.
type SymlinkExport struct {
	SnapName        string `json:"snap-name"`
	ExportedVersion string `json:"exported-version"`
	ExportSet       string `json:"export-set"`
	Name            string `json:"name"`
	Target          string `json:"target"`
}

// PathName returns the full path of the symbolic link.
func (s *SymlinkExport) PathName() string {
	return filepath.Join(ExportDir, s.SnapName, s.ExportedVersion, s.ExportSet, s.Name)
}

// Create creates a symbolic link and necessary directories.
//
// The function is idempotent.
func (s *SymlinkExport) Create() error {
	pathName := s.PathName()
	if err := os.MkdirAll(filepath.Dir(pathName), 0755); err != nil {
		return err
	}
	err := os.Symlink(s.Target, pathName)
	if err != nil && os.IsExist(err) {
		if actualTarget, _ := os.Readlink(pathName); actualTarget == s.Target {
			err = nil
		}
	}
	return err
}

// Remove removes the symbolic link and prunes any directories.
func (s *SymlinkExport) Remove() error {
	if err := os.Remove(s.PathName()); err != nil && !os.IsNotExist(err) {
		return err
	}
	// XXX: or iterate upwards until we reach ExportDir
	os.Remove(filepath.Join(ExportDir, s.SnapName, s.ExportedVersion, s.ExportSet))
	os.Remove(filepath.Join(ExportDir, s.SnapName, s.ExportedVersion))
	os.Remove(filepath.Join(ExportDir, s.SnapName))
	return nil
}

// exportSetSymlinks returns symlink exports for given snap and export entries.
func exportSetSymlinks(snapName, snapRev, exportSetName string, entries []*ExportEntry) []SymlinkExport {
	symlinks := make([]SymlinkExport, 0, len(entries))
	for _, entry := range entries {
		var target string
		if entry.IsExportedPathValidInHostMountNS {
			target = entry.PathInHostMountNS
		} else {
			target = entry.PathInSnapMountNS
		}
		symlinks = append(symlinks, SymlinkExport{
			SnapName:        snapName,
			ExportedVersion: snapRev,
			ExportSet:       exportSetName,
			Name:            entry.PathInExportSet,
			Target:          target,
		})
	}
	return symlinks
}

// ExportEntry describes a single file placed in a specific export set.
//
// The original file is described by two paths. PathInHostMountNS is is valid
// in the host mount namespace while PathInSnapMountNS is valid in the per-snap
// mount namespace. The original file is exported by creating a symbolic link
// under PathInExportSet, which is relative to the export set that the entry
// belongs to, to one of the two other paths.
//
// If IsExportedPathValidInHostMountNS returns true then PathInHostMountNS is
// used, if it returns false PathInSnapMountNS is used instead.
//
// This distinction enables exporting files that are consumed by either other
// snaps or by the classic system.
type ExportEntry struct {
	PathInExportSet   string
	PathInHostMountNS string
	PathInSnapMountNS string

	IsExportedPathValidInHostMountNS bool
}

// NewExportedHostFile returns an entry describing a file from the classic file system.
func NewExportedHostFile(pathOnHost, pathInExportSet string) *ExportEntry {
	// TODO: consider using this to describe nvidia libraries from the host.
	return &ExportEntry{
		PathInHostMountNS: pathOnHost,
		PathInSnapMountNS: filepath.Join("/var/lib/snapd/hostfs", pathOnHost),
		PathInExportSet:   pathInExportSet,
	}
}

// NewExportedSnapFile returns an entry describing a file stored inside a snap.
func NewExportedSnapFile(snap *snap.Info, pathInSnap, pathInExportSet string) *ExportEntry {
	return &ExportEntry{
		PathInHostMountNS: filepath.Join(snap.MountDir(), pathInSnap),
		PathInSnapMountNS: filepath.Join("/snap", snap.InstanceName(), snap.Revision.String(), pathInSnap),
		PathInExportSet:   pathInExportSet,
	}
}
