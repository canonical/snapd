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

// Observed link order between essential snaps in doUpdate (snapstate.go)
// base waits for snapd
// gadget waits for base/os + snapd
// kernel waits for base/os + snapd + gadget if present
var essentialSnapsRestartOrder = []snap.Type{
	snap.TypeOS,
	// The base will only require restart if it's the base of the model.
	snap.TypeBase,
	snap.TypeGadget,
	// Kernel must wait for gadget because the gadget may define
	// new "$kernel:refs".
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
		case snap.TypeOS, snap.TypeGadget, snap.TypeKernel:
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

func bootBaseSnapType(byTypeTss map[snap.Type]*state.TaskSet) snap.Type {
	if ts := byTypeTss[snap.TypeBase]; ts != nil {
		return snap.TypeBase
	}
	// On UC16 it's the TypeOS
	return snap.TypeOS
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

	bootSnapType := bootBaseSnapType(byTypeTss)

	// XXX: Currently we don't need to go through the correct order, but we do it
	// just in preparation of when single-reboot functionality is added.
	for _, o := range essentialSnapsRestartOrder {
		if byTypeTss[o] == nil {
			continue
		}
		// Make sure that the core snap is actually the boot-base
		if o == snap.TypeOS && bootSnapType != snap.TypeOS {
			continue
		}
		setDefaultRestartBoundaries(byTypeTss[o])
	}
	return nil
}
