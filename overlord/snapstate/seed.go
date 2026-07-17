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

// SeedRefreshEvictionPolicy carries the seed-refresh system pruning policy
// selected by snapstate for the current operation.
type SeedRefreshEvictionPolicy struct {
	// SeedsToRetain is the number of existing seed-refresh systems to keep.
	SeedsToRetain int
	// ReplaceLatest forces removal of the newest existing seed-refresh system.
	ReplaceLatest bool
}

// SeedRefreshCandidate carries information about a snap that might trigger a
// seed refresh.
type SeedRefreshCandidate struct {
	// InstanceName is the snap's instance name.
	InstanceName string
	// SnapSetupTaskIDs are the snap tasks that should be considered as inputs to
	// recovery system creation. Will be empty for component-only refreshes.
	SnapSetupTaskIDs []string
	// ComponentSetupTaskIDs are the component tasks that should be considered as
	// inputs to recovery system creation where the key is the component name.
	// Will be empty for snap-only refreshes.
	ComponentSetupTaskIDs map[string]string
}

// SeedRefreshTasks is set by devicestate to avoid an import cycle. See
// devicestate.SeedRefreshTasks.
var SeedRefreshTasks = func(st *state.State, dctx DeviceContext, candidates []SeedRefreshCandidate, eviction SeedRefreshEvictionPolicy) (*SeedRefreshTaskSet, map[string]bool, error) {
	panic("internal error: snapstate.SeedRefreshTasks is unset")
}

// UpdateSeedRefreshChange is set by devicestate to avoid an import cycle. See
// devicestate.UpdateSeedRefreshChange.
var UpdateSeedRefreshChange = func(chg *state.Change, dctx DeviceContext, candidate SeedRefreshCandidate) (*SeedRefreshTaskSet, error) {
	panic("internal error: snapstate.UpdateSeedRefreshChange is unset")
}

// CheckSeedRefreshRemove is set by devicestate to prevent removal of snaps that
// must remain present for seed-refresh.
//
// TODO:SEEDREFRESH: remove this hook once seed-refresh supports seeds
// gaining/losing snaps
var CheckSeedRefreshRemove = func(st *state.State, candidate SeedRefreshCandidate, dctx DeviceContext) error {
	panic("internal error: snapstate.CheckSeedRefreshRemove is unset")
}

