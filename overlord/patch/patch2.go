// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package patch

import (
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

func init() {
	patches[2] = patch2
}

type OldSnapSetup struct {
	Name     string        `json:"name,omitempty"`
	Revision snap.Revision `json:"revision,omitempty"`
	Channel  string        `json:"channel,omitempty"`
	UserID   int           `json:"user-id,omitempty"`

	Flags snapstate.SnapSetupFlags `json:"flags,omitempty"`

	SnapPath string `json:"snap-path,omitempty"`

	DownloadInfo *snap.DownloadInfo `json:"download-info,omitempty"`
	SideInfo     *snap.SideInfo     `json:"side-info,omitempty"`
}

func setRealName(si *snap.SideInfo, name string) {
	if si == nil {
		return
	}
	if si.RealName == "" {
		si.RealName = name
	}
}

// patch2:
// - migrates SnapSetup.Name to SnapSetup.SideInfo.RealName
// - backfills SnapState.{Sequence,Candidate}.RealName if its missing
func patch2(s *state.State) error {

	// migrate the SnapSetup.Name
	var oldSS OldSnapSetup
	var newSS snapstate.SnapSetup
	for _, t := range s.Tasks() {
		err := t.Get("snap-setup", &oldSS)
		if err == state.ErrNoState {
			continue
		}
		if err != nil && err != state.ErrNoState {
			return err
		}
		// some things stay the same
		newSS.Channel = oldSS.Channel
		newSS.Flags = oldSS.Flags
		newSS.SnapPath = oldSS.SnapPath
		newSS.DownloadInfo = oldSS.DownloadInfo
		newSS.SideInfo = oldSS.SideInfo
		// ... and some change
		if newSS.SideInfo == nil {
			newSS.SideInfo = &snap.SideInfo{}
		}
		if newSS.SideInfo.RealName == "" {
			newSS.SideInfo.RealName = oldSS.Name
		}
		if newSS.SideInfo.Revision.Unset() {
			newSS.SideInfo.Revision = oldSS.Revision
		}
		t.Set("snap-setup", &newSS)
	}

	// backfill snapstate.SnapState.{Sequence,Candidate} with RealName
	var stateMap map[string]*snapstate.SnapState
	err := s.Get("snaps", &stateMap)
	if err == state.ErrNoState {
		return nil
	}
	if err != nil {
		return err
	}

	for snapName, snapState := range stateMap {
		for _, si := range snapState.Sequence {
			setRealName(si, snapName)
		}
	}
	s.Set("snaps", stateMap)

	return nil
}
