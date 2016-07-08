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
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

func init() {
	patches[2] = patch2
}

// patch2 renames the snap "ubuntu-core" to just "core".
func patch2(s *state.State) error {
	var stateMap map[string]*snapstate.SnapState

	err := s.Get("snaps", &stateMap)
	if err == state.ErrNoState {
		return nil
	}
	if err != nil {
		return err
	}
	// TODO: put the ID of the core snap here
	const oldCoreID string = "..."

	for _, snapState := range stateMap {
		for _, sideInfo := range snapState.Sequence {
			if sideInfo.SnapID == oldCoreID && sideInfo.RealName == "ubuntu-core" {
				// XXX: we probably cannot unmount the core snap reliably
				// because services and programs may be already running at this
				// time. What we can do instead is rename the snap on disk and
				// in the state so that on next boot everything will pick up
				// the new name and setup a bind mount so that ubuntu-core and
				// core looks the same for running applications.
			}
		}
	}

	s.Set("snaps", stateMap)
	return nil
}
