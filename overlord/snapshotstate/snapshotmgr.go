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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
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
	backendImport        = backend.Import
	backendRestore       = (*backend.Reader).Restore // TODO: look into using an interface instead
	backendCheck         = (*backend.Reader).Check
	backendRevert        = (*backend.RestoreState).Revert // ditto
	backendCleanup       = (*backend.RestoreState).Cleanup

	backendCleanupAbandonedImports = backend.CleanupAbandonedImports

	autoExpirationInterval = time.Hour * 24 // interval between forgetExpiredSnapshots runs as part of Ensure()

	getSnapDirOpts = snapstate.GetSnapDirOpts
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
	runner.AddHandler("cleanup-after-restore", doCleanupAfterRestore, nil)

	manager := &SnapshotManager{
		state: st,
	}
	snapstate.RegisterAffectedSnapsByAttr("snapshot-setup", manager.affectedSnaps)

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

func (mgr *SnapshotManager) StartUp() error {
	mylog.Check2(backendCleanupAbandonedImports())

	return nil
}

func (mgr *SnapshotManager) forgetExpiredSnapshots() error {
	mgr.state.Lock()
	defer mgr.state.Unlock()

	sets := mylog.Check2(expiredSnapshotSets(mgr.state, time.Now()))

	if len(sets) == 0 {
		return nil
	}
	mylog.Check(backendIter(context.TODO(), func(r *backend.Reader) error {
		mylog.Check(
			// forget needs to conflict with check and restore
			checkSnapshotConflict(mgr.state, r.SetID, "export-snapshot",
				"check-snapshot", "restore-snapshot"))
		// there is a conflict, do nothing and we will retry this set on next Ensure().

		if sets[r.SetID] {
			delete(sets, r.SetID)
			mylog.Check(
				// remove from state first: in case removeSnapshotState succeeds but osRemove fails we will never attempt
				// to automatically remove this snapshot again and will leave it on the disk (so the user can still try to remove it manually);
				// this is better than the other way around where a failing osRemove would be retried forever because snapshot would never
				// leave the state.
				removeSnapshotState(mgr.state, r.SetID))
			mylog.Check(osRemove(r.Name()))

		}
		return nil
	}))

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
	mylog.Check(t.Get("snapshot-setup", &snapshot))

	return []string{snapshot.Snap}, nil
}

type snapshotSetup struct {
	SetID    uint64                `json:"set-id"`
	Snap     string                `json:"snap"`
	Users    []string              `json:"users,omitempty"`
	Options  *snap.SnapshotOptions `json:"options,omitempty"`
	Filename string                `json:"filename,omitempty"`
	Current  snap.Revision         `json:"current"`
	Auto     bool                  `json:"auto,omitempty"`
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
	mylog.Check(task.Get("snapshot-setup", &snapshot))

	cur = mylog.Check2(snapstateCurrentInfo(st, snapshot.Snap))

	// updating snapshot-setup with the filename, for use in undo
	snapshot.Filename = filename(snapshot.SetID, cur)
	task.Set("snapshot-setup", &snapshot)

	cfg = mylog.Check2(unmarshalSnapConfig(st, snapshot.Snap))

	// this should be done last because of it modifies the state and the caller needs to undo this if other operation fails.
	if snapshot.Auto {
		expiration := mylog.Check2(AutomaticSnapshotExpiration(st))
		mylog.Check(saveExpiration(st, snapshot.SetID, time.Now().Add(expiration)))

	}

	return snapshot, cur, cfg, nil
}

func doSave(task *state.Task, tomb *tomb.Tomb) error {
	snapshot, cur, cfg := mylog.Check4(prepareSave(task))

	st := task.State()

	st.Lock()
	opts := mylog.Check2(getSnapDirOpts(st, snapshot.Snap))
	st.Unlock()

	_ = mylog.Check2(backendSave(tomb.Context(nil), snapshot.SetID, cur, cfg, snapshot.Users, snapshot.Options, opts))

	return err
}

// prepareRestore does the steps of doRestore that require the state lock
// before the backend Restore call.
func prepareRestore(task *state.Task) (snapshot *snapshotSetup, oldCfg map[string]interface{}, reader *backend.Reader, err error) {
	st := task.State()

	st.Lock()
	defer st.Unlock()
	mylog.Check(task.Get("snapshot-setup", &snapshot))

	oldCfg = mylog.Check2(unmarshalSnapConfig(st, snapshot.Snap))

	reader = mylog.Check2(backendOpen(snapshot.Filename, backend.ExtractFnameSetID))

	// note given the Open succeeded, caller needs to close it when done

	return snapshot, oldCfg, reader, nil
}

