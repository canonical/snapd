// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package snapstate

import (
	"errors"

	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/state"
)

// SeedRefreshTaskSet carries the tasks needed to perform a seed refresh.
type SeedRefreshTaskSet struct {
	Create   *state.Task
	Finalize *state.Task

	// TODO: this will also carry the tasks that will remove any seeds that
	// should no longer be tracked by the seed-refresh mode
}

// SeedRefreshTasks is set by devicestate to avoid an import cycle. See
// devicestate.SeedRefreshTasks.
var SeedRefreshTasks = func(st *state.State, snapSetupTasks, compSetupTasks []string) (*SeedRefreshTaskSet, error) {
	panic("internal error: snapstate.SeedRefreshTasks is unset")
}

// seedRefreshEnabled reports whether the experimental seed-refresh feature is
// enabled.
func seedRefreshEnabled(st *state.State) (bool, error) {
	tr := config.NewTransaction(st)
	seedRefresh, err := features.Flag(tr, features.SeedRefresh)
	if err != nil && !config.IsNoOption(err) {
		return false, err
	}
	return seedRefresh, nil
}

// seedRefreshTasksAndUpdates returns the seed-refresh tasks and the task sets
// for snaps that are involved in the seed refresh.
func seedRefreshTasksAndUpdates(st *state.State, stss []snapInstallTaskSet, deviceCtx DeviceContext) (*SeedRefreshTaskSet, map[string]snapInstallTaskSet, error) {
	enabled, err := seedRefreshEnabled(st)
	if err != nil {
		return nil, nil, err
	}

	if !enabled {
		return nil, nil, nil
	}

	deviceCtx, err = DeviceCtx(st, nil, deviceCtx)
	if err != nil {
		return nil, nil, err
	}

	seedSnapUpdates := seedSnapsToUpdate(stss, deviceCtx)

	// none of the seed snaps are being updated, in that case there isn't
	// anything to do.
	if len(seedSnapUpdates) == 0 {
		return nil, nil, nil
	}

	seedSnapTSS := make([]snapInstallTaskSet, 0, len(seedSnapUpdates))
	for _, sts := range stss {
		name := sts.snapsup.InstanceName()
		if _, ok := seedSnapUpdates[name]; ok {
			seedSnapTSS = append(seedSnapTSS, sts)
		}
	}

	seedTS, err := seedRefreshTasksFromUpdates(st, seedSnapTSS)
	if err != nil {
		return nil, nil, err
	}

	return seedTS, seedSnapUpdates, nil
}

// setupTaskIDsForSeedCreation collects the snap and component setup task IDs
// that seed creation should consume.
func setupTaskIDsForSeedCreation(stss []snapInstallTaskSet) (snapsupIDs, compsupIDs []string, err error) {
	for _, sts := range stss {
		t, err := sts.ts.Edge(SnapSetupEdge)
		if err != nil {
			return nil, nil, err
		}

		snapsup, err := TaskSnapSetup(t)
		if err != nil {
			return nil, nil, err
		}

		if !snapsup.ComponentExclusiveOperation {
			snapsupIDs = append(snapsupIDs, t.ID())
		}

		var compsups []string
		if err := t.Get("component-setup-tasks", &compsups); err != nil && !errors.Is(err, state.ErrNoState) {
			return nil, nil, err
		}

		compsupIDs = append(compsupIDs, compsups...)
	}

	return snapsupIDs, compsupIDs, nil
}

// seedRefreshTasksFromUpdates builds the seed-refresh task set from the given
// refresh task sets.
func seedRefreshTasksFromUpdates(st *state.State, stss []snapInstallTaskSet) (*SeedRefreshTaskSet, error) {
	snapsupIDs, compsupIDs, err := setupTaskIDsForSeedCreation(stss)
	if err != nil {
		return nil, err
	}

	seedTS, err := SeedRefreshTasks(st, snapsupIDs, compsupIDs)
	if err != nil {
		return nil, err
	}

	return seedTS, nil
}

// seedSnapsToUpdate returns the task sets that correspond to snaps present in
// the model.
func seedSnapsToUpdate(stss []snapInstallTaskSet, dctx DeviceContext) map[string]snapInstallTaskSet {
	seedSnaps := make(map[string]bool)
	for _, sn := range dctx.Model().AllSnaps() {
		seedSnaps[sn.SnapName()] = true
	}

	// some models have an implicit snapd, make sure that we account for it here
	seedSnaps["snapd"] = true

	seedUpdates := make(map[string]snapInstallTaskSet, len(seedSnaps))
	for _, sts := range stss {
		name := sts.snapsup.InstanceName()
		if seedSnaps[name] {
			seedUpdates[name] = sts
		}
	}

	return seedUpdates
}