func seedRefreshCandidateForTaskSet(ts *state.TaskSet) (SeedRefreshCandidate, error) {
	t, err := ts.Edge(SnapSetupEdge)
	if err != nil {
		return SeedRefreshCandidate{}, err
	}

	snapsup, err := TaskSnapSetup(t)
	if err != nil {
		return SeedRefreshCandidate{}, err
	}

	filter := func(id string) *state.Task {
		for _, t := range ts.Tasks() {
			if t.ID() == id {
				return t
			}
		}
		return nil
	}

	var compsupTaskIDs []string
	if err := t.Get("component-setup-tasks", &compsupTaskIDs); err != nil && !errors.Is(err, state.ErrNoState) {
		return SeedRefreshCandidate{}, err
	}
	compSetupTaskIDs := make(map[string]string)
	for _, id := range compsupTaskIDs {
		compsupTask := filter(id)
		if compsupTask == nil {
			return SeedRefreshCandidate{}, err
		}
		var compSetup ComponentSetup
		err := compsupTask.Get("component-setup", &compSetup)
		if err != nil {
			return SeedRefreshCandidate{}, err
		}
		compSetupTaskIDs[compSetup.ComponentName()] = id
	}

	candidate := SeedRefreshCandidate{
		InstanceName: snapsup.InstanceName(),
	}
	if len(compSetupTaskIDs) > 0 {
		candidate.ComponentSetupTaskIDs = compSetupTaskIDs
	}
	if !snapsup.ComponentExclusiveOperation {
		candidate.SnapSetupTaskIDs = append(candidate.SnapSetupTaskIDs, t.ID())
	}

	return candidate, nil
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
func seedRefreshAndSeedSnapTaskSets(st *state.State, stss []snapInstallTaskSet, eviction SeedRefreshEvictionPolicy, opts Options) (*SeedRefreshTaskSet, map[string]snapInstallTaskSet, error) {
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

	candidates := make([]SeedRefreshCandidate, 0, len(stss))
	for _, sts := range stss {
		candidate, err := seedRefreshCandidateForTaskSet(sts.ts)
		if err != nil {
			return nil, nil, err
		}
		candidates = append(candidates, candidate)
	}

	seedTS, added, err := SeedRefreshTasks(st, deviceCtx, candidates, eviction)
	if err != nil {
		return nil, nil, err
	}
	if len(added) == 0 {
		return nil, nil, nil
	}

	seedSnapTaskSets := make(map[string]snapInstallTaskSet, len(added))
	for _, sts := range stss {
		if added[sts.snapsup.InstanceName()] {
			seedSnapTaskSets[sts.snapsup.InstanceName()] = sts
		}
	}

	return seedTS, seedSnapTaskSets, nil
}

// maybeMergeLateSeedRefreshPrereq folds a prerequisite refresh into an
// in-flight seed refresh when the current change still has pending
// recovery-system tasks and the prerequisite snap is part of the model.
func maybeMergeLateSeedRefreshPrereq(chg *state.Change, dctx DeviceContext, providerTS *state.TaskSet) error {
	if !changeHasPendingSeedRefresh(chg) {
		return nil
	}

	candidate, err := seedRefreshCandidateForTaskSet(providerTS)
	if err != nil {
		return err
	}

	seedTS, err := UpdateSeedRefreshChange(chg, dctx, candidate)
	if err != nil {
		return err
	}

	// snap didn't trigger a seed refresh
	if seedTS == nil {
		return nil
	}

	// TODO:SEEDREFRESH: drop this check
	if err := errorIfPrereqNeedsInFlightBaseBlockedBySeedCreation(chg, seedTS, providerTS); err != nil {
		return err
	}

	return mergeLateSeedRefreshPrereq(seedTS, providerTS)
}

// errorIfPrereqNeedsInFlightBaseBlockedBySeedCreation rejects the currently
// unsupported case where a prerequisite refresh depends on a base refresh whose
// link-snap is ordered after create-recovery-system. Without extra
// synchronization, the prerequisite refresh would wait forever on that base.
func errorIfPrereqNeedsInFlightBaseBlockedBySeedCreation(chg *state.Change, seedTS *SeedRefreshTaskSet, providerTS *state.TaskSet) error {
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

	baseLink, err := maybeFindTaskInChangeForSnap(chg, "link-snap", base)
	if err != nil {
		return err
	}
	if baseLink == nil || !willWaitOn(baseLink, seedTS.Create) {
		return nil
	}

	// TODO:SEEDREFRESH: introduce new form of prerequisite synchronization that
	// lets a late prerequisite refresh account for a base refresh whose
	// link-snap is ordered after create-recovery-system. without that extra
	// ordering, the prerequisite task keeps retrying forever on the in-flight
	// base link-snap.
	return fmt.Errorf("cannot automatically update prerequisite %q during seed-refresh while base %q waits for create-recovery-system", snapsup.InstanceName(), base)
}

// mergeLateSeedRefreshPrereq folds a prerequisite refresh selected by
// devicestate.UpdateSeedRefreshChange into the existing seed-refresh task
// graph. At this point, devicestate has already updated the recovery-system
// setup payload. this function only joins lanes and adds the ordering
// dependencies needed by seed refresh.
func mergeLateSeedRefreshPrereq(seedTS *SeedRefreshTaskSet, providerTS *state.TaskSet) error {
	_, err := providerTS.Edge(SnapSetupEdge)
	if err != nil {
		return errors.New("internal error: seed-refresh provider task set is missing required edge")
	}

	for _, lane := range seedTS.Create.Lanes() {
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

	waitForIfNeeded(seedTS.Create, lastBeforeLocal)
	waitForIfNeeded(seedTS.Finalize, end)

	return nil
}
