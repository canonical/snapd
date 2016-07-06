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
	patches[1] = patch1
}

// patch1 adds the snap type and the current revision to the snap state.
func patch1(s *state.State) error {
	var stateMap map[string]*snapstate.SnapState

	err := s.Get("snaps", &stateMap)
	if err == state.ErrNoState {
		return nil
	}
	if err != nil {
		return err
	}

	for snapName, snapState := range stateMap {
		seq := snapState.Sequence
		if len(seq) == 0 {
			continue
		}
		typ := snap.TypeApp
		snapInfo, err := readInfo(snapName, seq[len(seq)-1])
		if err != nil {
			logger.Noticef("Recording type for snap %q: cannot retrieve info, assuming it's a app: %v", snapName, err)
		} else {
			logger.Noticef("Recording type for snap %q: setting to %q", snapName, snapInfo.Type)
			typ = snapInfo.Type
		}
		snapState.SetType(typ)
		snapState.Current = seq[len(seq)-1].Revision
	}

	s.Set("snaps", stateMap)
	return nil
}
