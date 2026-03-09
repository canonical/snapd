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

// currentSeedSnapNames returns the snap names that should be treated as part
// of the current seed.
func currentSeedSnapNames(st *state.State, providedDeviceCtx DeviceContext) (map[string]bool, error) {
	deviceCtx, err := DeviceCtx(st, nil, providedDeviceCtx)
	if err != nil {
		return nil, err
	}

	names := make(map[string]bool)
	for _, sn := range deviceCtx.Model().AllSnaps() {
		names[sn.SnapName()] = true
	}

	// some models have an implicit snapd, make sure that we account for it here
	names["snapd"] = true

	return names, nil
}

// seedRefreshEarlyDownloads checks if the experimental seed-refresh feature
// flag is set, and if so, returns the set of seed snaps that are being
// refreshed and should be treated as early-downloads.
func seedRefreshEarlyDownloads(st *state.State, stss []snapInstallTaskSet, deviceCtx DeviceContext) (map[string]bool, error) {
	tr := config.NewTransaction(st)
	seedRefresh, err := features.Flag(tr, features.SeedRefresh)
	if err != nil && !config.IsNoOption(err) {
		return nil, err
	}

	if !seedRefresh {
		return nil, nil
	}

	seedSnaps, err := currentSeedSnapNames(st, deviceCtx)
	if err != nil {
		return nil, err
	}

	earlyDownloads := make(map[string]bool, len(stss))
	for _, sts := range stss {
		name := sts.snapsup.InstanceName()
		if seedSnaps[name] {
			earlyDownloads[name] = true
		}
	}

	return earlyDownloads, nil
}
