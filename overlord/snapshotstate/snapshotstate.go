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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/snapshotstate/backend"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/strutil"
)

var (
	snapstateAll                     = snapstate.All
	snapstateCheckChangeConflictMany = snapstate.CheckChangeConflictMany
	backendIter                      = backend.Iter
	backendEstimateSnapshotSize      = backend.EstimateSnapshotSize
	backendList                      = backend.List
	backendNewSnapshotExport         = backend.NewSnapshotExport

	// Default expiration time for automatic snapshots, if not set by the user
	defaultAutomaticSnapshotExpiration = time.Hour * 24 * 31
)

type snapshotState struct {
	ExpiryTime time.Time `json:"expiry-time"`
}

func newSnapshotSetID(st *state.State) (uint64, error) {
	var lastDiskSetID, lastStateSetID uint64

	// get last set id from state
	err := st.Get("last-snapshot-set-id", &lastStateSetID)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return 0, err
	}

	// get highest set id from the snapshots/ directory
	lastDiskSetID, err = backend.LastSnapshotSetID()
	if err != nil {
		return 0, fmt.Errorf("cannot determine last snapshot set id: %v", err)
	}

	// take the larger of the two numbers and store it back in the state.
	// the value in state acts as an allocation of IDs for scheduled snapshots,
	// they allocate set id early before any file gets created, so we cannot
	// rely on disk only.
	lastSetID := lastDiskSetID
	if lastStateSetID > lastSetID {
		lastSetID = lastStateSetID
	}
	lastSetID++
	st.Set("last-snapshot-set-id", lastSetID)

	return lastSetID, nil
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

func EstimateSnapshotSize(st *state.State, instanceName string, users []string) (uint64, error) {
	cur, err := snapstateCurrentInfo(st, instanceName)
	if err != nil {
		return 0, err
	}
	rawCfg, err := configGetSnapConfig(st, instanceName)
	if err != nil {
		return 0, err
	}

	opts, err := getSnapDirOpts(st, cur.InstanceName())
	if err != nil {
		return 0, err
	}

	sz, err := backendEstimateSnapshotSize(cur, users, opts)
	if err != nil {
		return 0, err
	}
	if rawCfg != nil {
		sz += uint64(len([]byte(*rawCfg)))
	}
	return sz, nil
}

func AutomaticSnapshotExpiration(st *state.State) (time.Duration, error) {
	var expirationStr string
	tr := config.NewTransaction(st)
	err := tr.Get("core", "snapshots.automatic.retention", &expirationStr)
	if err != nil && !config.IsNoOption(err) {
		return 0, err
	}
	if err == nil {
		if expirationStr == "no" {
			return 0, nil
		}
		dur, err := time.ParseDuration(expirationStr)
		if err == nil {
			return dur, nil
		}
		logger.Noticef("snapshots.automatic.retention cannot be parsed: %v", err)
	}
	// TODO: automatic snapshots are currently disable by default
	// on Ubuntu Core devices
	if !release.OnClassic {
		return 0, nil
	}
	return defaultAutomaticSnapshotExpiration, nil
}

// saveExpiration saves expiration date of the given snapshot set, in the state.
// The state needs to be locked by the caller.
func saveExpiration(st *state.State, setID uint64, expiryTime time.Time) error {
	var snapshots map[uint64]*json.RawMessage
	err := st.Get("snapshots", &snapshots)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}
	if snapshots == nil {
		snapshots = make(map[uint64]*json.RawMessage)
	}
	data, err := json.Marshal(&snapshotState{
		ExpiryTime: expiryTime,
	})
	if err != nil {
		return err
	}
	raw := json.RawMessage(data)
	snapshots[setID] = &raw
	st.Set("snapshots", snapshots)
	return nil
}

