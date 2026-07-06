// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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

	"github.com/snapcore/snapd/overlord/restart"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

// essentialSnapsRestartOrder describes the essential snaps that
// need restart boundaries in order.
// XXX: Snapd is not a part of this (for now), and snapd can essentially
// request a reboot when updating managed boot assets (i.e grub.cfg). This
// needs to be double-checked as right now because if snapd requests a restart
// this won't happen before the snapd update is complete (and not immediate due
// to no restart-boundaries set yet).
var essentialSnapsRestartOrder = []snap.Type{
	snap.TypeOS,
	// The base will only require restart if it's the base of the model.
	snap.TypeBase,
	snap.TypeGadget,
	// Kernel must wait for gadget because the gadget may define
	// new "$kernel:refs". Sorting the other way is impossible
	// because a kernel with new kernel-assets would never refresh
	// because the matching gadget could never get installed
	// because the gadget always waits for the kernel and if the
	// kernel aborts the wait tasks (the gadget) is put on "Hold".
	snap.TypeKernel,
}

func maybeTaskSetSnapSetup(ts *state.TaskSet) *SnapSetup {
	for _, t := range ts.Tasks() {
		snapsup, err := TaskSnapSetup(t)
		if err == nil {
			return snapsup
		}
	}
	return nil
}

func isEssentialSnap(snapName string, snapType snap.Type, bootBase string) bool {
	switch snapType {
	case snap.TypeBase, snap.TypeOS:
		if snapName == bootBase {
			return true
		}
	case snap.TypeSnapd, snap.TypeGadget, snap.TypeKernel:
		return true
	}
	return false
}

// taskSetsByTypeForEssentialSnaps returns a map of task-sets by their essential snap type, if
// a task-set for any of the essential snap exists.
func taskSetsByTypeForEssentialSnaps(tss []*state.TaskSet, bootBase string) (map[snap.Type]*state.TaskSet, error) {
	avail := make(map[snap.Type]*state.TaskSet)
	for _, ts := range tss {
		snapsup := maybeTaskSetSnapSetup(ts)
		if snapsup == nil {
			continue
		}

		if isEssentialSnap(snapsup.InstanceName(), snapsup.Type, bootBase) {
			avail[snapsup.Type] = ts
		}
	}
	return avail, nil
}

func findUnlinkTask(ts *state.TaskSet) *state.Task {
	for _, t := range ts.Tasks() {
		switch t.Kind() {
		case "unlink-snap", "unlink-current-snap":
			return t
		}
	}
	return nil
}

// setDefaultRestartBoundaries marks edge MaybeRebootEdge (Do) and task "unlink-snap"/"unlink-current-snap" (Undo)
// as restart boundaries to maintain the old restart logic. This means that a restart must be performed after
// each of those tasks in order for the change to continue.
func setDefaultRestartBoundaries(ts *state.TaskSet) {
	linkSnap := ts.MaybeEdge(MaybeRebootEdge)
	if linkSnap != nil {
		restart.MarkTaskAsRestartBoundary(linkSnap, restart.RestartBoundaryDirectionDo)
	}

	unlinkSnap := findUnlinkTask(ts)
	if unlinkSnap != nil {
		restart.MarkTaskAsRestartBoundary(unlinkSnap, restart.RestartBoundaryDirectionUndo)
	}
}

// deviceModelBootBase returns the base-snap name of the current model. For UC16
// this will return "core".
func deviceModelBootBase(st *state.State, providedDeviceCtx DeviceContext) (string, error) {
	deviceCtx, err := DeviceCtx(st, nil, providedDeviceCtx)
	if err != nil {
		if !errors.Is(err, state.ErrNoState) {
			return "", err
		}
		return "", nil
	}
	bootBase := deviceCtx.Model().Base()
	if bootBase == "" {
		return "core", nil
	}
	return bootBase, nil
}

func contains[T comparable](items []T, item T) bool {
	for _, i := range items {
		if i == item {
			return true
		}
	}
	return false
}

