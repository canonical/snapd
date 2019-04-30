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
	"fmt"
	"os"
	"time"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/snapshotstate/backend"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

var (
	osRemove             = os.Remove
	snapstateCurrentInfo = snapstate.CurrentInfo
	configGetSnapConfig  = config.GetSnapConfig
	configSetSnapConfig  = config.SetSnapConfig
	backendOpen          = backend.Open
	backendSave          = backend.Save
	backendRestore       = (*backend.Reader).Restore // TODO: look into using an interface instead
	backendCheck         = (*backend.Reader).Check
	backendRevert        = (*backend.RestoreState).Revert // ditto
	backendCleanup       = (*backend.RestoreState).Cleanup

	autoExpirationInterval = time.Hour * 24 // interval between forgetExpiredSnapshots runs as part of Ensure()
)

// SnapshotManager takes snapshots of active snaps
type SnapshotManager struct {
	state *state.State

	lastForgetExpiredSnapshotTime time.Time
}

// Manager returns a new SnapshotManager
func Manager(st *state.State, runner *state.TaskRunner) *SnapshotManager {
	delayedCrossMgrInit()

	runner.AddHandler("save-snapshot", doSave, doForget)
	runner.AddHandler("forget-snapshot", doForget, nil)
	runner.AddHandler("check-snapshot", doCheck, nil)
	runner.AddHandler("restore-snapshot", doRestore, undoRestore)
	runner.AddCleanup("restore-snapshot", cleanupRestore)

	manager := &SnapshotManager{
		state: st,
	}
	snapstate.AddAffectedSnapsByAttr("snapshot-setup", manager.affectedSnaps)

	return manager
}

// Ensure is part of the overlord.StateManager interface.
func (mgr *SnapshotManager) Ensure() error {
	// process expired snapshots once a day.
	if time.Now().After(mgr.lastForgetExpiredSnapshotTime.Add(autoExpirationInterval)) {
		return mgr.forgetExpiredSnapshots()
	}
	return nil
}

func (mgr *SnapshotManager) forgetExpiredSnapshots() error {
	mgr.state.Lock()
	defer mgr.state.Unlock()

	sets, err := expiredSnapshotSets(mgr.state, time.Now())
	if err != nil {
		return fmt.Errorf("internal error: cannot determine expired snapshots: %v", err)
	}

	if len(sets) == 0 {
		return nil
	}

	err = backendIter(context.TODO(), func(r *backend.Reader) error {
		// forget needs to conflict with check and restore
		if err := checkSnapshotTaskConflict(mgr.state, r.SetID, "check-snapshot", "restore-snapshot"); err != nil {
			// there is a conflict, do nothing and we will retry this set on next Ensure().
			return nil
		}
		if sets[r.SetID] {
			delete(sets, r.SetID)
			// remove from state first: in case removeSnapshotState succeeds but osRemove fails we will never attempt
			// to automatically remove this snapshot again and will leave it on the disk (so the user can still try to remove it manually);
			// this is better than the other way around where a failing osRemove would be retried forever because snapshot would never
			// leave the state.
			if err := removeSnapshotState(mgr.state, r.SetID); err != nil {
				return fmt.Errorf("internal error: cannot remove state of snapshot set %d: %v", r.SetID, err)
			}
			if err := osRemove(r.Name()); err != nil {
				return fmt.Errorf("cannot remove snapshot file %q: %v", r.Name(), err)
			}
		}
		return nil
	})

	if err != nil {
		return fmt.Errorf("cannot process expired snapshots: %v", err)
	}

	// only reset time if there are no sets left because of conflicts
	if len(sets) == 0 {
		mgr.lastForgetExpiredSnapshotTime = time.Now()
	}

	return nil
}

func (SnapshotManager) affectedSnaps(t *state.Task) ([]string, error) {
	if k := t.Kind(); k == "check-snapshot" || k == "forget-snapshot" {
		// check and forget don't affect snaps
		// (this could also be written k != save && k != restore, but it's safer this way around)
		return nil, nil
	}
	var snapshot snapshotSetup
	if err := t.Get("snapshot-setup", &snapshot); err != nil {
		return nil, taskGetErrMsg(t, err, "snapshot")
	}

	return []string{snapshot.Snap}, nil
}

