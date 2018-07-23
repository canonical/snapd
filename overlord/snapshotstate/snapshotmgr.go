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
	return &SnapshotManager{}
}

// Ensure is part of the overlord.StateManager interface.
func (m *SnapshotManager) Ensure() error { return nil }

// Wait is part of the overlord.StateManager interface.
func (m *SnapshotManager) Wait() {}

// Stop is part of the overlord.StateManager interface.
func (m *SnapshotManager) Stop() {}

type snapshotState struct {
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
func prepareSave(task *state.Task) (snapshot *snapshotState, cur *snap.Info, cfg map[string]interface{}, err error) {
	st := task.State()
	st.Lock()
	defer st.Unlock()

	if err := task.Get("snapshot", &snapshot); err != nil {
		return nil, nil, nil, err
	}
	cur, err = snapstateCurrentInfo(st, snapshot.Snap)
	if err != nil {
		return nil, nil, nil, err
	}
	snapshot.Filename = filename(snapshot.SetID, cur)
	task.Set("snapshot", &snapshot)

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
func prepareRestore(task *state.Task) (snapshot *snapshotState, oldCfg map[string]interface{}, reader *backend.Reader, err error) {
	st := task.State()

	st.Lock()
	defer st.Unlock()

	if err := task.Get("snapshot", &snapshot); err != nil {
		return nil, nil, nil, err
	}

	rawCfg, err := configGetSnapConfig(st, snapshot.Snap)
	if err != nil {
		return nil, nil, nil, err
	}

	if rawCfg != nil {
		if err := json.Unmarshal(*rawCfg, &oldCfg); err != nil {
			return nil, nil, nil, err
		}
	}

	reader, err = backendOpen(snapshot.Filename)
	if err != nil {
		return nil, nil, nil, err
	}

	return snapshot, oldCfg, reader, nil
}

func doRestore(task *state.Task, tomb *tomb.Tomb) error {
	snapshot, oldCfg, reader, err := prepareRestore(task)
	if err != nil {
		return err
	}

	restoreState, err := backendRestore(reader, tomb.Context(nil), snapshot.Users, task.Logf)
	if err != nil {
		return err
	}

	buf, err := json.Marshal(reader.Conf)
	if err != nil {
		backendRevert(restoreState)
		return err
	}

	st := task.State()
	st.Lock()
	defer st.Unlock()

	if err := configSetSnapConfig(st, snapshot.Snap, (*json.RawMessage)(&buf)); err != nil {
		backendRevert(restoreState)
		return err
	}

	restoreState.Config = oldCfg
	task.Set("restore-state", restoreState)

	return nil
}

func undoRestore(task *state.Task, _ *tomb.Tomb) error {
	var restoreState backend.RestoreState
	var snapshot snapshotState

	st := task.State()
	st.Lock()
	defer st.Unlock()

	if err := task.Get("restore-state", &restoreState); err != nil {
		return err
	}
	if err := task.Get("snapshot", &snapshot); err != nil {
		return err
	}

	buf, err := json.Marshal(restoreState.Config)
	if err != nil {
		// augh
		return err
	}

	if err := configSetSnapConfig(st, snapshot.Snap, (*json.RawMessage)(&buf)); err != nil {
		return err
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
		// this is bad :-(
		return err
	}
	st.Unlock()

	if status != state.DoneStatus {
		return nil
	}

	backendCleanup(&restoreState)

	return nil
}

func doCheck(task *state.Task, tomb *tomb.Tomb) error {
	var snapshot snapshotState

	st := task.State()
	st.Lock()
	err := task.Get("snapshot", &snapshot)
	st.Unlock()
	if err != nil {
		return err
	}

	sh, err := backendOpen(snapshot.Filename)
	if err != nil {
		return err
	}

	return backendCheck(sh, tomb.Context(nil), snapshot.Users)
}

func doForget(task *state.Task, _ *tomb.Tomb) error {
	// note this is also undoSave
	st := task.State()
	st.Lock()

	var snapshot snapshotState
	err := task.Get("snapshot", &snapshot)
	st.Unlock()
	if err != nil {
		return err
	}

	return osRemove(snapshot.Filename)
}
