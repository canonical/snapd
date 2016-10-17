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
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

func init() {
	patches[6] = patch6
}

type patch6StateFlags struct {
	DevMode  bool `json:"devmode,omitempty"`
	JailMode bool `json:"jailmode,omitempty"`
	TryMode  bool `json:"trymode,omitempty"`
}

type patch6SetupFlags struct {
	patch6StateFlags
	Revert bool `json:"revert,omitempty"`
}

type patch6SnapSetup struct {
	Channel string `json:"channel,omitempty"`
	UserID  int    `json:"user-id,omitempty"`

	Flags patch6SetupFlags `json:"flags,omitempty"`

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
	Flags    patch6StateFlags  `json:"flags,omitempty"`
}

func patch6StateFlagsFromPatch4(old patch4Flags) patch6StateFlags {
	return patch6StateFlags{
		DevMode:  old.DevMode(),
		TryMode:  old.TryMode(),
		JailMode: old.JailMode(),
	}
}

func patch6SetupFlagsFromPatch4(old patch4Flags) patch6SetupFlags {
	return patch6SetupFlags{
		patch6StateFlags: patch6StateFlagsFromPatch4(old),
		Revert:           old.Revert(),
	}
}

// patch6:
//  - move from a flags-are-ints world to a flags-are-struct-of-bools world
func patch6(st *state.State) error {
	var oldStateMap map[string]*patch4SnapState
	err := st.Get("snaps", &oldStateMap)
	if err == state.ErrNoState {
		return nil
	}
	if err != nil {
		return err
	}
	newStateMap := make(map[string]*patch6SnapState, len(oldStateMap))

	for key, old := range oldStateMap {
		newStateMap[key] = &patch6SnapState{
			SnapType: old.SnapType,
			Sequence: old.Sequence,
			Active:   old.Active,
			Current:  old.Current,
			Channel:  old.Channel,
			Flags:    patch6StateFlagsFromPatch4(old.Flags),
		}
	}

	for _, task := range st.Tasks() {
		var old patch4SnapSetup
		err := task.Get("snap-setup", &old)
		if err == state.ErrNoState {
			continue
		}
		if err != nil && err != state.ErrNoState {
			return err
		}

		task.Set("snap-setup", &patch6SnapSetup{
			Channel:      old.Channel,
			UserID:       old.UserID,
			SnapPath:     old.SnapPath,
			DownloadInfo: old.DownloadInfo,
			SideInfo:     old.SideInfo,
			Flags:        patch6SetupFlagsFromPatch4(old.Flags),
		})
	}

	st.Set("snaps", newStateMap)

	return nil
}
