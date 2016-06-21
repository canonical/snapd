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

	"github.com/snapcore/snapd/overlord/state"
)

// patchLevel is the current implemented patch level of the state format and content.
var patchLevel = 0

// PatchLevel returns the implemented patch level for state format and content.
func PatchLevel() int {
	return patchLevel
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
		err := runMigration(s, level)
		if err != nil {
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

	commit := func() {
		s.Set("patch-level", level+1)
		s.Unlock()
	}

	err := m(s, commit)
	if err != nil {
		return err
	}

	return nil
}

// migrations: maps from L to the migration implementation from patch
// level L to L+1
// migrations take a commit function that assumes the
// state is locked and should typically look like:
//
//func mLToL+1(s *state.State, commit func()) error {
// 	s.Lock()
// 	// get data to migrate from state...
// 	s.Unlock()
//
// 	// prepare migrated data, can return error
//
// 	s.Lock()
// 	// store back migrated data into state with s.Set etc, no error paths
// 	commit()
//}
var migrations = map[int]func(s *state.State, commit func()) error{}
