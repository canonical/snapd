// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2018 Canonical Ltd
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
	"fmt"
	"time"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
)

// Level is the current implemented patch level of the state format and content.
var Level = 6

// Sublevel is the current implemented sublevel for the Level. Sublevel patches do not prevent rollbacks.
var Sublevel = 0

type PatchFunc func(s *state.State) error

// patches maps from patch level L to the list of sublevel patches.
var patches = make(map[int][]PatchFunc)

// Init initializes an empty state to the current implemented patch level.
func Init(s *state.State) {
	s.Lock()
	defer s.Unlock()
	if s.Get("patch-level", new(int)) != state.ErrNoState {
		panic("internal error: expected empty state, attempting to override patch-level without actual patching")
	}
	s.Set("patch-level", Level)

	if s.Get("patch-sublevel", new(int)) != state.ErrNoState {
		panic("internal error: expected empty state, attempting to override patch-sublevel without actual patching")
	}
	s.Set("patch-sublevel", Sublevel)
}

// applySublevelPatches applies all sublevel patches for given level, starting
// from firstSublevel index.
func applySublevelPatches(level, firstSublevel int, s *state.State) error {
	for sublevel := firstSublevel; sublevel < len(patches[level]); sublevel++ {
		if sublevel > 0 {
			logger.Noticef("Patching system state level %d to sublevel %d...", level, sublevel+1)
		}
		err := applyOne(patches[level][sublevel], s, level, sublevel+1)
		if err != nil {
			logger.Noticef("Cannot patch: %v", err)
			return fmt.Errorf("cannot patch system state to level %d, sublevel %d: %v", level, sublevel+1, err)
		}
	}
	return nil
}

// maybeResetSublevelForLevel60 checks if the previously installed core revision was a patch 6 sublevel 0
// i.e. unaware of sublevels and the need for resetting sublevel state and if so, resets sublevel to 0
// to re-apply sublevel patches.
func maybeResetSublevelForLevel60(s *state.State, sublevel *int) error {
	s.Lock()
	defer s.Unlock()

	core, err := snapstate.CoreInfo(s)
	if err == state.ErrNoState {
		// no core snap - nothing to do.
		return nil
	}
	if err != nil {
		return err
	}

	var snapst snapstate.SnapState
	if err := snapstate.Get(s, core.InstanceName(), &snapst); err != nil {
		return err
	}
	if len(snapst.Sequence) < 2 {
		return nil
	}
	currentIndex := snapst.LastIndex(snapst.Current)
	if currentIndex < 0 {
		return fmt.Errorf("internal error: couldn't find current core revision in the snap sequence")
	}

	prevRev := snapst.Sequence[currentIndex-1].Revision

	// check if core revision is for patch level 6.0.
	// the revision number here is the last core revision released before this code goes live.
	if prevRev.N > 5145 {
		return nil
	}

	var sublevelResetTime time.Time
	var lastRefresh time.Time
	if err := s.Get("last-refresh", &lastRefresh); err != nil && err != state.ErrNoState {
		return fmt.Errorf("cannot read last-refresh: %s", err)
	}
	err = s.Get("patch-sublevel-reset", &sublevelResetTime)
	if err != nil && err != state.ErrNoState {
		return fmt.Errorf("cannot read patch-sublevel-reset: %s", err)
	}
	if sublevelResetTime != lastRefresh || err == state.ErrNoState {
		*sublevel = 0
		s.Set("patch-sublevel", *sublevel)
		s.Set("patch-sublevel-reset", lastRefresh)
	}

	return nil
}

// Apply applies any necessary patches to update the provided state to
// conventions required by the current patch level of the system.
func Apply(s *state.State) error {
	var stateLevel, stateSublevel int
	s.Lock()
	err := s.Get("patch-level", &stateLevel)
	if err == nil || err == state.ErrNoState {
		err = s.Get("patch-sublevel", &stateSublevel)
	}
	s.Unlock()

	if err != nil && err != state.ErrNoState {
		return err
	}

	if stateLevel > Level {
		return fmt.Errorf("cannot downgrade: snapd is too old for the current system state (patch level %d)", stateLevel)
	}

	// check we refreshed from 6.0 which was not aware of sublevels
	if stateLevel == 6 && stateSublevel > 0 {
		if err := maybeResetSublevelForLevel60(s, &stateSublevel); err != nil {
			return err
		}
	}

	if stateLevel == Level && stateSublevel == Sublevel {
		return nil
	}

	// downgrade within same level; update sublevel in the state so that sublevel patches
	// are re-applied if the user refreshes to a newer patch sublevel again.
	if stateLevel == Level && stateSublevel > Sublevel {
		s.Lock()
		s.Set("patch-sublevel", Sublevel)
		s.Unlock()
		return nil
	}

	// apply any missing sublevel patches for current state level.
	// the 0th sublevel patch is a patch for major level update (e.g. 7.0),
	// therefore there is +1 for the indices.
	if stateSublevel+1 < len(patches[stateLevel]) {
		if err := applySublevelPatches(stateLevel, stateSublevel+1, s); err != nil {
			return err
		}
	}

	// at the lower Level - apply all new level and sublevel patches
	for level := stateLevel; level < Level; level++ {
		sublevels := patches[level+1]
		logger.Noticef("Patching system state from level %d to %d", level, level+1)
		if sublevels == nil {
			return fmt.Errorf("cannot upgrade: snapd is too new for the current system state (patch level %d)", level)
		}
		if err := applySublevelPatches(level+1, 0, s); err != nil {
			return err
		}
	}

	return nil
}

func applyOne(patch func(s *state.State) error, s *state.State, newLevel, newSublevel int) error {
	s.Lock()
	defer s.Unlock()

	err := patch(s)
	if err != nil {
		return err
	}

	s.Set("patch-level", newLevel)
	s.Set("patch-sublevel", newSublevel)
	return nil
}

// Mock mocks the current patch level and available patches.
func Mock(level int, sublevel int, p map[int][]PatchFunc) (restore func()) {
	oldLevel := Level
	oldPatches := patches
	Level = level
	patches = p

	oldSublevel := Sublevel
	Sublevel = sublevel

	return func() {
		Level = oldLevel
		patches = oldPatches
		Sublevel = oldSublevel
	}
}