// waitForIfNeeded makes waiter wait on target, if there isn't already an
// implicit dependency present between the two.
func waitForIfNeeded(waiter, target *state.Task) {
	stack := append([]*state.Task(nil), waiter.WaitTasks()...)
	seen := make(map[*state.Task]bool, len(stack))
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		if cur == target {
			return
		}

		if seen[cur] {
			continue
		}
		seen[cur] = true
		stack = append(stack, cur.WaitTasks()...)
	}
	waiter.WaitFor(target)
}

// arrangeRebootAndUpdateSeed arranges the correct link-order between all the
// provided snap install task-sets, sets up restart boundaries for essential
// snaps (base, gadget, kernel), and returns the task set needed to update the
// seed when seed-refresh applies.
//
// The resulting ordering is defined here:
//
//	snapd (all tasks)
//	|
//	boot-base -> gadget -> kernel (mount-snap, when present)
//	|
//	boot-base -> gadget -> kernel (remaining pre-reboot tasks, up to and including link-snap)
//	|
//	create-recovery-system (if required)
//	|
//	boot-base -> gadget -> kernel (post-reboot tasks, everything after link-snap)
//	|
//	non-essential bases and apps
//
// Seed refresh adds a phase before create-recovery-system. The seed creation
// task waits on every snap's initial prerequisites task, because those tasks
// can schedule prerequisite seed snaps. For snaps that are part of the new
// seed, it also waits on the rest of their before-local-modifications tasks so
// devicestate can consume the selected snap/component setup tasks.
//
// finalize-recovery-system's placement is dependent upon which snaps are being
// refreshed. It runs after create-recovery-system is done, and it also waits
// for all model snaps to be finished refreshing. The set of model snaps being
// refreshed might be a combination of essential and non-essential snaps, so
// that task's placement will vary.
func arrangeRebootAndUpdateSeed(
	st *state.State,
	stss []snapInstallTaskSet,
	eviction SeedRefreshEvictionPolicy,
	opts Options,
) (seedRefreshTS *state.TaskSet, err error) {
	for _, sts := range stss {
		if sts.prerequisites == nil ||
			len(sts.beforeLocalSystemModificationsTasks) == 0 ||
			sts.prerequisitesSync == nil ||
			len(sts.upToLinkSnapAndBeforeReboot) == 0 ||
			len(sts.afterLinkSnapAndPostReboot) == 0 {
			return nil, errors.New("internal error: snap install task set has empty task ranges")
		}
	}

	// seedTS will be nil when seed-refresh is disabled or when this refresh does
	// not touch any model snaps
	//
	// note that seedSnapTaskSets will contain all snaps being refreshed that
	// will go into the seed, and it might contain a combination of essential
	// and non-essential snaps.
	seedTS, seedSnapTaskSets, err := seedRefreshAndSeedSnapTaskSets(st, stss, eviction, opts)
	if err != nil {
		return nil, err
	}

	head := func(tasks []*state.Task) *state.Task {
		return tasks[0]
	}

	tail := func(tasks []*state.Task) *state.Task {
		return tasks[len(tasks)-1]
	}

	bootBase, err := deviceModelBootBase(st, opts.DeviceCtx)
	if err != nil {
		return nil, err
	}

	// these sets of snaps are mutually exclusive and will not overlap with
	// each other
	essentials := make(map[snap.Type]snapInstallTaskSet)
	nonEssentialBases := make(map[string]snapInstallTaskSet)
	apps := make(map[string]snapInstallTaskSet)

	// categorize our snaps into a few different buckets
	for _, sts := range stss {
		switch {
		case isEssentialSnap(sts.snapsup.InstanceName(), sts.snapsup.Type, bootBase):
			essentials[sts.snapsup.Type] = sts
		case sts.snapsup.Type == snap.TypeBase || sts.snapsup.Type == snap.TypeOS:
			nonEssentialBases[sts.snapsup.InstanceName()] = sts
		case sts.snapsup.Type == snap.TypeApp:
			apps[sts.snapsup.InstanceName()] = sts
		}
	}

	// prev and chain build the ordered spine of the refresh: snapd first, then
	// the essential pre-reboot chain, then create-recovery-system when present,
	// and finally the essential post-reboot chain.
	//
	// note that these are only used for ordering, and the chain will not
	// contain all tasks involved in the refresh.
	var prev *state.Task
	chain := func(begin, end *state.Task) {
		if begin == nil || end == nil {
			if begin != end {
				panic("internal error: use of chain on partially empty range")
			}

			// nothing to do with prev here, since the caller added an empty
			// range. this is only allowed to make UC16 handling a bit easier,
			// since the post-reboot chains for UC16 are empty.

			return
		}

		if prev != nil {
			begin.WaitFor(prev)
		}
		prev = end
	}

	isUC16 := bootBase == "core"

	beforeReboot := func(sts snapInstallTaskSet) (*state.Task, *state.Task) {
		// on UC16, everything is before reboot
		if isUC16 {
			return sts.prerequisites, tail(sts.afterLinkSnapAndPostReboot)
		}

		// we let sts.beforeLocalSystemModificationsTasks happen in parallel
		return head(sts.upToLinkSnapAndBeforeReboot), tail(sts.upToLinkSnapAndBeforeReboot)
	}

	afterReboot := func(sts snapInstallTaskSet) (*state.Task, *state.Task) {
		// on UC16, nothing is after reboot
		if isUC16 {
			return nil, nil
		}
		return head(sts.afterLinkSnapAndPostReboot), tail(sts.afterLinkSnapAndPostReboot)
	}

	// enables us to require that everything start after we've swapped to the new
	// snapd, if we have one. might be nil!
	var finalSnapdTask *state.Task
	if sts, ok := essentials[snap.TypeSnapd]; ok {
		finalSnapdTask = tail(sts.afterLinkSnapAndPostReboot)
	}

	// then all the mount-snap tasks for essential snaps, in order. this applies
	// only to single-reboot systems where mount-snap is orchestrated separately
	// from the rest of the pre-reboot work.
	if !isUC16 {
		for _, t := range essentialSnapsRestartOrder {
			sts, ok := essentials[t]
			if !ok || sts.mountSnap == nil {
				continue
			}

			chain(sts.mountSnap, sts.mountSnap)
		}
	}

	// then all the remaining pre-reboot tasks for essential snaps, in order
	for _, t := range essentialSnapsRestartOrder {
		sts, ok := essentials[t]
		if !ok {
			continue
		}

		chain(beforeReboot(sts))
	}

	// insert the create-recovery-system task after the pre-reboot essential
	// chain. if we have this task, this is where the reboots will happen. one
	// reboot to test the system, and another reboot to return to the run
	// system.
	if seedTS != nil {
		chain(seedTS.Create, seedTS.Create)
	}

	// then all the post-reboot tasks for essential snaps, in order
	for _, t := range essentialSnapsRestartOrder {
		sts, ok := essentials[t]
		if !ok {
			continue
		}
		chain(afterReboot(sts))
	}

	// before doing anything else, keep a pointer to the final essential snap
	// task. we'll use this later. note, this might be nil! nothing about this
	// code requires essential snap presence.
	finalEssential := prev

	// UC16 systems enforce different reboot boundaries, which can result in
	// multiple reboots while refreshing many essential snaps.
	if !isUC16 {
		// when seed-refresh is active, the recovery-system task set sits
		// between the pre- and post-reboot essential chains and carries the do
		// boundary. thus, we only need the do boundary if we don't have a seed
		// refresh.
		if seedTS == nil {
			// set the reboot boundary on the final pre-reboot essential snap task
			for i := len(essentialSnapsRestartOrder) - 1; i >= 0; i-- {
				sts, ok := essentials[essentialSnapsRestartOrder[i]]
				if !ok {
					continue
				}

				_, end := beforeReboot(sts)
				if end == nil {
					return nil, errors.New("internal error: all essential snaps must have before boot tasks")
				}
				restart.MarkTaskAsRestartBoundary(end, restart.RestartBoundaryDirectionDo)

				break
			}
		} else {
			// we trust devicestate to properly set this reboot boundary, but
			// double check just to make sure
			if !restart.TaskIsRestartBoundary(seedTS.Create, restart.RestartBoundaryDirectionDo) {
				return nil, errors.New("internal error: seed creation task is missing expected reboot boundary")
			}
		}

		// set the reboot undo boundary on the first post-reboot essential
		// unlink-snap task
		for i := 0; i < len(essentialSnapsRestartOrder); i++ {
			sts, ok := essentials[essentialSnapsRestartOrder[i]]
			if !ok {
				continue
			}

			unlink := findUnlinkTask(sts.ts)
			if unlink == nil {
				continue
			}

			restart.MarkTaskAsRestartBoundary(unlink, restart.RestartBoundaryDirectionUndo)

			break
		}

		mergeEssentialAndSeedLanes(essentials, seedSnapTaskSets, seedTS)
	} else {
		// legacy behavior, set the do and undo reboot boundaries on all
		// essential snaps, with the exception of snapd
		for _, o := range essentialSnapsRestartOrder {
			sts, ok := essentials[o]
			if !ok {
				continue
			}

			restart.MarkTaskAsRestartBoundary(tail(sts.upToLinkSnapAndBeforeReboot), restart.RestartBoundaryDirectionDo)
			unlinkSnap := findUnlinkTask(sts.ts)
			if unlinkSnap != nil {
				restart.MarkTaskAsRestartBoundary(unlinkSnap, restart.RestartBoundaryDirectionUndo)
			}
		}
	}

	nonEssentialWaitHead := func(sts snapInstallTaskSet) *state.Task {
		// during a seed-refresh, all initial prerequisites tasks will run prior
		// to seed creation.
		if seedTS != nil {
			// for seed snaps, their before-local-modifications tasks also run
			// before seed creation, so schedule the synchronization task as the
			// first post-essential task.
			if _, ok := seedSnapTaskSets[sts.snapsup.InstanceName()]; ok {
				return sts.prerequisitesSync
			}

			// for non-seed snaps, schedule the rest of the
			// before-local-modifications phase as the first post-essential
			// task.
			return head(sts.beforeLocalSystemModificationsTasks)
		}

		// in the absence of a seed-refresh, the first post-essential tasks
		// should simply be the first task of each snap's chain of tasks.
		return sts.prerequisites
	}

	// make the bases just wait on the final essential snap to finish up
	for _, sts := range nonEssentialBases {
		if finalEssential != nil {
			nonEssentialWaitHead(sts).WaitFor(finalEssential)
		}
	}

	// make the apps wait on the final essential snap to finish up and their
	// base, if it is being refreshed too
	for _, sts := range apps {
		if finalEssential != nil {
			nonEssentialWaitHead(sts).WaitFor(finalEssential)
		}

		if baseSTS, ok := nonEssentialBases[sts.snapsup.Base]; ok {
			nonEssentialWaitHead(sts).WaitFor(tail(baseSTS.afterLinkSnapAndPostReboot))
		}
	}

	// ensure that everything waits on snapd. we do this pretty late to help
	// prevent superfluous dependencies between tasks.
	if finalSnapdTask != nil {
		// make sure snaps wait on snapd
		for _, sts := range stss {
			if sts.snapsup.InstanceName() == "snapd" {
				continue
			}

			waitForIfNeeded(sts.prerequisites, finalSnapdTask)
		}

		// make sure the seed waits on snapd. this dependency will usually
		// already be set up, but might not be if only snapd is being refreshed.
		if seedTS != nil {
			waitForIfNeeded(seedTS.Create, finalSnapdTask)
		}
	}

	if seedTS == nil {
		return nil, nil
	}

	// seed creation must wait on all initial prerequisites tasks, because those
	// tasks can schedule seed snaps.
	for _, sts := range stss {
		// for seed snaps, seed creation must also wait on the rest of the
		// before-local-modifications phase so it can consume the selected
		// snap/component setup tasks.
		if _, ok := seedSnapTaskSets[sts.snapsup.InstanceName()]; ok {
			waitForIfNeeded(seedTS.Create, tail(sts.beforeLocalSystemModificationsTasks))
			continue
		}

		// non-seed snaps only need to have their initial prerequisites task run
		// before seed creation
		for _, lane := range seedTS.Create.Lanes() {
			if !contains(sts.prerequisites.Lanes(), lane) {
				sts.prerequisites.JoinLane(lane)
			}
		}
		waitForIfNeeded(seedTS.Create, sts.prerequisites)
	}

	// finalize-recovery-system only waits on create-recovery-system by default.
	// since finalize marks the system as seeded with the new snaps, we should
	// wait until all the seed snaps are done.
	for _, sts := range seedSnapTaskSets {
		waitForIfNeeded(seedTS.Finalize, tail(sts.afterLinkSnapAndPostReboot))
	}

	// the removal tasks are already set up to come after the
	// finalize-recovery-system task. put each one in its own lane so a failure
	// in one cleanup neither undoes the refresh nor aborts sibling removals.
	if len(seedTS.Remove) != 0 {
		for _, remove := range seedTS.Remove {
			remove.JoinLane(st.NewLane())
		}
	}

	tasks := append([]*state.Task{seedTS.Create, seedTS.Finalize}, seedTS.Remove...)
	return state.NewTaskSet(tasks...), nil
}

