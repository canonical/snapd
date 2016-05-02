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
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ubuntu-core/snappy/overlord/snapstate"
	"github.com/ubuntu-core/snappy/overlord/state"
	"github.com/ubuntu-core/snappy/snap"
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

// localSnapInfo returns the information about the current snap for the given name plus the SnapState with the active flag and other snap revisions.
func localSnapInfo(st *state.State, name string) (info *snap.Info, active bool, err error) {
	st.Lock()
	defer st.Unlock()

	var snapst snapstate.SnapState
	err = snapstate.Get(st, name, &snapst)
	if err != nil && err != state.ErrNoState {
		return nil, false, fmt.Errorf("cannot consult state: %v", err)
	}

	cur := snapst.Current()
	if cur == nil {
		return nil, false, nil
	}

	info, err = snap.ReadInfo(name, cur)
	if err != nil {
		return nil, false, fmt.Errorf("cannot read snap details: %v", err)
	}

	return info, snapst.Active, nil
}

type aboutSnap struct {
	info   *snap.Info
	snapst *snapstate.SnapState
}

// allLocalSnapInfos returns the information about the all current snaps and their SnapStates.
func allLocalSnapInfos(st *state.State) ([]aboutSnap, error) {
	st.Lock()
	defer st.Unlock()

	snapStates, err := snapstate.All(st)
	if err != nil {
		return nil, err
	}

	about := make([]aboutSnap, 0, len(snapStates))

	var firstErr error
	for name, snapState := range snapStates {
		info, err := snap.ReadInfo(name, snapState.Current())
		if err != nil {
			// XXX: aggregate instead?
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		about = append(about, aboutSnap{info, snapState})
	}

	return about, firstErr
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
		"icon":          icon,
		"name":          name,
		"developer":     developer,
		"status":        status,
		"type":          _type,
		"vendor":        "",
		"revision":      revision,
		"version":       version,
		"description":   description,
		"summary":       summary,
		"download-size": downloadSize,
	}

	if len(prices) > 0 {
		result["prices"] = prices
	}

	if localSnap != nil {
		channel := localSnap.Channel
		if channel != "" {
			result["channel"] = channel
		}

		result["installed-size"] = localSnap.Size
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
