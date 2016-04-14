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
	"os"
	"path/filepath"
	"time"

	"github.com/ubuntu-core/snappy/snap"
	"github.com/ubuntu-core/snappy/snappy"
)

// snapIcon tries to find the icon inside the snap
func snapIcon(info *snap.Info) string {
	// XXX: copy of snap.Snap.Icon which will go away
	found, _ := filepath.Glob(filepath.Join(info.MountDir(), "meta", "gui", "icon.*"))
	if len(found) == 0 {
		return ""
	}

	return found[0]
}

// snapDate returns the time of the snap mount directory.
func snapDate(info *snap.Info) time.Time {
	st, err := os.Stat(info.MountDir())
	if err != nil {
		return time.Time{}
	}

	return st.ModTime()
}

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

// Map a localSnap information plus the given active flag to a
// map[string]interface{}, augmenting it with the given (purportedly remote)
// snap.
//
// It is a programming error (->panic) to call mapSnap with both arguments
// nil.
func mapSnap(localSnap *snap.Info, active bool, remoteSnap *snap.Info) map[string]interface{} {
	var version, icon, name, developer, _type, description, summary string
	var revision int

	rollback := -1
	update := -1

	if localSnap == nil && remoteSnap == nil {
		panic("no localSnaps & remoteSnap is nil -- how did i even get here")
	}

	status := "available"
	installedSize := int64(-1)
	downloadSize := int64(-1)
	var prices map[string]float64

	if remoteSnap != nil {
		prices = remoteSnap.Prices
	}

	if localSnap != nil {
		if active {
			status = "active"
		} else {
			status = "installed"
		}
	}

	var ref *snap.Info
	if localSnap != nil {
		ref = localSnap
	} else {
		ref = remoteSnap
	}

	name = ref.Name()
	developer = ref.Developer
	version = ref.Version
	revision = ref.Revision
	_type = string(ref.Type)

	if localSnap != nil {
		icon = snapIcon(localSnap)
		summary = localSnap.Summary()
		description = localSnap.Description()
		installedSize = localSnap.Size
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

	if localSnap != nil && active {
		if remoteSnap != nil && revision != remoteSnap.Revision {
			update = remoteSnap.Revision
		}

		// WARNING this'll only get the right* rollback if
		// only two things can be installed
		//
		// *) not the actual right rollback because we aren't
		// marking things failed etc etc etc)
		//
		//if len(localSnaps) == 2 {
		//	rollback = localSnaps[1^idx].Revision()
		//}
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
		"prices":         prices,
	}

	if localSnap != nil {
		channel := localSnap.Channel
		if channel != "" {
			result["channel"] = channel
		}

		result["install-date"] = snapDate(localSnap)
	}

	if rollback > -1 {
		result["rollback-available"] = rollback
	}

	if update > -1 {
		result["update-available"] = update
	}

	return result
}