// mergeEssentialAndSeedLanes makes essential snaps and refreshed seed snaps
// share transactional lanes, and adds the given seed-refresh tasks to those
// lanes as well.
func mergeEssentialAndSeedLanes(
	essentials map[snap.Type]snapInstallTaskSet, seedUpdates map[string]snapInstallTaskSet, seedTS *SeedRefreshTaskSet,
) {
	merge := make(map[string]snapInstallTaskSet)
	for _, sts := range seedUpdates {
		merge[sts.snapsup.InstanceName()] = sts
	}

	for _, sts := range essentials {
		if sts.snapsup.Type == snap.TypeSnapd {
			continue
		}
		merge[sts.snapsup.InstanceName()] = sts
	}

	rebootLanes := make(map[string][]int, len(merge))
	all := make([]int, 0, len(merge))
	for _, sts := range merge {
		lanes := unique(sts.upToLinkSnapAndBeforeReboot[len(sts.upToLinkSnapAndBeforeReboot)-1].Lanes())
		rebootLanes[sts.snapsup.InstanceName()] = lanes
		all = unique(append(all, lanes...))
	}

	for _, sts := range merge {
		for _, l := range all {
			if !contains(rebootLanes[sts.snapsup.InstanceName()], l) {
				sts.ts.JoinLane(l)
			}
		}
	}

	if seedTS == nil {
		return
	}

	for _, lane := range all {
		seedTS.Create.JoinLane(lane)
		seedTS.Finalize.JoinLane(lane)
	}
}

// SetEssentialSnapsRestartBoundaries sets up default restart boundaries for a list of task-sets. If the
// list of task-sets contain any updates/installs of essential snaps (base,gadget,kernel), then proper
// restart boundaries will be set up for them.
func SetEssentialSnapsRestartBoundaries(st *state.State, providedDeviceCtx DeviceContext, tss []*state.TaskSet) error {
	bootBase, err := deviceModelBootBase(st, providedDeviceCtx)
	if err != nil {
		return err
	}

	byTypeTss, err := taskSetsByTypeForEssentialSnaps(tss, bootBase)
	if err != nil {
		return err
	}

	// We don't actually need to go through the exact order, but
	// we need to go through this exact list of snap types.
	for _, o := range essentialSnapsRestartOrder {
		if byTypeTss[o] == nil {
			continue
		}
		// Make sure that the core snap is actually the boot-base.
		if o == snap.TypeOS && bootBase != "core" {
			continue
		}
		setDefaultRestartBoundaries(byTypeTss[o])
	}
	return nil
}
