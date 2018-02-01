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
	runner.AddHandler("save-snapshot", doSave, doLose)
	runner.AddHandler("lose-snapshot", doLose, nil)
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

type shotState struct {
	ID       uint64   `json:"id"`
	Snap     string   `json:"snap"`
	Homes    []string `json:"homes"`
	Filename string   `json:"filename"`
}

func filename(id uint64, si *snap.Info) string {
	skel := &client.Snapshot{
		ID:       id,
		Snap:     si.Name(),
		Revision: si.Revision,
		Version:  si.Version,
	}
	return backend.Filename(skel)
}

func prepareSave(task *state.Task, tomb *tomb.Tomb) (*shotState, *snap.Info, *json.RawMessage, error) {
	st := task.State()
	st.Lock()
	defer st.Unlock()

	var shot shotState
	if err := task.Get("shot", &shot); err != nil {
		return nil, nil, nil, err
	}
	cur, err := snapstate.CurrentInfo(st, shot.Snap)
	if err != nil {
		return nil, nil, nil, err
	}
	shot.Filename = filename(shot.ID, cur)
	task.Set("shot", &shot)

	cfg, err := config.GetSnapConfig(st, shot.Snap)
	if err != nil {
		return nil, nil, nil, err
	}

	return &shot, cur, cfg, nil
}

func doSave(task *state.Task, tomb *tomb.Tomb) error {
	shot, cur, cfg, err := prepareSave(task, tomb)
	if err != nil {
		return err
	}
	_, err = backend.Save(tomb.Context(nil), shot.ID, cur, cfg, shot.Homes)
	return err
}

func prepareRestore(task *state.Task, tomb *tomb.Tomb) (*shotState, *json.RawMessage, *backend.Reader, error) {
	st := task.State()

	st.Lock()
	defer st.Unlock()

	var shot shotState
	if err := task.Get("shot", &shot); err != nil {
		return nil, nil, nil, err
	}

	oldCfg, err := config.GetSnapConfig(st, shot.Snap)
	if err != nil {
		return nil, nil, nil, err
	}

	reader, err := backend.Open(shot.Filename)
	if err != nil {
		return nil, nil, nil, err
	}

	return &shot, oldCfg, reader, nil
}

func doRestore(task *state.Task, tomb *tomb.Tomb) error {
	shot, oldCfg, reader, err := prepareRestore(task, tomb)
	if err != nil {
		return err
	}

	backup, err := reader.RestoreLeavingBackup(tomb.Context(nil), shot.Homes, task.Logf)
	if err != nil {
		return err
	}

	st := task.State()
	st.Lock()
	defer st.Unlock()

	if err := config.SetSnapConfig(st, shot.Snap, reader.Config); err != nil {
		backup.Revert()
		return err
	}

	backup.Config = oldCfg
	task.Set("backup", backup)

	return nil
}

func undoRestore(t *state.Task, _ *tomb.Tomb) error {
	var backup backend.Backup
	err := func() error {
		var shot shotState
		st := t.State()

		st.Lock()
		defer st.Unlock()

		t.Get("backup", &backup)
		t.Get("shot", &shot)

		return config.SetSnapConfig(st, shot.Snap, backup.Config)
	}()

	backup.Revert()

	return err
}

func cleanupRestore(task *state.Task, _ *tomb.Tomb) error {
	println("*** cleanup restore")
	var backup backend.Backup

	st := task.State()
	st.Lock()
	status := task.Status()
	task.Get("backup", &backup)
	st.Unlock()

	if status != state.DoneStatus {
		return nil
	}

	backup.Cleanup()

	return nil
}

func doCheck(task *state.Task, tomb *tomb.Tomb) error {
	var shot shotState

	st := task.State()
	st.Lock()
	err := task.Get("shot", &shot)
	st.Unlock()
	if err != nil {
		return err
	}

	sh, err := backend.Open(shot.Filename)
	if err != nil {
		return err
	}

	return sh.Check(tomb.Context(nil), shot.Homes)
}

func doLose(task *state.Task, _ *tomb.Tomb) error {
	// note this is also undoSave
	st := task.State()
	st.Lock()

	var shot shotState
	err := task.Get("shot", &shot)
	st.Unlock()
	if err != nil {
		return err
	}

	return os.Remove(shot.Filename)
}
