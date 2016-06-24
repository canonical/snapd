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

package snapstate

import (
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

// MigrateToTypeInState implements a state migration to have the snap type in the snap state of each setup snap. To be used in overlord/migrations.go.
func MigrateToTypeInState(s *state.State) error {
	var stateMap map[string]*SnapState

	err := s.Get("snaps", &stateMap)
	if err == state.ErrNoState {
		// nothing to do
		return nil
	}
	if err != nil {
		return err
	}

	for snapName, snapState := range stateMap {
		if snapState.CurrentSideInfo() == nil {
			continue
		}
		typ := snap.TypeApp
		snapInfo, err := readInfo(snapName, snapState.CurrentSideInfo())
		if err != nil {
			logger.Noticef("Recording type for snap %q: cannot retrieve info, assuming it's a app: %v", snapName, err)
		} else {
			logger.Noticef("Recording type for snap %q: setting to %q", snapName, snapInfo.Type)
			typ = snapInfo.Type
		}
		snapState.SetType(typ)
	}

	s.Set("snaps", stateMap)
	return nil
}

// MigrateToCurrentRevision implements a state migration to have the snap Current revision in the snap state of each setup snap. Used in overlord/migrations.go.
func MigrateToCurrentRevision(s *state.State) error {
	var stateMap map[string]*SnapState

	err := s.Get("snaps", &stateMap)
	if err == state.ErrNoState {
		// nothing to do
		return nil
	}
	if err != nil {
		return err
	}

	for _, snapState := range stateMap {
		n := len(snapState.Sequence)
		if n == 0 {
			continue
		}
		snapState.Current = snapState.Sequence[n-1].Revision
	}

	s.Set("snaps", stateMap)
	return nil
}
