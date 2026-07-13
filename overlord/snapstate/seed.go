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
	"github.com/snapcore/snapd/snap"
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
	// inputs to recovery system creation. Will be empty for snap-only
	// refreshes.
	ComponentSetupTaskIDs []string
}

// SeedRefreshTasks is set by devicestate to avoid an import cycle. See
// devicestate.SeedRefreshTasks.
var SeedRefreshTasks = func(st *state.State, dctx DeviceContext, candidates []SeedRefreshCandidate, eviction SeedRefreshEvictionPolicy) (*SeedRefreshTaskSet, map[string]bool, error) {
	panic("internal error: snapstate.SeedRefreshTasks is unset")
}

// PendingSeedRefreshTasks is set by devicestate to avoid an import cycle. See
// devicestate.PendingSeedRefreshTasks.
var PendingSeedRefreshTasks = func(ts *state.TaskSet) (*SeedRefreshTaskSet, error) {
	panic("internal error: snapstate.PendingSeedRefreshTasks is unset")
}

// UpdateSeedRefreshChange is set by devicestate to avoid an import cycle. See
// devicestate.UpdateSeedRefreshChange.
var UpdateSeedRefreshChange = func(seedTS *SeedRefreshTaskSet, dctx DeviceContext, candidate SeedRefreshCandidate) (bool, error) {
	panic("internal error: snapstate.UpdateSeedRefreshChange is unset")
}

// CheckSeedRefreshRemove is set by devicestate to prevent removal of snaps that
// must remain present for seed-refresh.
//
// TODO:SEEDREFRESH: remove this hook once seed-refresh supports seeds
// gaining/losing snaps
var CheckSeedRefreshRemove = func(st *state.State, si *snap.Info, dctx DeviceContext) error {
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

	candidate := SeedRefreshCandidate{
		InstanceName: snapsup.InstanceName(),
	}
	if !snapsup.ComponentExclusiveOperation {
		candidate.SnapSetupTaskIDs = append(candidate.SnapSetupTaskIDs, t.ID())
	}

	if err := t.Get("component-setup-tasks", &candidate.ComponentSetupTaskIDs); err != nil && !errors.Is(err, state.ErrNoState) {
		return SeedRefreshCandidate{}, err
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

// seedRefreshAndSeedSnapTaskSets returns the seed-refresh tasks and the task
// sets for snaps that are involved in the seed refresh.
func seedRefreshAndSeedSnapTaskSets(st *state.State, stss []snapInstallTaskSet, eviction SeedRefreshEvictionPolicy, opts Options) (*SeedRefreshTaskSet, map[string]snapInstallTaskSet, error) {
	// try mode doesn't actually install the snap, should never trigger a
	// seed-refresh
	if opts.Flags.TryMode || opts.NoSeedRefresh {
		return nil, nil, nil
	}

	enabled, err := seedRefreshEnabled(st)
	if err != nil {
		return nil, nil, err
	}

	if !enabled {
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

// maybeMergeLateSeedRefreshPrereq orders a late prerequisite refresh against an
// in-flight seed refresh. The initial prerequisites task must run before seed
// creation even when the snap is not part of the seed refresh. If the snap is
// part of the seed refresh, then the before-local-modification tasks for that
// snap will be ordered before seed creation.
func maybeMergeLateSeedRefreshPrereq(chg *state.Change, seedTS *SeedRefreshTaskSet, dctx DeviceContext, ts *state.TaskSet) error {
	// if this task set already carries the seed creation tasks, then there
	// isn't anything more to do
	for _, t := range ts.Tasks() {
		if t.ID() == seedTS.Create.ID() {
			return nil
		}
	}

	var prereq *state.Task
	for _, t := range ts.Tasks() {
		if t.Kind() == "prerequisites" && !t.Has("prerequisites-sync") {
			prereq = t
			break
		}
	}
	if prereq == nil {
		return errors.New("internal error: seed-refresh provider task set is missing initial prerequisites task")
	}

	candidate, err := seedRefreshCandidateForTaskSet(ts)
	if err != nil {
		return err
	}

	added, err := UpdateSeedRefreshChange(seedTS, dctx, candidate)
	if err != nil {
		return err
	}

	if !added {
		// seed creation must wait on all prerequisite tasks spawned by a refresh.
		// this ensures that we've recursively resolved all prerequisites prior to
		// seed creation.
		for _, lane := range seedTS.Create.Lanes() {
			prereq.JoinLane(lane)
		}
		seedTS.Create.WaitFor(prereq)

		return nil
	}

	// TODO:SEEDREFRESH: if a snap going into the refreshed seed starts
	// requiring a base or content provider that was not in the existing
	// seed, supporting it would require the refreshed seed to gain this
	// prerequisite snap. this is intentionally unsupported for now. adding an
	// early failure here would be nice, but we'd have to open the seed. a
	// seed-refresh that hits this case will fail during seed creation.

	if err := errorIfPrereqNeedsInFlightBaseBlockedBySeedCreation(chg, seedTS, ts); err != nil {
		return err
	}

	return mergeLateSeedRefreshPrereq(seedTS, ts)
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
	end, err := providerTS.Edge(EndEdge)
	if err != nil {
		return errors.New("internal error: seed-refresh provider task set is missing required edge")
	}

	lastBeforeLocal, err := providerTS.Edge(LastBeforeLocalModificationsEdge)
	if err != nil {
		return errors.New("internal error: seed-refresh provider task set is missing required edge")
	}

	// joining the full task set to the seed lanes ensures that undoing the snap
	// also undoes the seed refresh.
	for _, lane := range seedTS.Create.Lanes() {
		for _, t := range providerTS.Tasks() {
			t.JoinLane(lane)
		}
	}

	// TODO:SEEDREFRESH: what about content-providers that are essential snaps?
	// this ordering is probably too weak, since essential snap updates trigger
	// reboots that would interfere with the original single-reboot
	// orchestration. however, this is not a uniquely seed-refresh problem.

	waitForIfNeeded(seedTS.Create, lastBeforeLocal)
	waitForIfNeeded(seedTS.Finalize, end)

	return nil
}
