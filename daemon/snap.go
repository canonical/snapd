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
	"github.com/ubuntu-core/snappy/snappy"
)

// allSnaps returns all installed snaps, grouped by name
func allSnaps() (map[string][]*snappy.Snap, error) {
	all, err := snappy.NewLocalSnapRepository().Installed()
	if err != nil {
		return nil, err
	}

	m := make(map[string][]*snappy.Snap)

	for _, part := range all {
		name := snappy.FullName(part)
		m[name] = append(m[name], part)
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
// Part.
//
// It is a programming error (->panic) to call Map on a nil/empty slice with
// a nil Part. Slice or remotePart may be empty/nil, but not both of them.
//
// Also may panic if the remote part is nil and Best() is nil.
func mapSnap(localSnaps []*snappy.Snap, remotePart *snappy.RemoteSnap) map[string]interface{} {
	var version, update, rollback, icon, name, developer, _type, description string

	if len(localSnaps) == 0 && remotePart == nil {
		panic("no localSnaps & remotePart is nil -- how did i even get here")
	}

	status := "not installed"
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
	} else if remotePart == nil {
		panic("unable to load a valid part")
	}

	if localSnap != nil {
		name = localSnap.Name()
		developer = localSnap.Developer()
		version = localSnap.Version()
		_type = string(localSnap.Type())

		icon = localSnap.Icon()
		description = localSnap.Description()
		installedSize = localSnap.InstalledSize()

		downloadSize = localSnap.DownloadSize()
	} else {
		name = remotePart.Name()
		developer = remotePart.Developer()
		version = remotePart.Version()
		_type = string(remotePart.Type())
	}

	if remotePart != nil {
		if icon == "" {
			icon = remotePart.Icon()
		}
		if description == "" {
			description = remotePart.Description()
		}

		downloadSize = remotePart.DownloadSize()
	}

	if localSnap != nil && localSnap.IsActive() {
		if remotePart != nil && version != remotePart.Version() {
			// XXX: this does not handle the case where the
			// one in the store is not the greatest version
			// (e.g.: store has 1.1, locally available 1.1,
			// 1.2, active 1.2)
			update = remotePart.Version()
		}

		// WARNING this'll only get the right* rollback if
		// only two things can be installed
		//
		// *) not the actual right rollback because we aren't
		// marking things failed etc etc etc)
		//
		if len(localSnaps) > 1 {
			rollback = localSnaps[1^idx].Version()
		}
	}

	result := map[string]interface{}{
		"icon":           icon,
		"name":           name,
		"developer":      developer,
		"status":         status,
		"type":           _type,
		"vendor":         "",
		"version":        version,
		"description":    description,
		"installed_size": installedSize,
		"download_size":  downloadSize,
	}

	if localSnap != nil {
		channel := localSnap.Channel()
		if channel != "" {
			result["channel"] = channel
		}
	}

	if rollback != "" {
		result["rollback_available"] = rollback
	}

	if update != "" {
		result["update_available"] = update
	}

	return result
}
