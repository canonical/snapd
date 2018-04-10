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

// SnapshotManager takes snapshots of active snaps
type SnapshotManager struct {
	runner *state.TaskRunner
}

// Manager returns a new SnapshotManager
func Manager(st *state.State) *SnapshotManager {
	runner := state.NewTaskRunner(st)
	runner.AddHandler("save-snapshot", doSave, doForget)
	runner.AddHandler("forget-snapshot", doForget, nil)
	runner.AddHandler("check-snapshot", doCheck, nil)
	runner.AddHandler("restore-snapshot", doRestore, undoRestore)
	runner.AddCleanup("restore-snapshot", cleanupRestore)
	return &SnapshotManager{runner: runner}
}

// KnownTaskKinds is part of the overlord.StateManager interface.
func (m *SnapshotManager) KnownTaskKinds() []string {
	return m.runner.KnownTaskKinds()
}

// Ensure is part of the overlord.StateManager interface.
func (m *SnapshotManager) Ensure() error {
	m.runner.Ensure()
	return nil
}

// Wait is part of the overlord.StateManager interface.
func (m *SnapshotManager) Wait() {
	m.runner.Wait()
}

// Stop is part of the overlord.StateManager interface.
func (m *SnapshotManager) Stop() {
	m.runner.Stop()
}

type snapshotState struct {
	SetID    uint64   `json:"set-id"`
	Snap     string   `json:"snap"`
	Users    []string `json:"users"`
	Filename string   `json:"filename"`
}

func filename(setID uint64, si *snap.Info) string {
	skel := &client.Snapshot{
		SetID:    setID,
		Snap:     si.Name(),
		Revision: si.Revision,
		Version:  si.Version,
	}
	return backend.Filename(skel)
}

func prepareSave(task *state.Task) (*snapshotState, *snap.Info, map[string]interface{}, error) {
	st := task.State()
	st.Lock()
	defer st.Unlock()

	var snapshot snapshotState
	if err := task.Get("snapshot", &snapshot); err != nil {
		return nil, nil, nil, err
	}
	cur, err := snapstate.CurrentInfo(st, snapshot.Snap)
	if err != nil {
		return nil, nil, nil, err
	}
	snapshot.Filename = filename(snapshot.SetID, cur)
	task.Set("snapshot", &snapshot)

	rawCfg, err := config.GetSnapConfig(st, snapshot.Snap)
	if err != nil {
		return nil, nil, nil, err
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(*rawCfg, &cfg); err != nil {
		cfg = nil
	}

	return &snapshot, cur, cfg, nil
}

func doSave(task *state.Task, tomb *tomb.Tomb) error {
	snapshot, cur, cfg, err := prepareSave(task)
	if err != nil {
		return err
	}
	_, err = backend.Save(tomb.Context(nil), snapshot.SetID, cur, cfg, snapshot.Users)
	return err
}

func prepareRestore(task *state.Task) (*snapshotState, *json.RawMessage, *backend.Reader, error) {
	st := task.State()

	st.Lock()
	defer st.Unlock()

	var snapshot snapshotState
	if err := task.Get("snapshot", &snapshot); err != nil {
		return nil, nil, nil, err
	}

	oldCfg, err := config.GetSnapConfig(st, snapshot.Snap)
	if err != nil {
		return nil, nil, nil, err
	}

	reader, err := backend.Open(snapshot.Filename)
	if err != nil {
		return nil, nil, nil, err
	}

	return &snapshot, oldCfg, reader, nil
}

func doRestore(task *state.Task, tomb *tomb.Tomb) error {
	snapshot, oldCfg, reader, err := prepareRestore(task)
	if err != nil {
		return err
	}

	trash, err := reader.RestoreLeavingTrash(tomb.Context(nil), snapshot.Users, task.Logf)
	if err != nil {
		return err
	}

	buf, err := json.Marshal(reader.Conf)
	if err != nil {
		return err
	}

	st := task.State()
	st.Lock()
	defer st.Unlock()

	if err := config.SetSnapConfig(st, snapshot.Snap, (*json.RawMessage)(&buf)); err != nil {
		trash.Revert()
		return err
	}

	trash.Config = oldCfg
	task.Set("trash", trash)

	return nil
}

func undoRestore(t *state.Task, _ *tomb.Tomb) error {
	var trash backend.Trash
	var snapshot snapshotState

	st := t.State()
	st.Lock()
	defer st.Unlock()

	t.Get("trash", &trash)
	defer trash.Revert()
	t.Get("snapshot", &snapshot)

	buf, err := json.Marshal(trash.Config)
	if err != nil {
		// augh
		return err
	}

	return config.SetSnapConfig(st, snapshot.Snap, (*json.RawMessage)(&buf))
}

func cleanupRestore(task *state.Task, _ *tomb.Tomb) error {
	var trash backend.Trash

	st := task.State()
	st.Lock()
	status := task.Status()
	task.Get("trash", &trash)
	st.Unlock()

	if status != state.DoneStatus {
		return nil
	}

	trash.Cleanup()

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

	sh, err := backend.Open(snapshot.Filename)
	if err != nil {
		return err
	}

	return sh.Check(tomb.Context(nil), snapshot.Users)
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

	return os.Remove(snapshot.Filename)
}
