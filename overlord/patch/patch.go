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
var Level = 7

// patches maps from patch level L to the function that moves from L-1 to L.
var patches = make(map[int]func(s *state.State) error)

// Init initializes an empty state to the current implemented patch level.
func Init(s *state.State) {
	s.Lock()
	defer s.Unlock()
	if s.Get("patch-level", new(int)) != state.ErrNoState {
		panic("internal error: expected empty state, attempting to override patch-level without actual patching")
	}
	s.Set("patch-level", Level)
}

// Apply applies any necessary patches to update the provided state to
// conventions required by the current patch level of the system.
func Apply(s *state.State) error {
	var stateLevel int
	s.Lock()
	err := s.Get("patch-level", &stateLevel)
	s.Unlock()
	if err != nil && err != state.ErrNoState {
		return err
	}
	if stateLevel == Level {
		// already at right level, nothing to do
		return nil
	}
	if stateLevel > Level {
		return fmt.Errorf("cannot downgrade: snapd is too old for the current system state (patch level %d)", stateLevel)
	}

	level := stateLevel
	for level < Level {
		logger.Noticef("Patching system state from level %d to %d", level, level+1)
		patch := patches[level+1]
		if patch == nil {
			return fmt.Errorf("cannot upgrade: snapd is too new for the current system state (patch level %d)", level)
		}
		err := applyOne(patch, s, level)
		if err != nil {
			logger.Noticef("Cannot patch: %v", err)
			return fmt.Errorf("cannot patch system state from level %d to %d: %v", level, level+1, err)
		}
		level++
	}

	return nil
}

func applyOne(patch func(s *state.State) error, s *state.State, level int) error {
	s.Lock()
	defer s.Unlock()

	err := patch(s)
	if err != nil {
		return err
	}

	s.Set("patch-level", level+1)
	return nil
}

// Mock mocks the current patch level and available patches.
func Mock(level int, p map[int]func(*state.State) error) (restore func()) {
	oldLevel := Level
	oldPatches := patches
	Level = level
	patches = p
	return func() {
		Level = oldLevel
		patches = oldPatches
	}
}
