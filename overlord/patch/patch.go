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

// Sublevel is the current implemented sublevel. Sublevel patches do not prevent rollbacks and should
// only add data in a way that's backwards compatible in case of rollbacks. They assume patch Level 6
// since no Level patches are to be added anymore.
var Sublevel = 0

// patches maps from patch level L to the function that moves from L-1 to L.
var patches = make(map[int]func(s *state.State) error)

// sublevel patches maps from patch sub-level L to the function that moves from L-1 to L.
var sublevelPatches = make(map[int]func(s *state.State) error)

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

	if stateLevel == Level && stateSublevel == Sublevel {
		// already at right level and sublevel, nothing to do
		return nil
	}
	if stateLevel > Level {
		return fmt.Errorf("cannot downgrade: snapd is too old for the current system state (patch level %d)", stateLevel)
	}

	for level := stateLevel; level < Level; level++ {
		logger.Noticef("Patching system state from level %d to %d", level, level+1)
		patch := patches[level+1]
		if patch == nil {
			return fmt.Errorf("cannot upgrade: snapd is too new for the current system state (patch level %d)", level)
		}
		err := applyOne(patch, s, "patch-level", level)
		if err != nil {
			logger.Noticef("Cannot patch: %v", err)
			return fmt.Errorf("cannot patch system state from level %d to %d: %v", level, level+1, err)
		}
	}

	// sub level patches assume patch level 6; we don't implement level==6 check here as we know we're at level 6.
	for sublevel := stateSublevel; sublevel < Sublevel; sublevel++ {
		logger.Noticef("Patching system state from sublevel %d to %d", sublevel, sublevel+1)
		patch := sublevelPatches[sublevel+1]
		if patch == nil {
			return fmt.Errorf("cannot upgrade: snapd is too new for the current system state (patch sublevel %d)", sublevel)
		}
		err := applyOne(patch, s, "patch-sublevel", sublevel)
		if err != nil {
			logger.Noticef("Cannot patch: %v", err)
			return fmt.Errorf("cannot patch system state from sublevel %d to %d: %v", sublevel, sublevel+1, err)
		}
	}

	return nil
}

func applyOne(patch func(s *state.State) error, s *state.State, patchStateKey string, level int) error {
	s.Lock()
	defer s.Unlock()

	err := patch(s)
	if err != nil {
		return err
	}

	s.Set(patchStateKey, level+1)
	return nil
}

// Mock mocks the current patch level and available patches.
func Mock(level int, p map[int]func(*state.State) error, sublevel int, sp map[int]func(*state.State) error) (restore func()) {
	oldLevel := Level
	oldPatches := patches
	Level = level
	patches = p

	oldSublevel := Sublevel
	oldSublevelPatches := sublevelPatches
	Sublevel = sublevel
	sublevelPatches = sp

	return func() {
		Level = oldLevel
		patches = oldPatches
		Sublevel = oldSublevel
		sublevelPatches = oldSublevelPatches
	}
}
