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
	patches[6] = []PatchFunc{patch6, patch6_1, patch6_2, patch6_3}
}

type patch6Flags struct {
	DevMode  bool `json:"devmode,omitempty"`
	JailMode bool `json:"jailmode,omitempty"`
	TryMode  bool `json:"trymode,omitempty"`
	Revert   bool `json:"revert,omitempty"`
}

type patch6SnapSetup struct {
	Channel string `json:"channel,omitempty"`
	UserID  int    `json:"user-id,omitempty"`

	patch6Flags

	SnapPath string `json:"snap-path,omitempty"`

	DownloadInfo *patch4DownloadInfo `json:"download-info,omitempty"`
	SideInfo     *patch4SideInfo     `json:"side-info,omitempty"`
}

type patch6SnapState struct {
	SnapType string            `json:"type"` // Use Type and SetType
	Sequence []*patch4SideInfo `json:"sequence"`
	Active   bool              `json:"active,omitempty"`
	Current  snap.Revision     `json:"current"`
	Channel  string            `json:"channel,omitempty"`
	patch6Flags
}

func patch6FlagsFromPatch4(old patch4Flags) patch6Flags {
	return patch6Flags{
		DevMode:  old.DevMode(),
		TryMode:  old.TryMode(),
		JailMode: old.JailMode(),
		Revert:   old.Revert(),
	}
}

// patch6:
//   - move from a flags-are-ints world to a flags-are-struct-of-bools world
func patch6(st *state.State) error {
	var oldStateMap map[string]*patch4SnapState
	mylog.Check(st.Get("snaps", &oldStateMap))
	if errors.Is(err, state.ErrNoState) {
		return nil
	}

	newStateMap := make(map[string]*patch6SnapState, len(oldStateMap))

	for key, old := range oldStateMap {
		newStateMap[key] = &patch6SnapState{
			SnapType:    old.SnapType,
			Sequence:    old.Sequence,
			Active:      old.Active,
			Current:     old.Current,
			Channel:     old.Channel,
			patch6Flags: patch6FlagsFromPatch4(old.Flags),
		}
	}

	for _, task := range st.Tasks() {
		var old patch4SnapSetup
		mylog.Check(task.Get("snap-setup", &old))
		if errors.Is(err, state.ErrNoState) {
			continue
		}
		if err != nil && !errors.Is(err, state.ErrNoState) {
			return err
		}

		task.Set("snap-setup", &patch6SnapSetup{
			Channel:      old.Channel,
			UserID:       old.UserID,
			SnapPath:     old.SnapPath,
			DownloadInfo: old.DownloadInfo,
			SideInfo:     old.SideInfo,
			patch6Flags:  patch6FlagsFromPatch4(old.Flags),
		})
	}

	st.Set("snaps", newStateMap)

	return nil
}
