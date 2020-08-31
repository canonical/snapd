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
	"fmt"
	"path/filepath"

	"github.com/snapcore/snapd/snap"
)

// exportManifestForSnap returns the export manifest, if any, for a given snap.
//
// Currently only the core and snapd snaps export any content to the system. As
// such the export manifest is not embedded into the snap meta-data but instead
// computed here.
//
// Both snapd and core snaps export content under the snap name "snapd", using
// the export set name "tools". The revision is mangled, for "snapd" it is used
// directly. For "core" it is transformed to "core_$revision".
func exportManifestForSnap(info *snap.Info) *ExportManifest {
	// XXX: should we use WellKnownSnapID here? Probably not as this must work for
	// unsigned snaps as well. Alternatively, should we look at snap type, as we
	// have unique values for both snapd and core.
	switch info.SnapName() {
	case "snapd":
		return &ExportManifest{
			PrimaryKey: "snapd",
			// The snapd snap cannot be installed in parallel and
			// thus do not have an instance key.
			SubKey: info.Revision.String(),
			ExportSets: map[ExportSetName][]ExportEntry{
				snapdTools: exportedSnapToolsFromSnapdOrCore(info),
			},
		}
	case "core":
		return &ExportManifest{
			// The core snap contains embedded copy of snapd.
			PrimaryKey: "snapd",
			// The core snap cannot be installed in parallel and
			// does not have an instance key.  As a special
			// exception, the sub key is prefixed with "core_", to
			// indicate that snapd is packaged inside the core
			// snap.
			SubKey: fmt.Sprintf("core_%s", info.Revision),
			ExportSets: map[ExportSetName][]ExportEntry{
				snapdTools: exportedSnapToolsFromSnapdOrCore(info),
			},
		}
	}
	return nil
}

// exportedSnapFile implements ExportEntry describing a file stored inside a snap.
type exportedSnapFile struct {
	snap            *snap.Info
	pathInSnap      string
	pathInExportSet string
}

func (esf *exportedSnapFile) PathInHostMountNS() string {
	return filepath.Join(esf.snap.MountDir(), esf.pathInSnap)
}

func (esf *exportedSnapFile) PathInSnapMountNS() string {
	return filepath.Join("/snap", esf.snap.InstanceName(), esf.snap.Revision.String(), esf.pathInSnap)
}

func (esf *exportedSnapFile) PathInExportSet() string {
	return esf.pathInExportSet
}

func (esf *exportedSnapFile) IsExportedPathValidInHostMountNS() bool {
	return false
}

func exportedSnapToolsFromSnapdOrCore(info *snap.Info) []ExportEntry {
	return []ExportEntry{
		&exportedSnapFile{
			snap:            info,
			pathInSnap:      "usr/lib/snapd/snap-exec",
			pathInExportSet: "snap-exec",
		},
		// TODO: add the remaining tools here.
	}
}
