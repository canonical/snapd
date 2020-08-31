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

// Package exportstate implements the manager and state aspects responsible
// for the exporting portions of installed snaps to the system.
package exportstate

import "fmt"

// ExportManifest describes content exported by one provider.
//
// The provider is usually a single revision of a given snap, but there are
// some exceptions discussed below.
//
// PrimaryKey is usually the snap name without the instance key. SubKey is
// usually the snap revision combined with the instance key.  ExportSets
// describes the set of exported files, grouped into topics by export set name.
//
// Exceptions apply whe PrimaryKey is "snapd". The SybKey has additional forms,
// incluging "$revision", "core_$revision" and "host". Neither primary nor sub
// keys should be parsed.
type ExportManifest struct {
	// XXX: should the keys be at the same level as export set name,
	// allowing a single snap to provide multiple objects under distinct
	// primary key, sub key and export set name?  Currently we don't need
	// this as only the "core" snap is a special case.
	PrimaryKey string
	SubKey     string
	ExportSets map[ExportSetName][]ExportEntry
}

// PutOnDisk writes a manifest to disk.
//
// The directory /var/lib/snapd/export/$primary/$sub is created and
// populated with symbolic links, as described by the export sets.
func (em *ExportManifest) PutOnDisk() error {
	// TODO: implement this.
	return fmt.Errorf("putting exports on disk is not implemented")
}

// RemoveFromDisk removes an error manifest from disk.
//
// The directory hosting the export set is recursively removed.
// In addition the path /var/lib/snapd/export/$primary/$sub
// is pruned, and empty directories are removed.
func (em *ExportManifest) RemoveFromDisk() error {
	// TODO: implement this
	return fmt.Errorf("removing exports from disk is not implemented")
}

// ExportSetName designates a group of related files exported by a snap.
type ExportSetName string

// snapdTools is the export set name for all internal snapd tools.
const snapdTools ExportSetName = "tools"

// ExportEntry is an interface describing a single file placed in a specific export set.
//
// The original file is described by two paths. PathInHostMountNS is is valid
// in the host mount namespace while PathInSnapMountNS is valid in the per-snap
// mount namespace. The original file is exported by creating a symbolic link
// under PathInExportSet, which is relative to the export set that the entry
// belongs to, to one of the two other paths.  If
// IsExportedPathValidInHostMountNS returns true then PathInHostMountNS is
// used, if it returns false PathInSnapMountNS is used instead.
//
// This distinction enables exporting files that are consumed by either other
// snaps or by the classic system.
type ExportEntry interface {
	PathInExportSet() string
	PathInHostMountNS() string
	PathInSnapMountNS() string
	IsExportedPathValidInHostMountNS() bool
}
