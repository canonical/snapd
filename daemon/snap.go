// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2016 Canonical Ltd
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

package daemon

import (
	"github.com/ubuntu-core/snappy/snap"
	"github.com/ubuntu-core/snappy/snappy"
)

// allSnaps returns all installed snaps, grouped by name
func allSnaps() (map[string][]*snappy.Snap, error) {
	all, err := (&snappy.Overlord{}).Installed()
	if err != nil {
		return nil, err
	}

	m := make(map[string][]*snappy.Snap)

	for _, snap := range all {
		name := snap.Name()
		m[name] = append(m[name], snap)
	}

	return m, nil
}

// Best Snap in the slice (and its index therein).
//
// If there is an active part, that. Otherwise, the last part in the slice.
//
// (-1, nil) if slice is nil or empty.
func bestSnap(snaps []*snappy.Snap) (idx int, snap *snappy.Snap) {
	idx = -1

	for idx, snap = range snaps {
		if snap.IsActive() {
			break
		}
	}

	return idx, snap
}

// Map a slice of *snapppy.Snaps that share a name into a
// map[string]interface{}, augmenting it with the given (purportedly remote)
// snap.
//
// It is a programming error (->panic) to call Map on a nil/empty slice with
// a nil remotSnap. Slice or remoteSnap may be empty/nil, but not both of them.
//
// Also may panic if the remoteSnap is nil and Best() is nil.
func mapSnap(localSnaps []*snappy.Snap, remoteSnap *snap.Info) map[string]interface{} {
	var version, icon, name, developer, _type, description, summary string
	var revision int

	rollback := -1
	update := -1

	if len(localSnaps) == 0 && remoteSnap == nil {
		panic("no localSnaps & remoteSnap is nil -- how did i even get here")
	}

	status := "available"
	installedSize := int64(-1)
	downloadSize := int64(-1)

	idx, localSnap := bestSnap(localSnaps)
	if localSnap != nil {
		if localSnap.IsActive() {
			status = "active"
		} else if localSnap.IsInstalled() {
			status = "installed"
		} else {
			status = "removed"
		}
	} else if remoteSnap == nil {
		panic("unable to load a valid snap")
	}

	if localSnap != nil {
		name = localSnap.Name()
		developer = localSnap.Developer()
		version = localSnap.Version()
		revision = localSnap.Revision()
		_type = string(localSnap.Type())

		icon = localSnap.Icon()
		summary = localSnap.Info().Summary()
		description = localSnap.Info().Description()
		installedSize = localSnap.InstalledSize()

		downloadSize = localSnap.DownloadSize()
	} else {
		name = remoteSnap.Name()
		developer = remoteSnap.Developer
		version = remoteSnap.Version
		revision = remoteSnap.Revision
		_type = string(remoteSnap.Type)
	}

	if remoteSnap != nil {
		if icon == "" {
			icon = remoteSnap.IconURL
		}
		if description == "" {
			description = remoteSnap.Description()
		}
		if summary == "" {
			summary = remoteSnap.Summary()
		}

		downloadSize = remoteSnap.Size
	}

	if localSnap != nil && localSnap.IsActive() {
		if remoteSnap != nil && revision != remoteSnap.Revision {
			update = remoteSnap.Revision
		}

		// WARNING this'll only get the right* rollback if
		// only two things can be installed
		//
		// *) not the actual right rollback because we aren't
		// marking things failed etc etc etc)
		//
		if len(localSnaps) == 2 {
			rollback = localSnaps[1^idx].Revision()
		}
	}

	result := map[string]interface{}{
		"icon":           icon,
		"name":           name,
		"developer":      developer,
		"status":         status,
		"type":           _type,
		"vendor":         "",
		"revision":       revision,
		"version":        version,
		"description":    description,
		"summary":        summary,
		"installed-size": installedSize,
		"download-size":  downloadSize,
	}

	if localSnap != nil {
		channel := localSnap.Channel()
		if channel != "" {
			result["channel"] = channel
		}

		result["install-date"] = localSnap.Date()
	}

	if rollback > -1 {
		result["rollback-available"] = rollback
	}

	if update > -1 {
		result["update-available"] = update
	}

	return result
}
