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

	"github.com/snapcore/snapd/snap"
)

// exportedSnapToolsFromSnapdOrCore returns export entries describing
// essential snapd tools, like snap-exec.
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
