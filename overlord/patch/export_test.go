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

// PatchesForTest returns the registered set of patches for testing purposes.
func PatchesForTest() map[int][]PatchFunc {
	return patches
}

// MockPatch1ReadType replaces patch1ReadType.
func MockPatch1ReadType(f func(name string, rev snap.Revision) (snap.Type, error)) (restore func()) {
	old := patch1ReadType
	patch1ReadType = f
	return func() { patch1ReadType = old }
}

// MockLevel replaces the current implemented patch level
func MockLevel(lv, sublvl int) (restorer func()) {
	old := Level
	Level = lv
	oldSublvl := Sublevel
	Sublevel = sublvl
	return func() {
		Level = old
		Sublevel = oldSublvl
	}
}

func Patch4TaskSnapSetup(task *state.Task) (*patch4SnapSetup, error) {
	return patch4T{}.taskSnapSetup(task)
}

func Patch4StateMap(st *state.State) (map[string]patch4SnapState, error) {
	var stateMap map[string]patch4SnapState
	err := st.Get("snaps", &stateMap)

	return stateMap, err
}

func Patch6StateMap(st *state.State) (map[string]patch6SnapState, error) {
	var stateMap map[string]patch6SnapState
	err := st.Get("snaps", &stateMap)

	return stateMap, err
}

func Patch6SnapSetup(task *state.Task) (patch6SnapSetup, error) {
	var snapsup patch6SnapSetup
	err := task.Get("snap-setup", &snapsup)
	return snapsup, err
}
