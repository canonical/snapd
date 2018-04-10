// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package snapshotstate

import (
	"fmt"
	"sort"

	"golang.org/x/net/context"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/overlord/snapshotstate/backend"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/strutil"
)

type snapshotSetState struct {
	LastID uint64 `json:"last-id"`
}

func newSnapshotSetID(st *state.State) (uint64, error) {
	var setState snapshotSetState

	err := st.Get("snapshot-set", &setState)
	if err == state.ErrNoState {
		setState = snapshotSetState{}
	} else if err != nil {
		return 0, err
	}

	setState.LastID++
	st.Set("snapshot-set", setState)

	return setState.LastID, nil
}

func allActiveSnapNames(st *state.State) ([]string, error) {
	all, err := snapstate.All(st)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(all))
	for name, snapst := range all {
		if snapst.IsInstalled() {
			names = append(names, name)
		}
	}

	return names, nil
}

func snapNamesInSnapshotSet(setID uint64, requested []string) (snapsFound []string, filenames []string, err error) {
	sort.Strings(requested)
	found := false
	err = backend.Iter(context.Background(), func(r *backend.Reader) error {
		if r.SetID == setID {
			found = true
			if len(requested) == 0 || strutil.SortedListContains(requested, r.Snap) {
				snapsFound = append(snapsFound, r.Snap)
				filenames = append(filenames, r.Name())
			}
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	if !found {
		return nil, nil, client.ErrSnapshotSetNotFound
	}
	if len(snapsFound) == 0 {
		return nil, nil, client.ErrSnapshotSnapsNotFound
	}

	return snapsFound, filenames, nil
}

// checkSnapshotChangeConflict checks whether there's an in-progress change for snapshots with the given set id.
func checkSnapshotChangeConflict(st *state.State, setID uint64, conflictingKinds ...string) error {
	for _, task := range st.Tasks() {
		chg := task.Change()
		if chg.Status().Ready() {
			continue
		}
		if !strutil.ListContains(conflictingKinds, chg.Kind()) {
			continue
		}

		var snapshot snapshotState
		if err := task.Get("snapshot", &snapshot); err != nil {
			return err
		}

		if snapshot.SetID == setID {
			return fmt.Errorf("snapshot set #%d has a %q change in progress", setID, chg.Kind())
		}
	}

	return nil
}

// List valid snapshots.
// Note that the state must be locked by the caller.
var List = backend.List

// Save creates a taskset for taking snapshots of snaps' data.
// Note that the state must be locked by the caller.
func Save(st *state.State, snapNames []string, users []string) (setID uint64, snapsSaved []string, ts *state.TaskSet, err error) {
	if len(snapNames) == 0 {
		snapNames, err = allActiveSnapNames(st)
		if err != nil {
			return 0, nil, nil, err
		}
	}

	if err := snapstate.CheckChangeConflictMany(st, snapNames, nil); err != nil {
		return 0, nil, nil, err
	}

	setID, err = newSnapshotSetID(st)
	if err != nil {
		return 0, nil, nil, err
	}

	ts = state.NewTaskSet()

	for _, name := range snapNames {
		desc := fmt.Sprintf("Save the data of snap %q in snapshot set #%d", name, setID)
		task := st.NewTask("save-snapshot", desc)
		snapshot := snapshotState{
			SetID: setID,
			Snap:  name,
			Users: users,
		}
		task.Set("snapshot", &snapshot)
		task.Set("snap-setup", &snapstate.SnapSetup{SideInfo: &snap.SideInfo{RealName: name}})
		ts.AddTask(task)
	}

	return setID, snapNames, ts, nil
}

// Restore creates a taskset for restoring a snapshot's data.
// Note that the state must be locked by the caller.
func Restore(st *state.State, setID uint64, snapNames []string, users []string) (snapsFound []string, ts *state.TaskSet, err error) {
	snapsFound, filenames, err := snapNamesInSnapshotSet(setID, snapNames)
	if err != nil {
		return nil, nil, err
	}

	if err := snapstate.CheckChangeConflictMany(st, snapsFound, nil); err != nil {
		return nil, nil, err
	}

	// restore needs to conflict with forget of itself
	if err := checkSnapshotChangeConflict(st, setID, "forget-snapshot"); err != nil {
		return nil, nil, err
	}

	ts = state.NewTaskSet()

	for i, name := range snapsFound {
		desc := fmt.Sprintf("Restore the data of snap %q from snapshot set #%d", name, setID)
		task := st.NewTask("restore-snapshot", desc)
		snapshot := snapshotState{
			SetID:    setID,
			Snap:     name,
			Users:    users,
			Filename: filenames[i],
		}
		task.Set("snapshot", &snapshot)
		// hackish, for conflict detection:
		task.Set("snap-setup", &snapstate.SnapSetup{SideInfo: &snap.SideInfo{RealName: name}})
		ts.AddTask(task)
	}

	return snapsFound, ts, nil
}

// Check creates a taskset for checking a snapshot's data.
// Note that the state must be locked by the caller.
func Check(st *state.State, setID uint64, snapNames []string, users []string) (snapsFound []string, ts *state.TaskSet, err error) {
	// check needs to conflict with forget of itself
	if err := checkSnapshotChangeConflict(st, setID, "forget-snapshot"); err != nil {
		return nil, nil, err
	}

	snapsFound, filenames, err := snapNamesInSnapshotSet(setID, snapNames)
	if err != nil {
		return nil, nil, err
	}

	ts = state.NewTaskSet()

	for i, name := range snapsFound {
		desc := fmt.Sprintf("Check the data of snap %q in snapshot set #%d", name, setID)
		task := st.NewTask("check-snapshot", desc)
		snapshot := snapshotState{
			SetID:    setID,
			Snap:     name,
			Users:    users,
			Filename: filenames[i],
		}
		task.Set("snapshot", &snapshot)
		ts.AddTask(task)
	}

	return snapsFound, ts, nil
}

// Forget creates a taskset for deletinig a snapshot.
// Note that the state must be locked by the caller.
func Forget(st *state.State, setID uint64, snapNames []string) (snapsFound []string, ts *state.TaskSet, err error) {
	// forget needs to conflict with check and restore
	if err := checkSnapshotChangeConflict(st, setID, "check-snapshot", "restore-snapshot"); err != nil {
		return nil, nil, err
	}

	snapsFound, filenames, err := snapNamesInSnapshotSet(setID, snapNames)
	if err != nil {
		return nil, nil, err
	}

	ts = state.NewTaskSet()
	for i, name := range snapsFound {
		desc := fmt.Sprintf("Remove the data of snap %q from snapshot set #%d", name, setID)
		task := st.NewTask("forget-snapshot", desc)
		snapshot := snapshotState{
			SetID:    setID,
			Snap:     name,
			Filename: filenames[i],
		}
		task.Set("snapshot", &snapshot)
		ts.AddTask(task)
	}

	return snapsFound, ts, nil
}
