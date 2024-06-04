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
	"fmt"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/restart"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

// only useful for procuring a buggy behavior in the tests
var enforcedSingleRebootForGadgetKernelBase = false

func MockEnforceSingleRebootForBaseKernelGadget(val bool) (restore func()) {
	osutil.MustBeTestBinary("mocking can be done only in tests")

	old := enforcedSingleRebootForGadgetKernelBase
	enforcedSingleRebootForGadgetKernelBase = val
	return func() {
		enforcedSingleRebootForGadgetKernelBase = old
	}
}

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

// taskSetsByTypeForEssentialSnaps returns a map of task-sets by their essential snap type, if
// a task-set for any of the essential snap exists.
func taskSetsByTypeForEssentialSnaps(tss []*state.TaskSet, bootBase string) (map[snap.Type]*state.TaskSet, error) {
	avail := make(map[snap.Type]*state.TaskSet)
	for _, ts := range tss {
		snapsup := maybeTaskSetSnapSetup(ts)
		if snapsup == nil {
			continue
		}

		switch snapsup.Type {
		case snap.TypeBase:
			if snapsup.SnapName() == bootBase {
				avail[snapsup.Type] = ts
			}
		case snap.TypeSnapd, snap.TypeOS, snap.TypeGadget, snap.TypeKernel:
			avail[snapsup.Type] = ts
		}
	}
	return avail, nil
}