// removeSnapshotState removes given set IDs from the state.
func removeSnapshotState(st *state.State, setIDs ...uint64) error {
	var snapshots map[uint64]*json.RawMessage
	err := st.Get("snapshots", &snapshots)
	if err != nil {
		if errors.Is(err, state.ErrNoState) {
			return nil
		}
		return err
	}

	for _, setID := range setIDs {
		delete(snapshots, setID)
	}

	st.Set("snapshots", snapshots)
	return nil
}

// expiredSnapshotSets returns expired snapshot sets from the state whose expiry-time is before the given cutoffTime.
// The state needs to be locked by the caller.
func expiredSnapshotSets(st *state.State, cutoffTime time.Time) (map[uint64]bool, error) {
	var snapshots map[uint64]*snapshotState
	err := st.Get("snapshots", &snapshots)
	if err != nil {
		if !errors.Is(err, state.ErrNoState) {
			return nil, err
		}
		return nil, nil
	}

	expired := make(map[uint64]bool)
	for setID, snapshotSet := range snapshots {
		if snapshotSet.ExpiryTime.Before(cutoffTime) {
			expired[setID] = true
		}
	}

	return expired, nil
}

// snapshotSnapSummaries are used internally to get useful data from a
// snapshot set when deciding whether to check/forget/restore it.
type snapshotSnapSummaries []*snapshotSnapSummary

func (summaries snapshotSnapSummaries) snapNames() []string {
	names := make([]string, len(summaries))
	for i, summary := range summaries {
		names[i] = summary.snap
	}
	return names
}

type snapshotSnapSummary struct {
	snap     string
	snapID   string
	filename string
	epoch    snap.Epoch
}

