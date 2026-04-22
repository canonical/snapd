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
	"fmt"

	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/state"
)

// SeedRefreshTaskSet carries the tasks needed to perform a seed refresh.
type SeedRefreshTaskSet struct {
	Create   *state.Task
	Finalize *state.Task
	Remove   []*state.Task
}

// SeedRefreshTasks is set by devicestate to avoid an import cycle. See
// devicestate.SeedRefreshTasks.
var SeedRefreshTasks = func(st *state.State, snapSetupTasks, compSetupTasks []string) (*SeedRefreshTaskSet, error) {
	panic("internal error: snapstate.SeedRefreshTasks is unset")
}

// AppendSeedRefreshSetupTaskIDs is set by devicestate to avoid an import
// cycle. See devicestate.AppendSeedRefreshSetupTaskIDs.
var AppendSeedRefreshSetupTaskIDs = func(create *state.Task, snapSetupTask string, compSetupTasks []string) error {
	panic("internal error: snapstate.AppendSeedRefreshSetupTaskIDs is unset")
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

func changeHasPendingSeedRefresh(chg *state.Change) bool {
	for _, t := range chg.Tasks() {
		switch t.Kind() {
		case "create-recovery-system", "finalize-recovery-system":
			if !t.Status().Ready() {
				return true
			}
		}
	}

	return false
}

// seedRefreshAndSeedSnapTaskSets returns the seed-refresh tasks and the task
// sets for snaps that are involved in the seed refresh.
func seedRefreshAndSeedSnapTaskSets(st *state.State, stss []snapInstallTaskSet, opts Options) (*SeedRefreshTaskSet, map[string]snapInstallTaskSet, error) {
	enabled, err := seedRefreshEnabled(st)
	if err != nil {
		return nil, nil, err
	}

	if !enabled {
		return nil, nil, nil
	}

	// if the tasks here are being created from within a change that is still
	// performing a seed refresh, then we don't want to create another one.
	if chg := st.Change(opts.FromChange); chg != nil && changeHasPendingSeedRefresh(chg) {
		return nil, nil, nil
	}

	deviceCtx, err := DeviceCtx(st, nil, opts.DeviceCtx)
	if err != nil {
		return nil, nil, err
	}

	seedSnapTaskSets := taskSetsForSeedSnaps(stss, deviceCtx)

	// none of the seed snaps are being updated, in that case there isn't
	// anything to do.
	if len(seedSnapTaskSets) == 0 {
		return nil, nil, nil
	}

	snapsupIDs, compsupIDs, err := setupTaskIDsForSeedCreation(seedSnapTaskSets)
	if err != nil {
		return nil, nil, err
	}

	seedTS, err := SeedRefreshTasks(st, snapsupIDs, compsupIDs)
	if err != nil {
		return nil, nil, err
	}

	return seedTS, seedSnapTaskSets, nil
}

// setupTaskIDsForSeedCreation collects the snap and component setup task IDs
// that seed creation should consume.
func setupTaskIDsForSeedCreation(seedSnapUpdates map[string]snapInstallTaskSet) (snapsupIDs, compsupIDs []string, err error) {
	for _, sts := range seedSnapUpdates {
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

// maybeMergeLateSeedRefreshPrereq folds a prerequisite refresh into an
// in-flight seed refresh when the current change still has pending
// recovery-system tasks and the prerequisite snap is part of the model.
func maybeMergeLateSeedRefreshPrereq(chg *state.Change, dctx DeviceContext, snapName string, providerTS *state.TaskSet) error {
	if !changeHasPendingSeedRefresh(chg) {
		return nil
	}

	// TODO:SEEDREFRESH: consider the intersections of snaps in the model and
	// snaps currently present in the seed, not all snaps in the model
	for _, sn := range dctx.Model().AllSnaps() {
		if snapName != sn.SnapName() {
			continue
		}

		// TODO:SEEDREFRESH: drop this check
		if err := errorIfPrereqNeedsInFlightBaseBlockedBySeedCreation(chg, providerTS); err != nil {
			return err
		}

		return mergeLateSeedRefreshPrereq(chg, providerTS)
	}

	return nil
}

func findRecoverySystemTasks(chg *state.Change) (create, finalize *state.Task, err error) {
	for _, t := range chg.Tasks() {
		switch t.Kind() {
		case "create-recovery-system":
			create = t
		case "finalize-recovery-system":
			finalize = t
		}
	}

	if create == nil || finalize == nil {
		return nil, nil, errors.New("internal error: seed-refresh change is missing recovery-system tasks")
	}

	return create, finalize, nil
}

// errorIfPrereqNeedsInFlightBaseBlockedBySeedCreation rejects the currently
// unsupported case where a prerequisite refresh depends on a base refresh whose
// link-snap is ordered after create-recovery-system. Without extra
// synchronization, the prerequisite refresh would wait forever on that base.
func errorIfPrereqNeedsInFlightBaseBlockedBySeedCreation(chg *state.Change, providerTS *state.TaskSet) error {
	snapsupTask, err := providerTS.Edge(SnapSetupEdge)
	if err != nil {
		return errors.New("internal error: seed-refresh provider task set is missing required edge")
	}

	snapsup, err := TaskSnapSetup(snapsupTask)
	if err != nil {
		return err
	}

	base := snapsup.Base
	if base == "none" {
		return nil
	}
	if base == "" {
		base = defaultCoreSnapName
	}

	create, _, err := findRecoverySystemTasks(chg)
	if err != nil {
		return err
	}

	baseLink, err := maybeFindTaskInChangeForSnap(chg, "link-snap", base)
	if err != nil {
		return err
	}
	if baseLink == nil || !willWaitOn(baseLink, create) {
		return nil
	}

	// TODO:SEEDREFRESH: introduce new form of prerequisite synchronization that
	// lets a late prerequisite refresh account for a base refresh whose
	// link-snap is ordered after create-recovery-system. without that extra
	// ordering, the prerequisite task keeps retrying forever on the in-flight
	// base link-snap.
	return fmt.Errorf("cannot automatically update prerequisite %q during seed-refresh while base %q waits for create-recovery-system", snapsup.InstanceName(), base)
}

// mergeLateSeedRefreshPrereq folds a prerequisite refresh into the existing
// seed-refresh task graph by adding its setup tasks to create-recovery-system,
// joining its task set to the seed-refresh lanes, and ensuring seed creation
// tasks depend on the prerequisite refresh tasks.
func mergeLateSeedRefreshPrereq(chg *state.Change, providerTS *state.TaskSet) error {
	create, finalize, err := findRecoverySystemTasks(chg)
	if err != nil {
		return err
	}

	snapsup, err := providerTS.Edge(SnapSetupEdge)
	if err != nil {
		return errors.New("internal error: seed-refresh provider task set is missing required edge")
	}

	var compsups []string
	if err := snapsup.Get("component-setup-tasks", &compsups); err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}

	if err := AppendSeedRefreshSetupTaskIDs(create, snapsup.ID(), compsups); err != nil {
		return err
	}

	for _, lane := range create.Lanes() {
		providerTS.JoinLane(lane)
	}

	end, err := providerTS.Edge(EndEdge)
	if err != nil {
		return errors.New("internal error: seed-refresh provider task set is missing required edge")
	}

	lastBeforeLocal, err := providerTS.Edge(LastBeforeLocalModificationsEdge)
	if err != nil {
		return errors.New("internal error: seed-refresh provider task set is missing required edge")
	}

	// TODO:SEEDREFRESH: what about content-providers that are essential snaps?
	// this ordering is probably too weak, since essential snap updates trigger
	// reboots that would interfere with the original single-reboot
	// orchestration. however, this is not a uniquely seed-refresh problem.

	waitForIfNeeded(create, lastBeforeLocal)
	waitForIfNeeded(finalize, end)

	return nil
}

// taskSetsForSeedSnaps returns the selected refresh task sets keyed by
// snap name for snaps present in the model.
func taskSetsForSeedSnaps(stss []snapInstallTaskSet, dctx DeviceContext) map[string]snapInstallTaskSet {
	// TODO:SEEDREFRESH: consider the intersections of snaps in the model and
	// snaps currently present in the seed, not all snaps in the model
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
