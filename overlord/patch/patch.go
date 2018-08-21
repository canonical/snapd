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
	"fmt"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/state"
)

// Level is the current implemented patch level of the state format and content.
var Level = 6

// Sublevel is the current implemented sublevel for the Level. Sublevel patches do not prevent rollbacks.
var Sublevel = 1

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

func applySublevelPatches(level, start int, s *state.State) error {
	for sublevel := start; sublevel < len(patches[level]); sublevel++ {
		logger.Noticef("Patching system state from level %d, sublevel %d to sublevel %d", level, sublevel, sublevel+1)
		err := applyOne(patches[level][sublevel], s, level, sublevel+1)
		if err != nil {
			logger.Noticef("Cannot patch: %v", err)
			return fmt.Errorf("cannot patch system state to level %d, sublevel %d: %v", level, sublevel+1, err)
		}
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
		if err == state.ErrNoState && stateLevel <= 6 {
			// accommodate for the fact that sublevel patches got introduced at patch level 6.
			// if state is missing the sublevel state key, it means it's
			// actually at sublevel 1 already (we don't want to apply the 1st patch again).
			stateSublevel = 1
		}
	}
	s.Unlock()

	if err != nil && err != state.ErrNoState {
		return err
	}

	if stateLevel > Level {
		return fmt.Errorf("cannot downgrade: snapd is too old for the current system state (patch level %d)", stateLevel)
	}

	if stateLevel == Level && stateSublevel == Sublevel {
		return nil
	}

	if stateLevel == Level && stateSublevel > Sublevel {
		// downgrade within same level; update sublevel in the state so that sublevel patches
		// are re-applied if the user refreshes to a newer patch sublevel again.
		s.Lock()
		s.Set("patch-sublevel", Sublevel)
		s.Unlock()
		return nil
	}

	// apply any missing sublevel patches for current state level.
	if stateSublevel < len(patches[stateLevel]) {
		if err := applySublevelPatches(stateLevel, stateSublevel, s); err != nil {
			return err
		}
	}

	// at the lower Level - apply all new level and sublevel patches
	for level := stateLevel; level < Level; level++ {
		pp := patches[level+1]
		logger.Noticef("Patching system state from level %d to %d, sublevel", level, level+1, len(pp))
		if pp == nil {
			return fmt.Errorf("cannot upgrade: snapd is too new for the current system state (patch level %d)", level)
		}
		if err := applySublevelPatches(level+1, 0, s); err != nil {
			return err
		}
	}

	return nil
}

func applyOne(patch func(s *state.State) error, s *state.State, level, sublevel int) error {
	s.Lock()
	defer s.Unlock()

	err := patch(s)
	if err != nil {
		return err
	}

	s.Set("patch-level", level)
	s.Set("patch-sublevel", sublevel)
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
