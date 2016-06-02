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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

var errNoSnap = errors.New("no snap installed")

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
func localSnapInfo(st *state.State, name string) (*snap.Info, *snapstate.SnapState, error) {
	st.Lock()
	defer st.Unlock()

	var snapst snapstate.SnapState
	err := snapstate.Get(st, name, &snapst)
	if err != nil && err != state.ErrNoState {
		return nil, nil, fmt.Errorf("cannot consult state: %v", err)
	}

	cur := snapst.Current()
	if cur == nil {
		return nil, nil, errNoSnap
	}

	info, err := snap.ReadInfo(name, cur)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot read snap details: %v", err)
	}

	return info, &snapst, nil
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

func effectiveConfinement(snapst *snapstate.SnapState) snap.ConfinementType {
	if snapst.DevMode() {
		return snap.DevmodeConfinement
	}
	return snap.StrictConfinement
}

func mapLocal(localSnap *snap.Info, snapst *snapstate.SnapState) map[string]interface{} {
	status := "installed"
	if snapst.Active {
		status = "active"
	}

	return map[string]interface{}{
		"description":    localSnap.Description(),
		"developer":      localSnap.Developer,
		"icon":           snapIcon(localSnap),
		"id":             localSnap.SnapID,
		"install-date":   snapDate(localSnap),
		"installed-size": localSnap.Size,
		"name":           localSnap.Name(),
		"revision":       localSnap.Revision,
		"status":         status,
		"summary":        localSnap.Summary(),
		"type":           string(localSnap.Type),
		"version":        localSnap.Version,
		"channel":        localSnap.Channel,
		"confinement":    localSnap.Confinement,
		"devmode":        snapst.DevMode(),
		"trymode":        snapst.TryMode(),
		"private":        localSnap.Private,
	}
}

func mapRemote(remoteSnap *snap.Info) map[string]interface{} {
	status := "available"
	if remoteSnap.MustBuy {
		status = "priced"
	}

	confinement := remoteSnap.Confinement
	if confinement == "" {
		confinement = snap.StrictConfinement
	}

	result := map[string]interface{}{
		"description":   remoteSnap.Description(),
		"developer":     remoteSnap.Developer,
		"download-size": remoteSnap.Size,
		"icon":          snapIcon(remoteSnap),
		"id":            remoteSnap.SnapID,
		"name":          remoteSnap.Name(),
		"revision":      remoteSnap.Revision,
		"status":        status,
		"summary":       remoteSnap.Summary(),
		"type":          string(remoteSnap.Type),
		"version":       remoteSnap.Version,
		"channel":       remoteSnap.Channel,
		"private":       remoteSnap.Private,
		"confinement":   confinement,
	}

	if len(remoteSnap.Prices) > 0 {
		result["prices"] = remoteSnap.Prices
	}
	return result
}