func nonEssentialSnapTaskSets(tss []*state.TaskSet, bootBase string) (bases, apps map[string]*state.TaskSet) {
	bases = make(map[string]*state.TaskSet)
	apps = make(map[string]*state.TaskSet)
	for _, ts := range tss {
		snapsup := maybeTaskSetSnapSetup(ts)
		if snapsup == nil {
			continue
		}

		switch snapsup.Type {
		case snap.TypeBase:
			if snapsup.SnapName() != bootBase {
				bases[snapsup.SnapName()] = ts
			}
		case snap.TypeApp:
			apps[snapsup.SnapName()] = ts
		}
	}
	return bases, apps
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

func tasksBefore(task *state.Task) *state.TaskSet {
	ts := state.NewTaskSet()
	for _, t := range task.WaitTasks() {
		ts.AddTask(t)
		ts.AddAll(tasksBefore(t))
	}
	return ts
}

func tasksAfter(task *state.Task) *state.TaskSet {
	ts := state.NewTaskSet()
	for _, t := range task.HaltTasks() {
		ts.AddTask(t)
		ts.AddAll(tasksAfter(t))
	}
	return ts
}

// splitTaskSetByRebootEdges makes use of the following edges on a task-set:
// - BeginEdge (marks the first task of a task-set)
// - EndEdge (marks the last task of a task-set)
// - MaybeRebootEdge (marks the last task that needs to run before the reboot)
// - MaybeRebootWaitEdge (marks the first task that needs to run after the reboot)
// and from these edges, constructs two new task-sets, one before reboot, and one
// after reboot.
func splitTaskSetByRebootEdges(ts *state.TaskSet) (before, after *state.TaskSet, err error) {
	// TaskSets must have edges set, so we can detect the first and last tasks
	// of a task-set.
	firstTask := ts.MaybeEdge(BeginEdge)
	lastTask := ts.MaybeEdge(EndEdge)
	if firstTask == nil || lastTask == nil {
		return nil, nil, fmt.Errorf("internal error: task-set is missing required edges (%q/%q)", BeginEdge, EndEdge)
	}

	// TaskSets must also have the reboot edges set.
	linkSnap := ts.MaybeEdge(MaybeRebootEdge)
	if linkSnap == nil {
		return nil, nil, fmt.Errorf("internal error: task-set is missing required edge %q", MaybeRebootEdge)
	}
	autoConnect := ts.MaybeEdge(MaybeRebootWaitEdge)
	if autoConnect == nil {
		return nil, nil, fmt.Errorf("internal error: task-set is missing required edge %q", MaybeRebootWaitEdge)
	}

	// Create the before task-set, which is the one before reboot, and
	// setup new edges for the task-set. In this task-set, we expect
	// the original start task (i.e prerequisites) to be first, and link-snap
	// to be the last
	before = state.NewTaskSet(linkSnap)
	before.AddAll(tasksBefore(linkSnap))
	before.MarkEdge(firstTask, BeginEdge)
	before.MarkEdge(linkSnap, EndEdge)

	// Create the after task-set, which is the post-reboot tasks, and setup
	// new begin/end edges. In this one, we expect auto-connect to be first, and
	// whatever was last before, is last again.
	after = state.NewTaskSet(autoConnect)
	after.AddAll(tasksAfter(autoConnect))
	after.MarkEdge(autoConnect, BeginEdge)
	after.MarkEdge(lastTask, EndEdge)
	return before, after, nil
}

func bootBaseSnapType(byTypeTss map[snap.Type]*state.TaskSet) snap.Type {
	if ts := byTypeTss[snap.TypeBase]; ts != nil {
		return snap.TypeBase
	}
	// On UC16 it's the TypeOS
	return snap.TypeOS
}

// arrangeSingleRebootForSplitTaskSets sets up restart boundaries for task-sets that have been
// split into two. It uses the first part of the task-set (i.e the one containing the reboot edges)
// to set a restart boundary along the 'Do' path for the last task-set, and one along the 'Undo' path
// for the first task-set, to introduce only one reboot along each direction.
func arrangeSingleRebootForSplitTaskSets(beforeTss map[snap.Type]*state.TaskSet) error {
	// Set reboot boundary along the do path for the last essential
	for i := len(essentialSnapsRestartOrder) - 1; i >= 0; i-- {
		o := essentialSnapsRestartOrder[i]
		if beforeTss[o] == nil {
			continue
		}

		linkSnap := beforeTss[o].MaybeEdge(EndEdge)
		if linkSnap == nil {
			return fmt.Errorf("internal error: no %q edge set in task-set for %q", EndEdge, o)
		}
		restart.MarkTaskAsRestartBoundary(linkSnap, restart.RestartBoundaryDirectionDo)
		break
	}

	// Set reboot boundary along the undo path for the first essential
	for _, o := range essentialSnapsRestartOrder {
		if beforeTss[o] == nil {
			continue
		}
		if unlinkSnap := findUnlinkTask(beforeTss[o]); unlinkSnap != nil {
			restart.MarkTaskAsRestartBoundary(unlinkSnap, restart.RestartBoundaryDirectionUndo)
			break
		}
	}
	return nil
}

// waitForLastTask makes the first task of 'ts' wait for the last task of the 'dep' task-set.
func waitForLastTask(ts, dep *state.TaskSet) error {
	last, err := dep.Edge(EndEdge)
	if err != nil {
		return err
	}
	first, err := ts.Edge(BeginEdge)
	if err != nil {
		return err
	}
	first.WaitFor(last)
	return nil
}

// arrangeSnapToWaitForBaseIfPresent sets up dependency on the base of a snap, if the base is
// also being updated. The boot-base is ignored here, as the boot-base is handled separately
// as a part of the essential snaps.
func arrangeSnapToWaitForBaseIfPresent(snapTs *state.TaskSet, bases map[string]*state.TaskSet) error {
	snapsup := maybeTaskSetSnapSetup(snapTs)
	if snapsup == nil {
		return fmt.Errorf("internal error: failed to get the SnapSetup instance from snap task-set")
	}

	if baseTs := bases[snapsup.Base]; baseTs != nil {
		return waitForLastTask(snapTs, baseTs)
	}
	return nil
}

func taskSetLanesByRebootEdge(ts *state.TaskSet) ([]int, error) {
	linkSnap := ts.MaybeEdge(MaybeRebootEdge)
	if linkSnap == nil {
		return nil, fmt.Errorf("internal error: no %q edge set in task-set", MaybeRebootEdge)
	}
	return linkSnap.Lanes(), nil
}

func mergeTaskSetLanes(lanesByTs map[*state.TaskSet][]int) {
	var allLanes []int
	for _, lanes := range lanesByTs {
		allLanes = append(allLanes, lanes...)
	}
	for ts, tsLanes := range lanesByTs {
		for _, l := range allLanes {
			if !listContains(tsLanes, l) {
				ts.JoinLane(l)
			}
		}
	}
}

func listContains(items []int, item int) bool {
	for _, i := range items {
		if i == item {
			return true
		}
	}
	return false
}

// arrangeSnapTaskSetsLinkageAndRestart arranges the correct link-order between all
// the provided snap task-sets, and sets up restart boundaries for essential snaps (base, gadget, kernel).
// Under normal circumstances link-order that will be configured is:
// snapd => boot-base (reboot) => gadget (reboot) => kernel (reboot) => bases => apps.
//
// However this may be configured into the following if conditions are right for single-reboot
// snapd => boot-base (up to auto-connect) => gadget(up to auto-connect) =>
// -  kernel (up to auto-connect, then reboot) => boot-base => gadget => kernel => bases => apps.
func arrangeSnapTaskSetsLinkageAndRestart(st *state.State, providedDeviceCtx DeviceContext, tss []*state.TaskSet) error {
	bootBase, err := deviceModelBootBase(st, providedDeviceCtx)
	if err != nil {
		return err
	}

	byTypeTss, err := taskSetsByTypeForEssentialSnaps(tss, bootBase)
	if err != nil {
		return err
	}

	// If the boot-base is 'core', then we don't allow splitting the task-sets to set up
	// for single-reboot, as we don't support this behavior on UC16.
	isUC16 := bootBase == "core"
	var lastEssentialTs *state.TaskSet
	lanesByTsToMerge := make(map[*state.TaskSet][]int)
	beforeTss := make(map[snap.Type]*state.TaskSet)
	afterTss := make(map[snap.Type]*state.TaskSet)
	// chainEssentialTs takes a task-set that needs to be 'chained' unto the previous (unless it's the first),
	// a snap type to specify which type of snap is being chained, and two operational flags.
	// <transactional>: If set, means that the task-set should be part of the essential snap transaction. Lanes
	// from the task-set will be merged. This behaviour is disabled for UC16 to not introduce any new changes.
	// <split>: If set, the task-set will be split up into two parts. One pre-reboot part, and one post-reboot part.
	// All chained task-sets that have <split> set will have their pre-reboot part run before the reboot, and then all
	// post-reboot parts run after the reboot. They will run in the order they are chained.
	// ts1-pre-reboot => ts2-pre-reboot => [reboot] => ts1-post-reboot => ts2-post-reboot
	chainEssentialTs := func(ts *state.TaskSet, snapType snap.Type, transactional, split bool) error {
		if transactional && !isUC16 {
			lanes, err := taskSetLanesByRebootEdge(ts)
			if err != nil {
				return err
			}
			lanesByTsToMerge[ts] = lanes
		}

		nextTs := ts
		if split && !isUC16 {
			before, after, err := splitTaskSetByRebootEdges(ts)
			if err != nil {
				return err
			}
			beforeTss[snapType] = before
			afterTss[snapType] = after
			nextTs = before
		}
		if lastEssentialTs != nil {
			if err := waitForLastTask(nextTs, lastEssentialTs); err != nil {
				return err
			}
		}
		lastEssentialTs = nextTs
		return nil
	}

	// Snapd always run first if present.
	if ts := byTypeTss[snap.TypeSnapd]; ts != nil {
		// Snapd does not need to be part of the transaction, as
		// it's not really necessary to undo snapd should base/kernel/gadget
		// fail.
		const transactional = false
		const split = false
		if err := chainEssentialTs(ts, snap.TypeSnapd, transactional, split); err != nil {
			return err
		}
	}

	bootSnapType := bootBaseSnapType(byTypeTss)

	// Then we link in the boot-base, to run after snapd, it could run in its
	// entirety before a reboot, as we expect boot-bases to be 'simple' and not
	// have any hooks.
	if ts := byTypeTss[bootSnapType]; ts != nil {
		const transactional = true
		const split = true
		if err := chainEssentialTs(ts, bootSnapType, transactional, split); err != nil {
			return err
		}
	}

	// Next we link in the gadget, and it needs to be part of the transaction
	// so it will be undone in the event of failures.
	if ts := byTypeTss[snap.TypeGadget]; ts != nil {
		const transactional = true
		split := !enforcedSingleRebootForGadgetKernelBase // keep this to be able to induce a buggy change
		if err := chainEssentialTs(ts, snap.TypeGadget, transactional, split); err != nil {
			return err
		}
	}

	// Then we link in the kernel, it needs to run latest, but before other bases and apps.
	if ts := byTypeTss[snap.TypeKernel]; ts != nil {
		const transactional = true
		const split = true
		if err := chainEssentialTs(ts, snap.TypeKernel, transactional, split); err != nil {
			return err
		}
	}

	// Now link in all the after task-sets that have been split, which should run
	// post-reboot.
	for _, o := range essentialSnapsRestartOrder {
		if afterTss[o] == nil {
			continue
		}
		const transactional = false
		const split = false
		if err := chainEssentialTs(afterTss[o], o, transactional, split); err != nil {
			return err
		}
	}

	// Ensure restart boundaries are set, for the task-sets that have been
	// split, we ensure that boundaries are set *only* for the last of those, to allow
	// them all to run before the reboot.
	if len(beforeTss) > 0 {
		if err := arrangeSingleRebootForSplitTaskSets(beforeTss); err != nil {
			return err
		}
	}

	// Ensure essential snaps that are transactional have their lanes merged. This will effectively
	// ensure that essential snaps will be undone together should one of the updates fail. So if we
	// are updating both gadget and kernel, and kernel fails, the gadget will also be undone.
	mergeTaskSetLanes(lanesByTsToMerge)

	// For the task-sets that have not been split, they must have boundaries set for
	// each of them. Reuse the restart order list here, so we go through the correct
	// list of snap types that needs restart boundaries set.
	for _, o := range essentialSnapsRestartOrder {
		// Make sure that the core snap is actually the boot-base
		if o == snap.TypeOS && bootBase != "core" {
			continue
		}
		// If the snap type was not split (i.e no task-set in "beforeTss"), and
		// the snap is actually present in this update, then set default restart
		// boundaries.
		if beforeTss[o] == nil && byTypeTss[o] != nil {
			setDefaultRestartBoundaries(byTypeTss[o])
		}
	}

	// Now we ensure that all other non-essential bases depend on the last
	// essential task-set.
	bases, apps := nonEssentialSnapTaskSets(tss, bootBase)
	if lastEssentialTs != nil {
		for _, ts := range bases {
			if err := waitForLastTask(ts, lastEssentialTs); err != nil {
				return err
			}
		}
	}

	// And last, we ensure apps wait for any non-essential base it needs, and the last task-set
	// of any essential-snap that is also being updated.
	for _, appTs := range apps {
		if lastEssentialTs != nil {
			if err := waitForLastTask(appTs, lastEssentialTs); err != nil {
				return err
			}
		}
		if err := arrangeSnapToWaitForBaseIfPresent(appTs, bases); err != nil {
			return err
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
