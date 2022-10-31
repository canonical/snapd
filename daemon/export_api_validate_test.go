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

package daemon

import (
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/state"
)

type (
	ValidationSetResult = validationSetResult
)

func MockCheckInstalledSnaps(f func(vsets *snapasserts.ValidationSets, snaps []*snapasserts.InstalledSnap, ignoreValidation map[string]bool) error) func() {
	old := checkInstalledSnaps
	checkInstalledSnaps = f
	return func() {
		checkInstalledSnaps = old
	}
}

func MockAssertstateMonitorValidationSet(f func(st *state.State, accountID, name string, sequence int, userID int) (*assertstate.ValidationSetTracking, error)) func() {
	old := assertstateMonitorValidationSet
	assertstateMonitorValidationSet = f
	return func() {
		assertstateMonitorValidationSet = old
	}
}

func MockAssertstateFetchEnforceValidationSet(f func(st *state.State, accountID, name string, sequence int, userID int, snaps []*snapasserts.InstalledSnap, ignoreValidation map[string]bool) (*assertstate.ValidationSetTracking, error)) func() {
	old := assertstateFetchAndApplyEnforcedValidationSet
	assertstateFetchAndApplyEnforcedValidationSet = f
	return func() {
		assertstateFetchAndApplyEnforcedValidationSet = old
	}
}
