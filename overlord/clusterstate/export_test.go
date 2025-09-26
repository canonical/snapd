// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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

package clusterstate

import (
	"context"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

// export for tests
var (
	TaskAssembleClusterSetup = taskAssembleClusterSetup
)

// AssembleClusterSetup is exported for tests
type AssembleClusterSetup = assembleClusterSetup

func MockInstallWithGoal(f func(context.Context, *state.State, snapstate.InstallGoal, snapstate.Options) ([]*snap.Info, []*state.TaskSet, error)) func() {
	restore := testutil.Backup(&installWithGoal)
	installWithGoal = f
	return restore
}

func MockRemoveMany(f func(*state.State, []string, *snapstate.RemoveFlags) ([]string, []*state.TaskSet, error)) func() {
	restore := testutil.Backup(&removeMany)
	removeMany = f
	return restore
}

func MockSnapstateUpdateWithGoal(f func(context.Context, *state.State, snapstate.UpdateGoal, func(*snap.Info, *snapstate.SnapState) bool, snapstate.Options) ([]string, *snapstate.UpdateTaskSets, error)) func() {
	restore := testutil.Backup(&updateWithGoal)
	updateWithGoal = f
	return restore
}

func MockDevicestateSerial(f func(*state.State) (*asserts.Serial, error)) func() {
	restore := testutil.Backup(&devicestateSerial)
	devicestateSerial = f
	return restore
}
