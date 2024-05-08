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

	"github.com/snapcore/snapd/overlord/state"
)

func MockBuildID(mock string) (restore func()) {
	old := buildID
	buildID = mock
	return func() {
		buildID = old
	}
}

func MockSystemdVirt(newVirt string) (restore func()) {
	oldVirt := systemdVirt
	systemdVirt = newVirt
	return func() { systemdVirt = oldVirt }
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

type (
	ChangeInfo = changeInfo
)

func MockSnapstateSnapsAffectedByTask(f func(t *state.Task) ([]string, error)) (restore func()) {
	old := snapstateSnapsAffectedByTask
	snapstateSnapsAffectedByTask = f
	return func() {
		snapstateSnapsAffectedByTask = old
	}
}
