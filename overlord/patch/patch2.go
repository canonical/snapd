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

type OldSnapState struct {
	SnapType string           `json:"type"` // Use Type and SetType
	Sequence []*snap.SideInfo `json:"sequence"`
	Active   bool             `json:"active,omitempty"`
	// Current indicates the current active revision if Active is
	// true or the last active revision if Active is false
	// (usually while a snap is being operated on or disabled)
	Current   snap.Revision            `json:"current"`
	Candidate *snap.SideInfo           `json:"candidate,omitempty"`
	Channel   string                   `json:"channel,omitempty"`
	Flags     snapstate.SnapStateFlags `json:"flags,omitempty"`

	// incremented revision used for local installs
	LocalRevision snap.Revision `json:"local-revision,omitempty"`
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
// - migrates SnapSetup.Name to SnapSetup.SideInfo.RealName and candidate
// - backfills SnapState.{Sequence,Candidate}.RealName if its missing
func patch2(s *state.State) error {

	var stateMap map[string]*OldSnapState
	err := s.Get("snaps", &stateMap)
	if err == state.ErrNoState {
		return nil
	}
	if err != nil {
		return err
	}

	// migrate SnapSetup in all tasks:
	//  - the new SnapSetup uses SideInfo, backfil from Candidate
	//  - also move SnapSetup.{Name,Revision} into SnapSetup.SideInfo.{RealName,Revision}
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
			if snapst, ok := stateMap[oldSS.Name]; ok && snapst.Candidate != nil {
				newSS.SideInfo = snapst.Candidate
			}
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
	// (if that is missing, was missing for e.g. sideloaded snaps)
	for snapName, snapState := range stateMap {
		for _, si := range snapState.Sequence {
			setRealName(si, snapName)
		}
	}
	s.Set("snaps", stateMap)

	return nil
}
