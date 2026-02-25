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

// arrangeInstallTasksForSingleReboot arranges the correct link-order between all the
// provided snap install task-sets, and sets up restart boundaries for essential
// snaps (base, gadget, kernel).
//
// Under normal circumstances link-order that will be configured is:
// snapd => boot-base (reboot) => gadget (reboot) => kernel (reboot) => bases => apps.
//
// However this may be configured into the following if conditions are right for single-reboot:
// snapd => boot-base (up to auto-connect) => gadget(up to auto-connect) =>
// -  kernel (up to auto-connect, then reboot) => boot-base => gadget => kernel => bases => apps.
func arrangeInstallTasksForSingleReboot(st *state.State, stss []snapInstallTaskSet) error {
	for _, sts := range stss {
		if len(sts.beforeLocalSystemModificationsTasks) == 0 ||
			len(sts.upToLinkSnapAndBeforeReboot) == 0 ||
			len(sts.afterLinkSnapAndPostReboot) == 0 {
			return errors.New("internal error: snap install task set has empty slices")
		}
	}

	head := func(tasks []*state.Task) *state.Task {
		return tasks[0]
	}

	tail := func(tasks []*state.Task) *state.Task {
		return tasks[len(tasks)-1]
	}

	bootBase, err := deviceModelBootBase(st, nil)
	if err != nil {
		return err
	}

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
			return head(sts.beforeLocalSystemModificationsTasks), tail(sts.afterLinkSnapAndPostReboot)
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

	// snapd fully goes first, we rely on the existing ordering of the tasks
	// updating snapd
	if sts, ok := essentials[snap.TypeSnapd]; ok {
		prev = tail(sts.afterLinkSnapAndPostReboot)
	}

	// enables us to require that downloads start after we've swapped to the new
	// snapd, if we have one. might be nil!
	finalSnapdTask := prev

	// then all the pre-reboot tasks for essential snaps, in order
	for _, t := range essentialSnapsRestartOrder {
		sts, ok := essentials[t]
		if !ok {
			continue
		}

		// if we refreshed snapd, force all of the downloads to start after it
		// is done. note: we don't add this to the chain of tasks we're building
		// because we don't want to serialize the download tasks.
		if finalSnapdTask != nil {
			head(sts.beforeLocalSystemModificationsTasks).WaitFor(finalSnapdTask)
		}

		chain(beforeReboot(sts))
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

	// ensure essential snaps that are transactional have their lanes merged.
	// this will ensure that essential snaps will be undone together, if one
	// of the updates fails.
	mergeSnapInstallTaskSetLanes := func(stss []snapInstallTaskSet) {
		rebootLanes := make(map[string][]int)
		all := make([]int, 0, len(stss))
		for _, sts := range stss {
			lanes := unique(tail(sts.upToLinkSnapAndBeforeReboot).Lanes())
			rebootLanes[sts.snapsup.InstanceName()] = lanes
			all = unique(append(all, lanes...))
		}

		for _, sts := range stss {
			for _, l := range all {
				if !contains(rebootLanes[sts.snapsup.InstanceName()], l) {
					sts.ts.JoinLane(l)
				}
			}
		}
	}

	// UC16 systems enforce different reboot boundaries, which can result in
	// multiple reboots while refreshing many essential snaps.
	if !isUC16 {
		// set the reboot boundary on the final pre-reboot essential snap task
		for i := len(essentialSnapsRestartOrder) - 1; i >= 0; i-- {
			sts, ok := essentials[essentialSnapsRestartOrder[i]]
			if !ok {
				continue
			}

			_, end := beforeReboot(sts)
			if end == nil {
				return errors.New("internal error: all essential snaps must have before boot tasks")
			}
			restart.MarkTaskAsRestartBoundary(end, restart.RestartBoundaryDirectionDo)

			break
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

		// since the essential snaps are sharing a reboot, they should also
		// share lanes so that they're undone together if one fails
		var merge []snapInstallTaskSet
		for _, sts := range essentials {
			if sts.snapsup.Type == snap.TypeSnapd {
				continue
			}
			merge = append(merge, sts)
		}
		mergeSnapInstallTaskSetLanes(merge)
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

	// make the bases just wait on the final essential snap to finish up
	for _, sts := range nonEssentialBases {
		if finalEssential != nil {
			head(sts.beforeLocalSystemModificationsTasks).WaitFor(finalEssential)
		}
	}

	// make the apps wait on the final essential snap to finish up and their
	// base, if it is being refreshed too
	for _, sts := range apps {
		if finalEssential != nil {
			head(sts.beforeLocalSystemModificationsTasks).WaitFor(finalEssential)
		}

		if baseSTS, ok := nonEssentialBases[sts.snapsup.Base]; ok {
			head(sts.beforeLocalSystemModificationsTasks).WaitFor(tail(baseSTS.afterLinkSnapAndPostReboot))
		}
	}

	return nil
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
