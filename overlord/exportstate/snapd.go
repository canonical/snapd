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
func exportedSnapToolsFromSnapdOrCore(info *snap.Info) []*ExportEntry {
	entries := make([]*ExportEntry, 0, len(toolsToExport))
	for _, tool := range toolsToExport {
		entries = append(entries, NewExportedSnapFile(info, filepath.Join("usr/lib/snapd", tool), tool))
	}
	return entries
}

// NewExportedSnapFile returns an entry describing a file stored inside a snap.
func NewExportedSnapFile(snap *snap.Info, pathInSnap, pathInExportSet string) *ExportEntry {
	return &ExportEntry{
		PathInHostMountNS: filepath.Join(snap.MountDir(), pathInSnap),
		PathInSnapMountNS: filepath.Join("/snap", snap.InstanceName(), snap.Revision.String(), pathInSnap),
		PathInExportSet:   pathInExportSet,
	}
}
