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
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/state"
)

type (
	ValidationSetResult = validationSetResult
)

func MockCheckInstalledSnaps(f func(vsets *snapasserts.ValidationSets, snaps []*snapasserts.InstalledSnap) error) func() {
	old := checkInstalledSnaps
	checkInstalledSnaps = f
	return func() {
		checkInstalledSnaps = old
	}
}

func MockValidationSetAssertionForMonitor(f func(st *state.State, accountID, name string, sequence int, pinned bool, userID int, opts *assertstate.ResolveOptions) (*asserts.ValidationSet, bool, error)) func() {
	old := validationSetAssertionForMonitor
	validationSetAssertionForMonitor = f
	return func() {
		validationSetAssertionForMonitor = old
	}
}
