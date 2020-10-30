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

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
)

// Manifest describes content content exported to snaps or the host.
type Manifest struct {
	SnapInstanceName          string        `json:"snap-instance-name"`
	SnapRevision              snap.Revision `json:"snap-revision"` // TODO: change to snap.Revision later
	ExportedForSnapdAsVersion string        `json:"exported-for-snapd-as-version,omitempty"`

	// SourceIsHost is only true if provider of the manifest is not a snap but the classic system.
	// All SourcePath fields, as visible through Sets[*].Exports[*].SourcePath, are absolute names
	// in the host file system.
	SourceIsHost bool `json:"source-is-host,omitempty"`

	// Sets describe groups of exported files.
	Sets map[string]ExportSet `json:"sets"`
}

// ExportSet describes a group of files for a given type of consumer.
type ExportSet struct {
	Name string `json:"name"`
	// ConsumerIsHost is true if an export set provides files usable from the host system.
	ConsumerIsHost bool                    `json:"consumer-is-host,omitempty"`
	Exports        map[string]ExportedFile `json:"exports"`
}

// ExportedFile describes a single file exported from one place to anther.
//
// Currently all files are exported as symbolic links. If this changes a new
// field should be added here, to encode this information.
type ExportedFile struct {
	// Name is the name of the exported file, usually the same as the leaf component of SourcePath.
	Name string `json:"name"`
	// SourcePath is the path of the exported file relative to its source. The
	// ultimate interpretation of this field depends on Manifest.SourceIsHost
	// and ExportSet.ConsumerIsHost fields.
	SourcePath string `json:"source-path"`
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
	switch info.Type() {
	case snap.TypeSnapd:
		return manifestForSnapdSnap(info)
	case snap.TypeOS: // we really mean "core" snap here.
		return manifestForCoreSnap(info)
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
	for _, set := range m.Sets {
		if len(set.Exports) > 0 {
			return false
		}
	}
	return true
}

// createExportedFiles creates all the files constituting the export manifest.
//
// The directory /var/lib/snapd/export/$exportedName/$exportedVersion is created
// if necessary. For each export set in the manifest, additional sub-directory
// is created and populated with symbolic links pointing to the exported files.
//
// The function is idempotent.
func createExportedFiles(manifest *Manifest) error {
	for _, set := range manifest.Sets {
		for _, exported := range set.Exports {
			if err := createExportedFile(manifest, &set, &exported); err != nil {
				return err
			}
		}
	}
	return nil
}

// removeExportedFiles removes all the files constituting the export state.
//
// In addition the path /var/lib/snapd/export/$exportedName/$exportedVersion
// is pruned, removing empty directories if possible.
//
// On failure removal continues and the first error is returned.
func removeExportedFiles(manifest *Manifest) error {
	var firstErr error
	for _, set := range manifest.Sets {
		for _, exported := range set.Exports {
			if err := removeExportedFile(manifest, &set, &exported); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// exportedFilePath returns the path path of an exported file.
func exportedFilePath(manifest *Manifest, set *ExportSet, exported *ExportedFile) string {
	snapInstanceName := manifest.SnapInstanceName
	snapRevision := manifest.SnapRevision.String()
	if manifest.ExportedForSnapdAsVersion != "" { // Exception for core and host
		snapInstanceName = "snapd"
		snapRevision = manifest.ExportedForSnapdAsVersion
	}
	return filepath.Join(dirs.ExportDir, snapInstanceName, snapRevision, set.Name, exported.Name)
}

// exportedFileSourcePath returns the source path that is to be exported.
func exportedFileSourcePath(manifest *Manifest, set *ExportSet, exported *ExportedFile) string {
	// Consumer uses host mount namespace
	if set.ConsumerIsHost {
		if manifest.SourceIsHost {
			// host-to-host, no translation needed.
			return exported.SourcePath
		}
		// snap-to-host
		return filepath.Join(dirs.SnapMountDir, manifest.SnapInstanceName, manifest.SnapRevision.String(), exported.SourcePath)
	}
	// Consumer uses snap mount namespace
	if manifest.SourceIsHost {
		// host-to-snap, access via hostfs.
		return filepath.Join("/var/lib/snapd/hostfs", exported.SourcePath)
	}
	// snap-to-snap, fixed /snap location inside the snap mount namespace.
	return filepath.Join("/snap", manifest.SnapInstanceName, manifest.SnapRevision.String(), exported.SourcePath)
}

// createExportedFile creates an exported file and necessary directories.
//
// The function is idempotent.
//
// Currently all files are exported as a symbolic link.
func createExportedFile(manifest *Manifest, set *ExportSet, exported *ExportedFile) error {
	pathName := exportedFilePath(manifest, set, exported)
	if err := os.MkdirAll(filepath.Dir(pathName), 0755); err != nil {
		return err
	}
	sourcePath := exportedFileSourcePath(manifest, set, exported)
	// When there are additional kinds of exported files, switch on the type of
	// file here. It is possible that some content may be more practical to copy
	// out of a snap than to link to it.
	err := os.Symlink(sourcePath, pathName)
	if err != nil && os.IsExist(err) {
		if actualTarget, _ := os.Readlink(pathName); actualTarget == sourcePath {
			err = nil
		}
	}
	return err
}

// removeExportedFile removes the exported file and prunes any directories.
func removeExportedFile(manifest *Manifest, set *ExportSet, exported *ExportedFile) error {
	pathName := exportedFilePath(manifest, set, exported)
	// When there are additional kinds of exported files, switch on the type of
	// file here. It is possible that some content may be more practical to copy
	// out of a snap than to link to it.
	if err := os.Remove(pathName); err != nil && !os.IsNotExist(err) {
		return err
	}
	// Chomp three levels up: once for export set name, once for exported
	// version (or revision) and finally once more for exported snap name (or
	// snapd). This way we do not need to repeat the special case described in
	// exportedFilePath.
	dir := filepath.Dir(pathName)
	for i := 0; i < 3; i++ {
		os.Remove(dir)
		dir = filepath.Dir(dir)
	}
	return nil
}