// snapSummariesInSnapshotSet goes looking for the requested snaps in the
// given snap set, and returns summaries of the matching snaps in the set.
func snapSummariesInSnapshotSet(setID uint64, requested []string) (summaries snapshotSnapSummaries, err error) {
	sort.Strings(requested)
	found := false
	err = backendIter(context.TODO(), func(r *backend.Reader) error {
		if r.SetID == setID {
			found = true
			if len(requested) == 0 || strutil.SortedListContains(requested, r.Snap) {
				summaries = append(summaries, &snapshotSnapSummary{
					filename: r.Name(),
					snap:     r.Snap,
					snapID:   r.SnapID,
					epoch:    r.Epoch,
				})
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, client.ErrSnapshotSetNotFound
	}
	if len(summaries) == 0 {
		return nil, client.ErrSnapshotSnapsNotFound
	}

	return summaries, nil
}

func taskGetErrMsg(task *state.Task, err error, what string) error {
	if errors.Is(err, state.ErrNoState) {
		return fmt.Errorf("internal error: task %s (%s) is missing %s information", task.ID(), task.Kind(), what)
	}
	return fmt.Errorf("internal error: retrieving %s information from task %s (%s): %v", what, task.ID(), task.Kind(), err)
}

// checkSnapshotConflict checks whether there's an in-progress task for snapshots with the given set id.
func checkSnapshotConflict(st *state.State, setID uint64, conflictingKinds ...string) error {
	if val := st.Cached("snapshot-ops"); val != nil {
		snapshotOps, _ := val.(map[uint64]string)
		if op, ok := snapshotOps[setID]; ok {
			for _, conflicting := range conflictingKinds {
				if op == conflicting {
					return fmt.Errorf("cannot operate on snapshot set #%d while operation %s is in progress", setID, op)
				}
			}
		}
	}
	for _, task := range st.Tasks() {
		if task.Change().IsReady() {
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
func List(ctx context.Context, st *state.State, setID uint64, snapNames []string) ([]client.SnapshotSet, error) {
	sets, err := backendList(ctx, setID, snapNames)
	if err != nil {
		return nil, err
	}

	var snapshots map[uint64]*snapshotState
	if err := st.Get("snapshots", &snapshots); err != nil && !errors.Is(err, state.ErrNoState) {
		return nil, err
	}

	// decorate all snapshots with "auto" flag if we have expiry time set for them.
	for _, sset := range sets {
		// at the moment we only keep records with expiry time so checking non-zero
		// expiry-time is not strictly necessary, but it makes it future-proof in case
		// we add more attributes to these entries.
		if snapshotState, ok := snapshots[sset.ID]; ok && !snapshotState.ExpiryTime.IsZero() {
			for _, snapshot := range sset.Snapshots {
				snapshot.Auto = true
			}
		}
	}

	return sets, nil
}

// Import a given snapshot ID from an exported snapshot
func Import(ctx context.Context, st *state.State, r io.Reader) (setID uint64, snapNames []string, err error) {
	st.Lock()
	setID, err = newSnapshotSetID(st)
	// note, this is a new set id which is not exposed yet, no need to mark it
	// for conflicts via snapshotOp. Also, since we're keeping state lock while
	// checking conflicts below, there is no need to for setSnapshotOpInProgress.
	st.Unlock()
	if err != nil {
		return 0, nil, err
	}

	snapNames, err = backendImport(ctx, setID, r, nil)
	if err != nil {
		if dupErr, ok := err.(backend.DuplicatedSnapshotImportError); ok {
			st.Lock()
			defer st.Unlock()

			if err := checkSnapshotConflict(st, dupErr.SetID, "forget-snapshot"); err != nil {
				// we found an existing snapshot but it's being forgotten, so
				// retry the import without checking for existing snapshot.
				flags := &backend.ImportFlags{NoDuplicatedImportCheck: true}
				st.Unlock()
				snapNames, err = backendImport(ctx, setID, r, flags)
				st.Lock()
				return setID, snapNames, err
			}

			// trying to import identical snapshot; instead return set ID of
			// the existing one and reset its expiry time.
			// XXX: at the moment expiry-time is the only attribute so we can
			// just remove the record. If we ever add more attributes this needs
			// to reset expiry-time only.
			if err := removeSnapshotState(st, dupErr.SetID); err != nil {
				return 0, nil, err
			}
			return dupErr.SetID, dupErr.SnapNames, nil
		}
		return 0, nil, err
	}
	return setID, snapNames, nil
}

// Save creates a taskset for taking snapshots of snaps' data.
// Note that the state must be locked by the caller.
func Save(st *state.State, instanceNames []string, users []string, options map[string]*snap.SnapshotOptions) (setID uint64, snapsSaved []string, ts *state.TaskSet, err error) {
	if len(instanceNames) == 0 {
		instanceNames, err = allActiveSnapNames(st)
		if err != nil {
			return 0, nil, nil, err
		}
	} else {
		installedSnaps, err := snapstate.All(st)
		if err != nil {
			return 0, nil, nil, err
		}

		for _, name := range instanceNames {
			if _, ok := installedSnaps[name]; !ok {
				return 0, nil, nil, &snap.NotInstalledError{Snap: name}
			}
		}
	}

	// Make sure we do not snapshot if anything like install/remove/refresh is in progress
	if err := snapstateCheckChangeConflictMany(st, instanceNames, ""); err != nil {
		return 0, nil, nil, err
	}

	setID, err = newSnapshotSetID(st)
	if err != nil {
		return 0, nil, nil, err
	}

	ts = state.NewTaskSet()

	for _, name := range instanceNames {
		desc := fmt.Sprintf("Save data of snap %q in snapshot set #%d", name, setID)
		task := st.NewTask("save-snapshot", desc)

		snapshot := snapshotSetup{
			SetID:   setID,
			Snap:    name,
			Users:   users,
			Options: options[name],
		}

		task.Set("snapshot-setup", &snapshot)
		// Here, note that a snapshot set behaves as a unit: it either
		// succeeds, or fails, as a whole; we don't use lanes, to have
		// some snaps' snapshot succeed and not others in a single set.
		// In practice: either the snapshot will be automatic and only
		// for one snap (already in a lane via refresh), or it will be
		// done by hand and the user can remove failing snaps (or find
		// the cause of the failure). A snapshot failure can happen if
		// a user has dropped files they can't read in their directory,
		// for example.
		// Also note we aren't promising this behaviour; we can change
		// it if we find it to be wrong.
		ts.AddTask(task)
	}

	return setID, instanceNames, ts, nil
}

func AutomaticSnapshot(st *state.State, snapName string) (ts *state.TaskSet, err error) {
	expiration, err := AutomaticSnapshotExpiration(st)
	if err != nil {
		return nil, err
	}
	if expiration == 0 {
		return nil, snapstate.ErrNothingToDo
	}
	setID, err := newSnapshotSetID(st)
	if err != nil {
		return nil, err
	}

	ts = state.NewTaskSet()
	desc := fmt.Sprintf("Save data of snap %q in automatic snapshot set #%d", snapName, setID)
	task := st.NewTask("save-snapshot", desc)
	snapshot := snapshotSetup{
		SetID: setID,
		Snap:  snapName,
		Auto:  true,
	}
	task.Set("snapshot-setup", &snapshot)
	ts.AddTask(task)

	return ts, nil
}

// Restore creates a taskset for restoring a snapshot's data.
// Note that the state must be locked by the caller.
func Restore(st *state.State, setID uint64, snapNames []string, users []string) (snapsFound []string, ts *state.TaskSet, err error) {
	summaries, err := snapSummariesInSnapshotSet(setID, snapNames)
	if err != nil {
		return nil, nil, err
	}
	all, err := snapstateAll(st)
	if err != nil {
		return nil, nil, err
	}

	snapsFound = summaries.snapNames()

	if err := snapstateCheckChangeConflictMany(st, snapsFound, ""); err != nil {
		return nil, nil, err
	}

	// restore needs to conflict with forget of itself
	if err := checkSnapshotConflict(st, setID, "forget-snapshot"); err != nil {
		return nil, nil, err
	}

	ts = state.NewTaskSet()

	for _, summary := range summaries {
		var current snap.Revision
		if snapst, ok := all[summary.snap]; ok {
			info, err := snapst.CurrentInfo()
			if err != nil {
				// how?
				return nil, nil, fmt.Errorf("unexpected error while reading snap info: %v", err)
			}
			if !info.Epoch.CanRead(summary.epoch) {
				const tpl = "cannot restore snapshot for %q: current snap (epoch %s) cannot read snapshot data (epoch %s)"
				return nil, nil, fmt.Errorf(tpl, summary.snap, &info.Epoch, &summary.epoch)
			}
			if summary.snapID != "" && info.SnapID != "" && info.SnapID != summary.snapID {
				const tpl = "cannot restore snapshot for %q: current snap (ID %.7s…) does not match snapshot (ID %.7s…)"
				return nil, nil, fmt.Errorf(tpl, summary.snap, info.SnapID, summary.snapID)
			}
			current = snapst.Current
		}

		desc := fmt.Sprintf("Restore data of snap %q from snapshot set #%d", summary.snap, setID)
		task := st.NewTask("restore-snapshot", desc)
		snapshot := snapshotSetup{
			SetID:    setID,
			Snap:     summary.snap,
			Users:    users,
			Filename: summary.filename,
			Current:  current,
		}
		task.Set("snapshot-setup", &snapshot)
		// see the note about snapshots not using lanes, above.
		ts.AddTask(task)
	}

	if len(summaries) > 0 {
		// take care of cleaning up all restore working state if all the
		// restore tasks succeeded; if they didn't, the undo logic will take
		// care of this
		desc := fmt.Sprintf("Cleanup after restore from snapshot set #%d", setID)
		task := st.NewTask("cleanup-after-restore", desc)
		task.WaitAll(ts)
		ts.AddTask(task)
	}

	return snapsFound, ts, nil
}

// Check creates a taskset for checking a snapshot's data.
// Note that the state must be locked by the caller.
func Check(st *state.State, setID uint64, snapNames []string, users []string) (snapsFound []string, ts *state.TaskSet, err error) {
	// check needs to conflict with forget of itself
	if err := checkSnapshotConflict(st, setID, "forget-snapshot"); err != nil {
		return nil, nil, err
	}

	summaries, err := snapSummariesInSnapshotSet(setID, snapNames)
	if err != nil {
		return nil, nil, err
	}

	ts = state.NewTaskSet()

	for _, summary := range summaries {
		desc := fmt.Sprintf("Check data of snap %q in snapshot set #%d", summary.snap, setID)
		task := st.NewTask("check-snapshot", desc)
		snapshot := snapshotSetup{
			SetID:    setID,
			Snap:     summary.snap,
			Users:    users,
			Filename: summary.filename,
		}
		task.Set("snapshot-setup", &snapshot)
		ts.AddTask(task)
	}

	return summaries.snapNames(), ts, nil
}

// Forget creates a taskset for deletinig a snapshot.
// Note that the state must be locked by the caller.
func Forget(st *state.State, setID uint64, snapNames []string) (snapsFound []string, ts *state.TaskSet, err error) {
	// forget needs to conflict with check, restore, import and export.
	if err := checkSnapshotConflict(st, setID, "export-snapshot",
		"check-snapshot", "restore-snapshot"); err != nil {
		return nil, nil, err
	}

	summaries, err := snapSummariesInSnapshotSet(setID, snapNames)
	if err != nil {
		return nil, nil, err
	}

	ts = state.NewTaskSet()
	for _, summary := range summaries {
		desc := fmt.Sprintf("Drop data of snap %q from snapshot set #%d", summary.snap, setID)
		task := st.NewTask("forget-snapshot", desc)
		snapshot := snapshotSetup{
			SetID:    setID,
			Snap:     summary.snap,
			Filename: summary.filename,
		}
		task.Set("snapshot-setup", &snapshot)
		ts.AddTask(task)
	}

	return summaries.snapNames(), ts, nil
}

// setSnapshotOpInProgress marks the given set ID as being a subject of
// snapshot op inside state cache. The state must be locked by the caller.
func setSnapshotOpInProgress(st *state.State, setID uint64, op string) {
	var snapshotOps map[uint64]string
	if val := st.Cached("snapshot-ops"); val != nil {
		snapshotOps, _ = val.(map[uint64]string)
	} else {
		snapshotOps = make(map[uint64]string)
	}
	snapshotOps[setID] = op
	st.Cache("snapshot-ops", snapshotOps)
}

// UnsetSnapshotOpInProgress un-sets the given set ID as being a
// subject of a snapshot op. It returns the last operation (or empty string
// if no op was marked active).
// The state must be locked by the caller.
func UnsetSnapshotOpInProgress(st *state.State, setID uint64) string {
	var op string
	if val := st.Cached("snapshot-ops"); val != nil {
		var snapshotOps map[uint64]string
		snapshotOps, _ = val.(map[uint64]string)
		op = snapshotOps[setID]
		delete(snapshotOps, setID)
		st.Cache("snapshot-ops", snapshotOps)
	}
	return op
}

// Export exports a given snapshot ID
// Note that the state must be locked by the caller.
func Export(ctx context.Context, st *state.State, setID uint64) (se *backend.SnapshotExport, err error) {
	if err := checkSnapshotConflict(st, setID, "forget-snapshot"); err != nil {
		return nil, err
	}

	setSnapshotOpInProgress(st, setID, "export-snapshot")
	se, err = backendNewSnapshotExport(ctx, setID)
	if err != nil {
		UnsetSnapshotOpInProgress(st, setID)
	}
	return se, err
}

// SnapshotExport provides a snapshot export that can be streamed out
type SnapshotExport = backend.SnapshotExport
