// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package devicestate

import (
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
)

func checkSystemRequestConflict(st *state.State, systemLabel string) error {
	st.Lock()
	defer st.Unlock()

	var seeded bool
	if err := st.Get("seeded", &seeded); err != nil && err != state.ErrNoState {
		return err
	}
	if seeded {
		// the system is fully seeded already
		return nil
	}

	// not yet fully seeded, hold off requests for the system that is being
	// seeded, but allow requests for other systems
	var isSeeding bool
	var whatSeeds *seededSystem
changesLoop:
	for _, chg := range st.Changes() {
		if chg.Kind() != "seed" {
			continue
		}
		isSeeding = true
		if chg.Status().Ready() {
			// change is done but 'seeded' was unset, perhaps it
			// errored
			return nil
		}
		for _, t := range chg.Tasks() {
			if t.Kind() != "mark-seeded" {
				continue
			}
			if err := t.Get("seed-system", &whatSeeds); err != nil && err != state.ErrNoState {
				return err
			}
			break changesLoop
		}
	}
	if whatSeeds != nil && whatSeeds.System == systemLabel {
		//
		return &snapstate.ChangeConflictError{Message: "cannot request system action, system is seeding"}
	}
	if !isSeeding {
		// seeding not yet started, error out just in case the same
		// system is seeded
		return &snapstate.ChangeConflictError{Message: "cannot request system action, seeding not started yet"}
	}
	return nil
}
