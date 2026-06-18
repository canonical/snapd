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
	"time"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/overlord/fdestate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/testutil"
)

func MockBuildID(mock string) (restore func()) {
	oldBuildID := buildID
	r := testutil.Mock(&setBuildID, func() {
		buildID = mock
	})

	return func() {
		r()
		buildID = oldBuildID
	}
}

func MockSystemdVirt(newVirt string) (restore func()) {
	oldVirt := systemdVirt
	r := testutil.Mock(&setSystemdDetectVirt, func() {
		systemdVirt = newVirt
	})

	return func() {
		r()
		systemdVirt = oldVirt
	}
}

func MockWarningsAccessors(okay func(*state.State, time.Time) int, all func(*state.State) []*state.Warning, pending func(*state.State) ([]*state.Warning, time.Time)) (restore func()) {
	oldOK := stateOkayWarnings
	oldAll := stateAllWarnings
	oldPending := statePendingWarnings
	stateOkayWarnings = okay
	stateAllWarnings = all
	statePendingWarnings = pending
	return func() {
		stateOkayWarnings = oldOK
		stateAllWarnings = oldAll
		statePendingWarnings = oldPending
	}
}

func MockSnapdtoolsIsReexecd(f func() (bool, error)) (restore func()) {
	return testutil.Mock(&snapdtoolIsReexecd, f)
}

func MockFdestateSystemState(f func(*state.State, *asserts.Model) (*fdestate.FDESystemState, error)) (restore func()) {
	old := fdestateSystemState
	fdestateSystemState = f
	return func() { fdestateSystemState = old }
}