type snapshotSetup struct {
	SetID    uint64        `json:"set-id"`
	Snap     string        `json:"snap"`
	Users    []string      `json:"users,omitempty"`
	Filename string        `json:"filename,omitempty"`
	Current  snap.Revision `json:"current"`
	Auto     bool          `json:"auto,omitempty"`
}

func filename(setID uint64, si *snap.Info) string {
	skel := &client.Snapshot{
		SetID:    setID,
		Snap:     si.InstanceName(),
		Revision: si.Revision,
		Version:  si.Version,
	}
	return backend.Filename(skel)
}

// prepareSave does all the steps of doSave that require the state lock;
// it has no real significance beyond making the lock handling simpler
func prepareSave(task *state.Task) (snapshot *snapshotSetup, cur *snap.Info, cfg map[string]interface{}, err error) {
	st := task.State()
	st.Lock()
	defer st.Unlock()

	if err := task.Get("snapshot-setup", &snapshot); err != nil {
		return nil, nil, nil, taskGetErrMsg(task, err, "snapshot")
	}
	cur, err = snapstateCurrentInfo(st, snapshot.Snap)
	if err != nil {
		return nil, nil, nil, err
	}
	// updating snapshot-setup with the filename, for use in undo
	snapshot.Filename = filename(snapshot.SetID, cur)
	task.Set("snapshot-setup", &snapshot)

	rawCfg, err := configGetSnapConfig(st, snapshot.Snap)
	if err != nil {
		return nil, nil, nil, err
	}
	if rawCfg != nil {
		if err := json.Unmarshal(*rawCfg, &cfg); err != nil {
			return nil, nil, nil, err
		}
	}

	// this should be done last because of it modifies the state and the caller needs to undo this if other operation fails.
	if snapshot.Auto {
		expiration, err := AutomaticSnapshotExpiration(st)
		if err != nil {
			return nil, nil, nil, err
		}
		if err := saveExpiration(st, snapshot.SetID, time.Now().Add(expiration)); err != nil {
			return nil, nil, nil, err
		}
	}

	return snapshot, cur, cfg, nil
}

func doSave(task *state.Task, tomb *tomb.Tomb) error {
	snapshot, cur, cfg, err := prepareSave(task)
	if err != nil {
		return err
	}
	_, err = backendSave(tomb.Context(nil), snapshot.SetID, cur, cfg, snapshot.Users, &backend.Flags{Auto: snapshot.Auto})
	if err != nil {
		st := task.State()
		st.Lock()
		defer st.Unlock()
		removeSnapshotState(st, snapshot.SetID)
	}
	return err
}

// prepareRestore does the steps of doRestore that require the state lock
// before the backend Restore call.
func prepareRestore(task *state.Task) (snapshot *snapshotSetup, oldCfg map[string]interface{}, reader *backend.Reader, err error) {
	st := task.State()

	st.Lock()
	defer st.Unlock()

	if err := task.Get("snapshot-setup", &snapshot); err != nil {
		return nil, nil, nil, taskGetErrMsg(task, err, "snapshot")
	}

	rawCfg, err := configGetSnapConfig(st, snapshot.Snap)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("internal error: cannot obtain current snap config for snapshot restore: %v", err)
	}

	if rawCfg != nil {
		if err := json.Unmarshal(*rawCfg, &oldCfg); err != nil {
			return nil, nil, nil, fmt.Errorf("internal error: cannot decode current snap config: %v", err)
		}
	}

	reader, err = backendOpen(snapshot.Filename)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("cannot open snapshot: %v", err)
	}
	// note given the Open succeeded, caller needs to close it when done

	return snapshot, oldCfg, reader, nil
}

