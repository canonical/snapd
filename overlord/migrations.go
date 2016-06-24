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

package overlord

import (
	"fmt"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
)

// patchLevel is the current implemented patch level of the state format and content.
var patchLevel = 2

// PatchLevel returns the implemented patch level for state format and content.
func PatchLevel() int {
	return patchLevel
}

// migrations maps from patch level L to migration function for L to L+1.
// Migration functions are run with the state lock held.
//
// Note that you must increase the "patchLevel" if you add something here.
var migrations = map[int]func(s *state.State) error{
	// backfill SnapStates with types
	0: snapstate.MigrateToTypeInState,
	// backfill SnapStates with Current revision
	1: snapstate.MigrateToCurrentRevision,
}

// initialize state at the current implemented patch level.
func initialize(s *state.State) {
	s.Lock()
	defer s.Unlock()
	s.Set("patch-level", patchLevel)
}

// migrate executes migrations that bridge state format changes as
// identified by patch level increments that can be dealt with as
// one-shot.
func migrate(s *state.State) error {
	var level int
	s.Lock()
	err := s.Get("patch-level", &level)
	s.Unlock()
	if err != nil && err != state.ErrNoState {
		return err
	}
	if level == patchLevel {
		// already at right level, nothing to do
		return nil
	}
	if level > patchLevel {
		return fmt.Errorf("cannot downgrade: snapd is too old for the current state patch level %d", level)
	}

	for level != patchLevel {
		logger.Noticef("Running migration from state patch level %d to %d", level, level+1)
		err := runMigration(s, level)
		if err != nil {
			logger.Noticef("Cannnot migrate: %v", err)
			return fmt.Errorf("cannot migrate from state patch level %d to %d: %v", level, level+1, err)
		}
		level++
	}

	return nil
}

func runMigration(s *state.State, level int) error {
	m := migrations[level]
	if m == nil {
		return fmt.Errorf("no supported migration")
	}
	s.Lock()
	defer s.Unlock()

	err := m(s)
	if err != nil {
		return err
	}

	s.Set("patch-level", level+1)

	return nil
}
