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
	"github.com/snapcore/snapd/strutil"
)

var (
	snapstateAll                     = snapstate.All
	snapstateCheckChangeConflictMany = snapstate.CheckChangeConflictMany
	backendIter                      = backend.Iter
)

type snapshotState struct {
	LastSetID uint64 `json:"last-set-id"`
}

func newSnapshotSetID(st *state.State) (uint64, error) {
	var shotState snapshotState

	err := st.Get("snapshots", &shotState)
	if err == state.ErrNoState {
		shotState = snapshotState{}
	} else if err != nil {
		return 0, err
	}

	shotState.LastSetID++
	st.Set("snapshots", shotState)

	return shotState.LastSetID, nil
}

func allActiveSnapNames(st *state.State) ([]string, error) {
	all, err := snapstateAll(st)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(all))
	for name, snapst := range all {
		if snapst.Active {
			names = append(names, name)
		}
	}

	sort.Strings(names)

	return names, nil
}

func snapNamesInSnapshotSet(setID uint64, requested []string) (snapsFound, filenames []string, err error) {
	sort.Strings(requested)
	found := false
	err = backendIter(context.TODO(), func(r *backend.Reader) error {
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

func taskGetErrMsg(task *state.Task, err error, what string) error {
	if err == state.ErrNoState {
		return fmt.Errorf("internal error: task %s (%s) is missing %s information", task.ID(), task.Kind(), what)
	}
	return fmt.Errorf("internal error: retrieving %s information from task %s (%s): %v", what, task.ID(), task.Kind(), err)
}

// checkSnapshotTaskConflict checks whether there's an in-progress task for snapshots with the given set id.
func checkSnapshotTaskConflict(st *state.State, setID uint64, conflictingKinds ...string) error {
	for _, task := range st.Tasks() {
		if task.Change().Status().Ready() {
			continue
		}
		if !strutil.ListContains(conflictingKinds, task.Kind()) {
			continue
		}

		var snapshot snapshotSetup
		if err := task.Get("snapshot-setup", &snapshot); err != nil {
			return taskGetErrMsg(task, err, "snapshot")
		}

		if snapshot.SetID == setID {
			return fmt.Errorf("cannot operate on snapshot set #%d while change %q is in progress", setID, task.Change().ID())
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

	// Make sure we do not snapshot if anything like install/remove/refresh is in progress
	if err := snapstateCheckChangeConflictMany(st, snapNames, ""); err != nil {
		return 0, nil, nil, err
	}

	setID, err = newSnapshotSetID(st)
	if err != nil {
		return 0, nil, nil, err
	}

	ts = state.NewTaskSet()

	for _, name := range snapNames {
		desc := fmt.Sprintf("Save data of snap %q in snapshot set #%d", name, setID)
		task := st.NewTask("save-snapshot", desc)
		snapshot := snapshotSetup{
			SetID: setID,
			Snap:  name,
			Users: users,
		}
		task.Set("snapshot-setup", &snapshot)
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

	if err := snapstateCheckChangeConflictMany(st, snapsFound, ""); err != nil {
		return nil, nil, err
	}

	// restore needs to conflict with forget of itself
	if err := checkSnapshotTaskConflict(st, setID, "forget-snapshot"); err != nil {
		return nil, nil, err
	}

	ts = state.NewTaskSet()

	for i, name := range snapsFound {
		desc := fmt.Sprintf("Restore data of snap %q from snapshot set #%d", name, setID)
		task := st.NewTask("restore-snapshot", desc)
		snapshot := snapshotSetup{
			SetID:    setID,
			Snap:     name,
			Users:    users,
			Filename: filenames[i],
		}
		task.Set("snapshot-setup", &snapshot)
		ts.AddTask(task)
	}

	return snapsFound, ts, nil
}

// Check creates a taskset for checking a snapshot's data.
// Note that the state must be locked by the caller.
func Check(st *state.State, setID uint64, snapNames []string, users []string) (snapsFound []string, ts *state.TaskSet, err error) {
	// check needs to conflict with forget of itself
	if err := checkSnapshotTaskConflict(st, setID, "forget-snapshot"); err != nil {
		return nil, nil, err
	}

	snapsFound, filenames, err := snapNamesInSnapshotSet(setID, snapNames)
	if err != nil {
		return nil, nil, err
	}

	ts = state.NewTaskSet()

	for i, name := range snapsFound {
		desc := fmt.Sprintf("Check data of snap %q in snapshot set #%d", name, setID)
		task := st.NewTask("check-snapshot", desc)
		snapshot := snapshotSetup{
			SetID:    setID,
			Snap:     name,
			Users:    users,
			Filename: filenames[i],
		}
		task.Set("snapshot-setup", &snapshot)
		ts.AddTask(task)
	}

	return snapsFound, ts, nil
}

// Forget creates a taskset for deletinig a snapshot.
// Note that the state must be locked by the caller.
func Forget(st *state.State, setID uint64, snapNames []string) (snapsFound []string, ts *state.TaskSet, err error) {
	// forget needs to conflict with check and restore
	if err := checkSnapshotTaskConflict(st, setID, "check-snapshot", "restore-snapshot"); err != nil {
		return nil, nil, err
	}

	snapsFound, filenames, err := snapNamesInSnapshotSet(setID, snapNames)
	if err != nil {
		return nil, nil, err
	}

	ts = state.NewTaskSet()
	for i, name := range snapsFound {
		desc := fmt.Sprintf("Drop data of snap %q from snapshot set #%d", name, setID)
		task := st.NewTask("forget-snapshot", desc)
		snapshot := snapshotSetup{
			SetID:    setID,
			Snap:     name,
			Filename: filenames[i],
		}
		task.Set("snapshot-setup", &snapshot)
		ts.AddTask(task)
	}

	return snapsFound, ts, nil
}
