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
	"encoding/json"
	"fmt"
	"os"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/client"
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
)

// SnapshotManager takes snapshots of active snaps
type SnapshotManager struct{}

// Manager returns a new SnapshotManager
func Manager(st *state.State, runner *state.TaskRunner) *SnapshotManager {
	runner.AddHandler("save-snapshot", doSave, doForget)
	runner.AddHandler("forget-snapshot", doForget, nil)
	runner.AddHandler("check-snapshot", doCheck, nil)
	runner.AddHandler("restore-snapshot", doRestore, undoRestore)
	runner.AddCleanup("restore-snapshot", cleanupRestore)

	manager := &SnapshotManager{}
	snapstate.AddAffectedSnapsByAttr("snapshot-setup", manager.affectedSnaps)

	return manager
}

// Ensure is part of the overlord.StateManager interface.
func (SnapshotManager) Ensure() error { return nil }

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
	SetID    uint64   `json:"set-id"`
	Snap     string   `json:"snap"`
	Users    []string `json:"users,omitempty"`
	Filename string   `json:"filename,omitempty"`
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

	return snapshot, cur, cfg, nil
}

func doSave(task *state.Task, tomb *tomb.Tomb) error {
	snapshot, cur, cfg, err := prepareSave(task)
	if err != nil {
		return err
	}
	_, err = backendSave(tomb.Context(nil), snapshot.SetID, cur, cfg, snapshot.Users)
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

	restoreState, err := backendRestore(reader, tomb.Context(nil), snapshot.Users, task.Logf)
	if err != nil {
		return err
	}

	buf, err := json.Marshal(reader.Conf)
	if err != nil {
		backendRevert(restoreState)
		return fmt.Errorf("cannot marshal saved config: %v", err)
	}

	st := task.State()
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
	if err := task.Get("restore-state", &restoreState); err != nil {
		// this is bad: we somehow lost the information to restore things
		return taskGetErrMsg(task, err, "snapshot restore")
	}
	st.Unlock()

	if status != state.DoneStatus {
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

	var snapshot snapshotSetup
	err := task.Get("snapshot-setup", &snapshot)
	st.Unlock()
	if err != nil {
		return taskGetErrMsg(task, err, "snapshot")
	}

	if snapshot.Filename == "" {
		return fmt.Errorf("internal error: task %s (%s) snapshot info is missing the filename", task.ID(), task.Kind())
	}

	return osRemove(snapshot.Filename)
}
