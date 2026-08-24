// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) Canonical Ltd
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

package export

import (
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
)

// InterfaceRoot returns the directory holding every export unit and the
// export.sources manifest for the given interface:
//
//	/var/lib/snapd/export/system/<ifaceName>/
//
// The "system/" component is safe from collisions with per-snap export
// trees added by a future non-system-snap plug user, because "system" is a
// reserved snap name (see overlord/snapstate.checkInstallPreconditions).
//
// This directory is owned by the export backend, except for the sibling
// /var/lib/snapd/export/system_<snap>_<slot>_<iface>.library-source files
// written directly under /var/lib/snapd/export/ by the configfiles backend
// (see systemLibrarySourcePath in interfaces/builtin/helpers.go): garbage
// collection must only ever touch subtrees of InterfaceRoot, never
// /var/lib/snapd/export/ itself.
func InterfaceRoot(ifaceName string) string {
	return filepath.Join(dirs.SnapExportDirUnder(dirs.GlobalRootDir), "system", ifaceName)
}

// UnitDir returns the directory holding the files exported by a single unit
// (see UnitName) of the given interface.
func UnitDir(ifaceName, unit string) string {
	return filepath.Join(InterfaceRoot(ifaceName), unit)
}

// UnitTmpDir returns the temporary directory a unit is materialised into
// before being atomically renamed into place at UnitDir. It is a sibling of
// UnitDir (same parent directory) so that the rename is a same-filesystem,
// same-directory rename.
func UnitTmpDir(ifaceName, unit string) string {
	return UnitDir(ifaceName, unit) + ".tmp"
}

// ManifestPath returns the path to the export.sources manifest for the
// given interface. The manifest lists, one per line, the paths (relative to
// InterfaceRoot) of every file that is currently part of the interface's
// exported state; any unit directory under InterfaceRoot not referenced by
// it is garbage.
func ManifestPath(ifaceName string) string {
	return filepath.Join(InterfaceRoot(ifaceName), "export.sources")
}
