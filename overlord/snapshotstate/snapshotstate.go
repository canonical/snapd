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
	"path/filepath"
	"sort"

	"golang.org/x/net/context"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/snapshotstate/backend"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/strutil"
)

type snapshotState struct {
	LastID uint64 `json:"last-id"`
}

func newSnapshotID(st *state.State) (uint64, error) {
	var shotState snapshotState

	err := st.Get("snapshot", &shotState)
	if err != nil {
		if err == state.ErrNoState {
			shotState = snapshotState{}
		} else {
			return 0, err
		}
	}

	shotState.LastID++
	st.Set("snapshot", shotState)

	return shotState.LastID, nil
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

func allHomes() ([]string, error) {
	snapHomes, err := filepath.Glob(filepath.Join(dirs.GlobalRootDir, "home/*/snap"))
	if err != nil {
		// can't happen?
		return nil, err
	}
	snapHomes = append(snapHomes, filepath.Join(dirs.GlobalRootDir, "root/snap"))
	homes := make([]string, 0, len(snapHomes))
	for _, home := range snapHomes {
		home, err = filepath.EvalSymlinks(home)
		if err != nil {
			continue
		}
		homes = append(homes, filepath.Dir(home))
	}

	return homes, nil
}

func snapNamesInSnapshot(snapshotID uint64, requested []string) (snapsFound []string, filenames []string, err error) {
	sort.Strings(requested)
	found := false
	err = backend.Iter(context.Background(), func(r *backend.Reader) error {
		if r.ID == snapshotID {
			found = true
			if len(requested) == 0 || strutil.SortedListContains(requested, r.Snap) {
				snapsFound = append(snapsFound, r.Snap)
				filenames = append(filenames, r.Filename())
			}
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	if !found {
		return nil, nil, client.ErrSnapshotNotFound
	}
	if len(snapsFound) == 0 {
		return nil, nil, client.ErrSnapshotSnapsNotFound
	}

	return snapsFound, filenames, nil
}

// some snapshot ops only conflict with other snapshot ops (in
// particular check, lose and restore)
func checkSnapshotChangeConflict(st *state.State, snapshotID uint64, conflictingKinds ...string) error {
	for _, task := range st.Tasks() {
		chg := task.Change()
		if chg.Status().Ready() {
			continue
		}
		if !strutil.ListContains(conflictingKinds, chg.Kind()) {
			continue
		}

		var shot shotState
		if err := task.Get("shot", &shot); err != nil {
			return err
		}

		if shot.ID == snapshotID {
			return fmt.Errorf("snapshot #%d has a %q change in progress", snapshotID, chg.Kind())
		}
	}

	return nil
}

// List valid snapshots
//
// The snapshots are closed before returning.
var List = backend.List

// Save creates a taskset for taking snapshots of snaps' data.
// Note that the state must be locked by the caller.
func Save(st *state.State, snapNames []string, homes []string) (uint64, []string, *state.TaskSet, error) {
	var err error

	if len(homes) == 0 {
		homes, err = allHomes()
		if err != nil {
			return 0, nil, nil, err
		}
	}

	if len(snapNames) == 0 {
		snapNames, err = allActiveSnapNames(st)
		if err != nil {
			return 0, nil, nil, err
		}
	}

	if err := snapstate.CheckChangeConflictMany(st, snapNames, nil); err != nil {
		return 0, nil, nil, err
	}

	shID, err := newSnapshotID(st)
	if err != nil {
		return 0, nil, nil, err
	}

	ts := state.NewTaskSet()

	for _, name := range snapNames {
		desc := fmt.Sprintf("Saving the data of snap %q in snapshot #%d", name, shID)
		task := st.NewTask("save-snapshot", desc)
		shot := shotState{
			ID:    shID,
			Snap:  name,
			Homes: homes,
		}
		task.Set("shot", &shot)
		task.Set("snap-setup", &snapstate.SnapSetup{SideInfo: &snap.SideInfo{RealName: name}})
		ts.AddTask(task)
	}

	return shID, snapNames, ts, nil
}

// Restore creates a taskset for restoring a snapshot's data.
// Note that the state must be locked by the caller.
func Restore(st *state.State, snapshotID uint64, snapNames []string, homes []string) ([]string, *state.TaskSet, error) {
	snapNames, filenames, err := snapNamesInSnapshot(snapshotID, snapNames)
	if err != nil {
		return nil, nil, err
	}

	if err := snapstate.CheckChangeConflictMany(st, snapNames, nil); err != nil {
		return nil, nil, err
	}

	// restore needs to conflict with lose of itself
	if err := checkSnapshotChangeConflict(st, snapshotID, "lose-snapshot"); err != nil {
		return nil, nil, err
	}

	ts := state.NewTaskSet()

	for i, name := range snapNames {
		desc := fmt.Sprintf("Restoring data from snapshot #%d for snap %q", snapshotID, name)
		task := st.NewTask("restore-snapshot", desc)
		shot := shotState{
			ID:       snapshotID,
			Snap:     name,
			Homes:    homes,
			Filename: filenames[i],
		}
		task.Set("shot", &shot)
		// hackish, for conflict detection:
		task.Set("snap-setup", &snapstate.SnapSetup{SideInfo: &snap.SideInfo{RealName: name}})
		ts.AddTask(task)
	}

	return snapNames, ts, nil
}

// Check creates a taskset for checking a snapshot's data.
// Note that the state must be locked by the caller.
func Check(st *state.State, snapshotID uint64, snapNames []string, homes []string) ([]string, *state.TaskSet, error) {
	// check needs to conflict with lose of itself
	if err := checkSnapshotChangeConflict(st, snapshotID, "lose-snapshot"); err != nil {
		return nil, nil, err
	}

	snapNames, filenames, err := snapNamesInSnapshot(snapshotID, snapNames)
	if err != nil {
		return nil, nil, err
	}

	ts := state.NewTaskSet()

	for i, name := range snapNames {
		desc := fmt.Sprintf("Checking data in snapshot #%d for snap %q", snapshotID, name)
		task := st.NewTask("check-snapshot", desc)
		shot := shotState{
			ID:       snapshotID,
			Snap:     name,
			Homes:    homes,
			Filename: filenames[i],
		}
		task.Set("shot", &shot)
		ts.AddTask(task)
	}

	return snapNames, ts, nil
}

// Lose creates a taskset for deletinig a snapshot.
// Note that the state must be locked by the caller.
func Lose(st *state.State, snapshotID uint64, snapNames []string) ([]string, *state.TaskSet, error) {
	// lose needs to conflict with check and restore
	if err := checkSnapshotChangeConflict(st, snapshotID, "check-snapshot", "restore-snapshot"); err != nil {
		return nil, nil, err
	}

	snapNames, filenames, err := snapNamesInSnapshot(snapshotID, snapNames)
	if err != nil {
		return nil, nil, err
	}

	ts := state.NewTaskSet()

	for i, name := range snapNames {
		desc := fmt.Sprintf("Removing snapshot #%d for snap %q", snapshotID, name)
		task := st.NewTask("lose-snapshot", desc)
		shot := shotState{
			ID:       snapshotID,
			Snap:     name,
			Filename: filenames[i],
		}
		task.Set("shot", &shot)
		ts.AddTask(task)
	}

	return snapNames, ts, nil
}