// marshalSnapConfig encodes cfg to JSON and returns raw JSON message, unless
// cfg is nil - in this case nil is returned.
func marshalSnapConfig(cfg map[string]interface{}) (*json.RawMessage, error) {
	if cfg == nil {
		// do not marshal nil - this would result in "null" raw message which
		// we want to avoid.
		return nil, nil
	}
	buf := mylog.Check2(json.Marshal(cfg))

	raw := (*json.RawMessage)(&buf)
	return raw, err
}

func unmarshalSnapConfig(st *state.State, snapName string) (map[string]interface{}, error) {
	rawCfg := mylog.Check2(configGetSnapConfig(st, snapName))

	var cfg map[string]interface{}
	if rawCfg != nil {
		mylog.Check(json.Unmarshal(*rawCfg, &cfg))
	}
	return cfg, nil
}

func doRestore(task *state.Task, tomb *tomb.Tomb) error {
	snapshot, oldCfg, reader := mylog.Check4(prepareRestore(task))

	defer reader.Close()

	st := task.State()
	logf := func(format string, args ...interface{}) {
		st.Lock()
		defer st.Unlock()
		task.Logf(format, args...)
	}

	st.Lock()
	opts := mylog.Check2(getSnapDirOpts(st, snapshot.Snap))
	st.Unlock()

	restoreState := mylog.Check2(backendRestore(reader, tomb.Context(nil), snapshot.Current, snapshot.Users, logf, opts))

	raw := mylog.Check2(marshalSnapConfig(reader.Conf))

	st.Lock()
	defer st.Unlock()
	mylog.Check(configSetSnapConfig(st, snapshot.Snap, raw))

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
	mylog.Check(task.Get("restore-state", &restoreState))
	mylog.Check(task.Get("snapshot-setup", &snapshot))

	raw := mylog.Check2(marshalSnapConfig(restoreState.Config))
	mylog.Check(configSetSnapConfig(st, snapshot.Snap, raw))

	backendRevert(&restoreState)

	return nil
}

func doCleanupAfterRestore(task *state.Task, tomb *tomb.Tomb) error {
	st := task.State()
	st.Lock()
	restoreTasks := task.WaitTasks()
	st.Unlock()
	for _, t := range restoreTasks {
		mylog.Check(cleanupRestore(t, tomb))

		// do not quit the loop: we must perform all cleanups anyway
	}

	// Also, do not return an error here: we don't want a failed cleanup to
	// trigger an undo of the restore operation
	return nil
}

func cleanupRestore(task *state.Task, _ *tomb.Tomb) error {
	var restoreState backend.RestoreState

	st := task.State()
	st.Lock()
	status := task.Status()
	mylog.Check(task.Get("restore-state", &restoreState))
	st.Unlock()

	if status != state.DoneStatus {
		// only need to clean up restores that worked
		return nil
	}

	// this is bad: we somehow lost the information to restore things
	// but if we return the error we'll just get called again :-(
	// TODO: use warnings :-)

	backendCleanup(&restoreState)

	return nil
}

func doCheck(task *state.Task, tomb *tomb.Tomb) error {
	var snapshot snapshotSetup

	st := task.State()
	st.Lock()
	mylog.Check(task.Get("snapshot-setup", &snapshot))
	st.Unlock()

	reader := mylog.Check2(backendOpen(snapshot.Filename, backend.ExtractFnameSetID))

	defer reader.Close()

	return backendCheck(reader, tomb.Context(nil), snapshot.Users)
}

func doForget(task *state.Task, _ *tomb.Tomb) error {
	// note this is also undoSave
	st := task.State()
	st.Lock()
	defer st.Unlock()

	var snapshot snapshotSetup
	mylog.Check(task.Get("snapshot-setup", &snapshot))

	if snapshot.Filename == "" {
		return fmt.Errorf("internal error: task %s (%s) snapshot info is missing the filename", task.ID(), task.Kind())
	}
	mylog.Check(

		// in case it's an automatic snapshot, remove the set also from the state (automatic snapshots have just one snap per set).
		removeSnapshotState(st, snapshot.SetID))

	return osRemove(snapshot.Filename)
}

func delayedCrossMgrInit() {
	// hook automatic snapshots into snapstate logic
	snapstate.AutomaticSnapshot = AutomaticSnapshot
	snapstate.AutomaticSnapshotExpiration = AutomaticSnapshotExpiration
	snapstate.EstimateSnapshotSize = EstimateSnapshotSize
}

func MockBackendSave(f func(context.Context, uint64, *snap.Info, map[string]interface{}, []string, *snap.SnapshotOptions, *dirs.SnapDirOptions) (*client.Snapshot, error)) (restore func()) {
	old := backendSave
	backendSave = f
	return func() {
		backendSave = old
	}
}