func doRestore(task *state.Task, tomb *tomb.Tomb) error {
	snapshot, oldCfg, reader, err := prepareRestore(task)
	if err != nil {
		return err
	}
	defer reader.Close()

	st := task.State()
	logf := func(format string, args ...interface{}) {
		st.Lock()
		defer st.Unlock()
		task.Logf(format, args...)
	}

	restoreState, err := backendRestore(reader, tomb.Context(nil), snapshot.Current, snapshot.Users, logf)
	if err != nil {
		return err
	}

	buf, err := json.Marshal(reader.Conf)
	if err != nil {
		backendRevert(restoreState)
		return fmt.Errorf("cannot marshal saved config: %v", err)
	}

	st.Lock()
	defer st.Unlock()

	if err := configSetSnapConfig(st, snapshot.Snap, (*json.RawMessage)(&buf)); err != nil {
		backendRevert(restoreState)
		return fmt.Errorf("cannot set snap config: %v", err)
	}

	restoreState.Config = oldCfg
	task.Set("restore-state", restoreState)

	return nil
}

func undoRestore(task *state.Task, _ *tomb.Tomb) error {
	var restoreState backend.RestoreState
	var snapshot snapshotSetup

	st := task.State()
	st.Lock()
	defer st.Unlock()

	if err := task.Get("restore-state", &restoreState); err != nil {
		return taskGetErrMsg(task, err, "snapshot restore")
	}
	if err := task.Get("snapshot-setup", &snapshot); err != nil {
		return taskGetErrMsg(task, err, "snapshot")
	}

	buf, err := json.Marshal(restoreState.Config)
	if err != nil {
		return fmt.Errorf("cannot marshal saved config: %v", err)
	}

	if err := configSetSnapConfig(st, snapshot.Snap, (*json.RawMessage)(&buf)); err != nil {
		return fmt.Errorf("cannot restore saved config: %v", err)
	}

	backendRevert(&restoreState)

	return nil
}

func cleanupRestore(task *state.Task, _ *tomb.Tomb) error {
	var restoreState backend.RestoreState

	st := task.State()
	st.Lock()
	status := task.Status()
	err := task.Get("restore-state", &restoreState)
	st.Unlock()

	if status != state.DoneStatus {
		// only need to clean up restores that worked
		return nil
	}

	if err != nil {
		// this is bad: we somehow lost the information to restore things
		// but if we return the error we'll just get called again :-(
		// TODO: use warnings :-)
		logger.Noticef("%v", taskGetErrMsg(task, err, "snapshot restore"))
		return nil
	}

	backendCleanup(&restoreState)

	return nil
}

func doCheck(task *state.Task, tomb *tomb.Tomb) error {
	var snapshot snapshotSetup

	st := task.State()
	st.Lock()
	err := task.Get("snapshot-setup", &snapshot)
	st.Unlock()
	if err != nil {
		return taskGetErrMsg(task, err, "snapshot")
	}

	reader, err := backendOpen(snapshot.Filename)
	if err != nil {
		return fmt.Errorf("cannot open snapshot: %v", err)
	}
	defer reader.Close()

	return backendCheck(reader, tomb.Context(nil), snapshot.Users)
}

func doForget(task *state.Task, _ *tomb.Tomb) error {
	// note this is also undoSave
	st := task.State()
	st.Lock()
	defer st.Unlock()

	var snapshot snapshotSetup
	err := task.Get("snapshot-setup", &snapshot)

	if err != nil {
		return taskGetErrMsg(task, err, "snapshot")
	}

	if snapshot.Filename == "" {
		return fmt.Errorf("internal error: task %s (%s) snapshot info is missing the filename", task.ID(), task.Kind())
	}

	// in case it's an automatic snapshot, remove the set also from the state (automatic snapshots have just one snap per set).
	if err := removeSnapshotState(st, snapshot.SetID); err != nil {
		return fmt.Errorf("internal error: cannot remove state of snapshot set %d: %v", snapshot.SetID, err)
	}

	return osRemove(snapshot.Filename)
}

func delayedCrossMgrInit() {
	// hook automatic snapshots into snapstate logic
	snapstate.AutomaticSnapshot = AutomaticSnapshot
	snapstate.AutomaticSnapshotExpiration = AutomaticSnapshotExpiration
}

func MockBackendSave(f func(context.Context, uint64, *snap.Info, map[string]interface{}, []string, *backend.Flags) (*client.Snapshot, error)) (restore func()) {
	old := backendSave
	backendSave = f
	return func() {
		backendSave = old
	}
}
