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
	"errors"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

func init() {
	patches[2] = []PatchFunc{patch2}
}

type patch2SideInfo struct {
	RealName          string        `yaml:"name,omitempty" json:"name,omitempty"`
	SnapID            string        `yaml:"snap-id" json:"snap-id"`
	Revision          snap.Revision `yaml:"revision" json:"revision"`
	Channel           string        `yaml:"channel,omitempty" json:"channel,omitempty"`
	DeveloperID       string        `yaml:"developer-id,omitempty" json:"developer-id,omitempty"`
	Developer         string        `yaml:"developer,omitempty" json:"developer,omitempty"` // XXX: obsolete, will be retired after full backfilling of DeveloperID
	EditedSummary     string        `yaml:"summary,omitempty" json:"summary,omitempty"`
	EditedDescription string        `yaml:"description,omitempty" json:"description,omitempty"`
	Size              int64         `yaml:"size,omitempty" json:"size,omitempty"`
	Sha512            string        `yaml:"sha512,omitempty" json:"sha512,omitempty"`
	Private           bool          `yaml:"private,omitempty" json:"private,omitempty"`
}

type patch2DownloadInfo struct {
	AnonDownloadURL string `json:"anon-download-url,omitempty"`
	DownloadURL     string `json:"download-url,omitempty"`
}

type patch2Flags int

type patch2SnapState struct {
	SnapType string            `json:"type"` // Use Type and SetType
	Sequence []*patch2SideInfo `json:"sequence"`
	Active   bool              `json:"active,omitempty"`
	// Current indicates the current active revision if Active is
	// true or the last active revision if Active is false
	// (usually while a snap is being operated on or disabled)
	Current snap.Revision `json:"current"`
	Channel string        `json:"channel,omitempty"`
	Flags   patch2Flags   `json:"flags,omitempty"`
}

type patch2SnapSetup struct {
	// FIXME: rename to RequestedChannel to convey the meaning better
	Channel string `json:"channel,omitempty"`
	UserID  int    `json:"user-id,omitempty"`

	Flags patch2Flags `json:"flags,omitempty"`

	SnapPath string `json:"snap-path,omitempty"`

	DownloadInfo *patch2DownloadInfo `json:"download-info,omitempty"`
	SideInfo     *patch2SideInfo     `json:"side-info,omitempty"`
}

func patch2SideInfoFromPatch1(oldInfo *patch1SideInfo, name string) *patch2SideInfo {
	return &patch2SideInfo{
		RealName:          name, // NOTE: OfficialName dropped
		SnapID:            oldInfo.SnapID,
		Revision:          oldInfo.Revision,
		Channel:           oldInfo.Channel,
		Developer:         oldInfo.Developer, // NOTE: no DeveloperID in patch1SideInfo
		EditedSummary:     oldInfo.EditedSummary,
		EditedDescription: oldInfo.EditedDescription,
		Size:              oldInfo.Size,
		Sha512:            oldInfo.Sha512,
		Private:           oldInfo.Private,
	}
}

func patch2SequenceFromPatch1(oldSeq []*patch1SideInfo, name string) []*patch2SideInfo {
	newSeq := make([]*patch2SideInfo, len(oldSeq))
	for i, si := range oldSeq {
		newSeq[i] = patch2SideInfoFromPatch1(si, name)
	}

	return newSeq
}

func patch2SnapStateFromPatch1(oldSnapState *patch1SnapState, name string) *patch2SnapState {
	return &patch2SnapState{
		SnapType: oldSnapState.SnapType,
		Sequence: patch2SequenceFromPatch1(oldSnapState.Sequence, name),
		Active:   oldSnapState.Active,
		Current:  oldSnapState.Current,
		Channel:  oldSnapState.Channel,
		Flags:    patch2Flags(oldSnapState.Flags),
	}
}

// patch2:
// - migrates SnapSetup.Name to SnapSetup.SideInfo.RealName
// - backfills SnapState.{Sequence,Candidate}.RealName if its missing
func patch2(s *state.State) error {
	var oldStateMap map[string]*patch1SnapState
	mylog.Check(s.Get("snaps", &oldStateMap))
	if errors.Is(err, state.ErrNoState) {
		return nil
	}

	newStateMap := make(map[string]*patch2SnapState, len(oldStateMap))

	for key, oldSnapState := range oldStateMap {
		newStateMap[key] = patch2SnapStateFromPatch1(oldSnapState, key)
	}

	// migrate SnapSetup in all tasks:
	//  - the new SnapSetup uses SideInfo, backfil from Candidate
	//  - also move SnapSetup.{Name,Revision} into SnapSetup.SideInfo.{RealName,Revision}
	var oldSS patch1SnapSetup
	for _, t := range s.Tasks() {
		var newSS patch2SnapSetup
		mylog.Check(t.Get("snap-setup", &oldSS))
		if errors.Is(err, state.ErrNoState) {
			continue
		}
		if err != nil && !errors.Is(err, state.ErrNoState) {
			return err
		}
		// some things stay the same
		newSS.Channel = oldSS.Channel
		newSS.Flags = patch2Flags(oldSS.Flags)
		newSS.SnapPath = oldSS.SnapPath
		// ... and some change
		newSS.SideInfo = &patch2SideInfo{}
		if snapst, ok := oldStateMap[oldSS.Name]; ok && snapst.Candidate != nil {
			newSS.SideInfo = patch2SideInfoFromPatch1(snapst.Candidate, oldSS.Name)
		}
		if newSS.SideInfo.RealName == "" {
			newSS.SideInfo.RealName = oldSS.Name
		}
		if newSS.SideInfo.Revision.Unset() {
			newSS.SideInfo.Revision = oldSS.Revision
		}
		t.Set("snap-setup", &newSS)
	}

	s.Set("snaps", newStateMap)

	return nil
}
