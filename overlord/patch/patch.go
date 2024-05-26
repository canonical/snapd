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
	"errors"
	"fmt"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snapdtool"
)

// Level is the current implemented patch level of the state format and content.
var Level = 6

// Sublevel is the current implemented sublevel for the Level.
// Sublevel 0 is the first patch for the new Level, rollback below x.0 is not possible.
// Sublevel patches > 0 do not prevent rollbacks.
var Sublevel = 3

type PatchFunc func(s *state.State) error

// patches maps from patch level L to the list of sublevel patches.
var patches = make(map[int][]PatchFunc)

// Init initializes an empty state to the current implemented patch level.
func Init(s *state.State) {
	s.Lock()
	defer s.Unlock()
	if mylog.Check(s.Get("patch-level", new(int))); !errors.Is(err, state.ErrNoState) {
		panic("internal error: expected empty state, attempting to override patch-level without actual patching")
	}
	s.Set("patch-level", Level)

	if mylog.Check(s.Get("patch-sublevel", new(int))); !errors.Is(err, state.ErrNoState) {
		panic("internal error: expected empty state, attempting to override patch-sublevel without actual patching")
	}
	s.Set("patch-sublevel", Sublevel)
}

// applySublevelPatches applies all sublevel patches for given level, starting
// from firstSublevel index.
func applySublevelPatches(level, firstSublevel int, s *state.State) error {
	for sublevel := firstSublevel; sublevel < len(patches[level]); sublevel++ {
		if sublevel > 0 {
			logger.Noticef("Patching system state level %d to sublevel %d...", level, sublevel)
		}
		mylog.Check(applyOne(patches[level][sublevel], s, level, sublevel))

	}
	return nil
}

// maybeResetSublevelForLevel60 checks if we're coming from a different version
// of snapd and if so, reset sublevel back to 0 to re-apply sublevel patches.
func maybeResetSublevelForLevel60(s *state.State, sublevel *int) error {
	s.Lock()
	defer s.Unlock()

	var lastVersion string
	mylog.Check(s.Get("patch-sublevel-last-version", &lastVersion))
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}
	if errors.Is(err, state.ErrNoState) || lastVersion != snapdtool.Version {
		*sublevel = 0
		s.Set("patch-sublevel", *sublevel)
		// unset old reset key in case of revert into old version.
		// TODO: this can go away if we go through a snapd epoch.
		s.Set("patch-sublevel-reset", nil)
	}

	return nil
}

// Apply applies any necessary patches to update the provided state to
// conventions required by the current patch level of the system.
func Apply(s *state.State) error {
	var stateLevel, stateSublevel int
	s.Lock()
	mylog.Check(s.Get("patch-level", &stateLevel))
	if err == nil || errors.Is(err, state.ErrNoState) {
		mylog.Check(s.Get("patch-sublevel", &stateSublevel))
	}
	s.Unlock()

	if err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}

	if stateLevel > Level {
		return fmt.Errorf("cannot downgrade: snapd is too old for the current system state (patch level %d)", stateLevel)
	}

	// check if we refreshed from 6.0 which was not aware of sublevels
	if stateLevel == 6 && stateSublevel > 0 {
		mylog.Check(maybeResetSublevelForLevel60(s, &stateSublevel))
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

	// apply any missing sublevel patches for current state level before upgrading to new levels.
	// the 0th sublevel patch is a patch for major level update (e.g. 7.0),
	// therefore there is +1 for the indices.
	if stateSublevel+1 < len(patches[stateLevel]) {
		mylog.Check(applySublevelPatches(stateLevel, stateSublevel+1, s))
	}

	// at the lower Level - apply all new level and sublevel patches
	for level := stateLevel + 1; level <= Level; level++ {
		sublevels := patches[level]
		logger.Noticef("Patching system state from level %d to %d", level-1, level)
		if sublevels == nil {
			return fmt.Errorf("cannot upgrade: snapd is too new for the current system state (patch level %d)", level-1)
		}
		mylog.Check(applySublevelPatches(level, 0, s))

	}

	s.Lock()
	// store last snapd version last in case system is restarted before patches are applied
	s.Set("patch-sublevel-last-version", snapdtool.Version)
	s.Unlock()

	return nil
}

func applyOne(patch func(s *state.State) error, s *state.State, newLevel, newSublevel int) error {
	s.Lock()
	defer s.Unlock()
	mylog.Check(patch(s))

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
