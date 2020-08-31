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
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/release"
)

// HostAbstractManifest returns the export manifest of the host system.
//
// Ubuntu Core systems do not have an export manifest. Classic systems have an export
// manifest which describes snapd tools from the classic package.
func HostAbstractManifest() *AbstractManifest {
	if release.OnClassic {
		// This does not need to consider re-exec vs not, as that logic
		// is only necessary during the election process of the current
		// export set.
		return abstractManifestForClassicSystem()
	}
	return nil
}

// abstractManifestForClassicSystem returns the export manifest of the classic system.
//
// The classic system exposes snapd tools coming from the classic package.
// The tools are exposes under the "snapd" name, with the subkey "host".
func abstractManifestForClassicSystem() *AbstractManifest {
	return &AbstractManifest{
		// The system contains a classic package.
		PrimaryKey: "snapd",
		SubKey:     "host",
		ExportSets: map[ExportSetName][]ExportEntry{
			snapdTools: exportedSnapdToolsFromHost(),
		},
	}
}

// exportedHostFile implements ExportEntry describing a file from the classic file system.
//
// TODO: consider using this to describe nvidia libraries from the host.
type exportedHostFile struct {
	pathOnHost      string
	pathInExportSet string
}

func (ehf *exportedHostFile) PathInHostMountNS() string {
	return ehf.pathOnHost
}

func (ehf *exportedHostFile) PathInSnapMountNS() string {
	return filepath.Join("/var/lib/snapd/hostfs", ehf.pathOnHost)
}

func (ehf *exportedHostFile) PathInExportSet() string {
	return ehf.pathInExportSet
}

func (ehf *exportedHostFile) IsExportedPathValidInHostMountNS() bool {
	return false
}

func exportedSnapdToolsFromHost() []ExportEntry {
	return []ExportEntry{
		&exportedHostFile{
			pathOnHost:      filepath.Join(dirs.DistroLibExecDir, "snap-exec"),
			pathInExportSet: "snap-exec",
		},
		// TODO: add the remaining tools here.
	}
}
