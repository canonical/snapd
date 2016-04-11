// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

// Package snapstate implements the manager and state aspects responsible for the installation and removal of snaps.
package snapstate

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"

	"gopkg.in/tomb.v2"

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/overlord/state"
	"github.com/ubuntu-core/snappy/snap"
	"github.com/ubuntu-core/snappy/snappy"
)

// SnapManager is responsible for the installation and removal of snaps.
type SnapManager struct {
	state   *state.State
	backend managerBackend

	runner *state.TaskRunner
}

// SnapSetup holds the necessary snap details to perform most snap manager tasks.
type SnapSetup struct {
	Name      string `json:"name"`
	Developer string `json:"developer,omitempty"`
	Revision  int    `json:"revision,omitempty"`
	Channel   string `json:"channel,omitempty"`

	// XXX: should be switched to use Revision instead
	RollbackVersion string `json:"rollback-version,omitempty"`

	Flags int `json:"flags,omitempty"`

	SnapPath string `json:"snap-path,omitempty"`
}

// snapState holds the state for a snap installed in the system.
type snapState struct {
	Sequence  []*snap.SideInfo `json:"sequence"` // Last is current
	Candidate *snap.SideInfo   `josn:"candidate,omitempty"`
	Active    bool             `json:"active,omitempty"`
	Channel   string           `json:"channel,omitempty"`
	DevMode   bool             `json:"dev-mode,omitempty"`
}

func (ss *SnapSetup) placeInfo() snap.PlaceInfo {
	return snap.MinimalPlaceInfo(ss.Name, ss.Revision)
}

func (ss *SnapSetup) MountDir() string {
	return ss.placeInfo().MountDir()
}

// Manager returns a new snap manager.
func Manager(s *state.State) (*SnapManager, error) {
	runner := state.NewTaskRunner(s)
	backend := &defaultBackend{}
	m := &SnapManager{
		state:   s,
		backend: backend,
		runner:  runner,
	}

	// this handler does nothing
	runner.AddHandler("nop", func(t *state.Task, _ *tomb.Tomb) error {
		return nil
	}, nil)

	// install/update releated
	runner.AddHandler("prepare-snap", m.doPrepareSnap, m.undoPrepareSnap)
	runner.AddHandler("download-snap", m.doDownloadSnap, m.undoPrepareSnap)
	runner.AddHandler("mount-snap", m.doMountSnap, m.undoMountSnap)
	runner.AddHandler("copy-snap-data", m.doCopySnapData, m.undoCopySnapData)
	runner.AddHandler("link-snap", m.doLinkSnap, m.undoLinkSnap)
	// FIXME: port to native tasks and rename
	//runner.AddHandler("garbage-collect", m.doGarbageCollect, nil)

	// remove releated
	runner.AddHandler("unlink-snap", m.doUnlinkSnap, nil)
	runner.AddHandler("remove-snap-files", m.doRemoveSnapFiles, nil)
	runner.AddHandler("remove-snap-data", m.doRemoveSnapData, nil)

	// FIXME: work on those
	runner.AddHandler("rollback-snap", m.doRollbackSnap, nil)
	runner.AddHandler("activate-snap", m.doActivateSnap, nil)
	runner.AddHandler("deactivate-snap", m.doDeactivateSnap, nil)

	// test handlers
	runner.AddHandler("fake-install-snap", func(t *state.Task, _ *tomb.Tomb) error {
		return nil
	}, nil)
	runner.AddHandler("fake-install-snap-error", func(t *state.Task, _ *tomb.Tomb) error {
		return fmt.Errorf("fake-install-snap-error errored")
	}, nil)

	return m, nil
}

func (m *SnapManager) doPrepareSnap(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	ss, snapst, err := snapSetupAndState(t)
	st.Unlock()
	if err != nil {
		return err
	}
	st.Lock()
	snapst.Candidate = &snap.SideInfo{}
	setSnapState(st, ss.Name, snapst)
	st.Unlock()
	return nil
}

func (m *SnapManager) undoPrepareSnap(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	ss, snapst, err := snapSetupAndState(t)
	st.Unlock()
	if err != nil {
		return err
	}
	snapst.Candidate = nil
	setSnapState(st, ss.Name, snapst)
	return nil
}

func (m *SnapManager) doDownloadSnap(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	ss, snapst, err := snapSetupAndState(t)
	st.Unlock()
	if err != nil {
		return err
	}

	// construct the store name
	name := ss.Name
	if ss.Developer != "" {
		name = fmt.Sprintf("%s.%s", ss.Name, ss.Developer)
	}
	pb := &TaskProgressAdapter{task: t}
	storeInfo, downloadedSnapFile, err := m.backend.Download(name, ss.Channel, pb)
	if err != nil {
		return err
	}
	ss.SnapPath = downloadedSnapFile
	ss.Revision = storeInfo.Revision

	// update the snap setup and state for the follow up tasks
	st.Lock()
	t.Set("snap-setup", ss)
	snapst.Candidate = &storeInfo.SideInfo
	setSnapState(st, ss.Name, snapst)
	st.Unlock()

	return nil
}

func (m *SnapManager) doUnlinkSnap(t *state.Task, _ *tomb.Tomb) error {
	var ss SnapSetup

	t.State().Lock()
	err := t.Get("snap-setup", &ss)
	t.State().Unlock()
	if err != nil {
		return err
	}

	pb := &TaskProgressAdapter{task: t}
	return m.backend.UnlinkSnap(ss.MountDir(), pb)
}

func (m *SnapManager) doRemoveSnapFiles(t *state.Task, _ *tomb.Tomb) error {
	t.State().Lock()
	ss, err := TaskSnapSetup(t)
	t.State().Unlock()
	if err != nil {
		return err
	}

	pb := &TaskProgressAdapter{task: t}
	return m.backend.RemoveSnapFiles(ss.placeInfo(), pb)
}

func (m *SnapManager) doRemoveSnapData(t *state.Task, _ *tomb.Tomb) error {
	t.State().Lock()
	ss, err := TaskSnapSetup(t)
	t.State().Unlock()
	if err != nil {
		return err
	}

	return m.backend.RemoveSnapData(ss.Name, ss.Revision)
}

func (m *SnapManager) doRollbackSnap(t *state.Task, _ *tomb.Tomb) error {
	var ss SnapSetup

	t.State().Lock()
	err := t.Get("snap-setup", &ss)
	t.State().Unlock()
	if err != nil {
		return err
	}

	pb := &TaskProgressAdapter{task: t}
	_, err = m.backend.Rollback(ss.Name, ss.RollbackVersion, pb)
	return err
}

func (m *SnapManager) doActivateSnap(t *state.Task, _ *tomb.Tomb) error {
	var ss SnapSetup

	t.State().Lock()
	err := t.Get("snap-setup", &ss)
	t.State().Unlock()
	if err != nil {
		return err
	}

	pb := &TaskProgressAdapter{task: t}
	return m.backend.Activate(ss.Name, true, pb)
}

func (m *SnapManager) doDeactivateSnap(t *state.Task, _ *tomb.Tomb) error {
	var ss SnapSetup

	t.State().Lock()
	err := t.Get("snap-setup", &ss)
	t.State().Unlock()
	if err != nil {
		return err
	}

	pb := &TaskProgressAdapter{task: t}
	return m.backend.Activate(ss.Name, false, pb)
}

// Ensure implements StateManager.Ensure.
func (m *SnapManager) Ensure() error {
	m.runner.Ensure()
	return nil
}

// Wait implements StateManager.Wait.
func (m *SnapManager) Wait() {
	m.runner.Wait()
}

// Stop implements StateManager.Stop.
func (m *SnapManager) Stop() {
	m.runner.Stop()
}

func TaskSnapSetup(t *state.Task) (*SnapSetup, error) {
	var ss SnapSetup

	err := t.Get("snap-setup", &ss)
	if err != nil && err != state.ErrNoState {
		return nil, err
	}
	if err == nil {
		return &ss, nil
	}

	var id string
	err = t.Get("snap-setup-task", &id)
	if err != nil {
		return nil, err
	}

	ts := t.State().Task(id)
	if err := ts.Get("snap-setup", &ss); err != nil {
		return nil, err
	}
	return &ss, nil
}

func snapSetupAndState(t *state.Task) (*SnapSetup, *snapState, error) {
	ss, err := TaskSnapSetup(t)
	if err != nil {
		return nil, nil, err
	}
	var snapst snapState
	err = getSnapState(t.State(), ss.Name, &snapst)
	if err != nil && err != state.ErrNoState {
		return nil, nil, err
	}
	return ss, &snapst, nil
}

func getSnapState(s *state.State, name string, snapst *snapState) error {
	var snaps map[string]*json.RawMessage
	err := s.Get("snaps", &snaps)
	if err != nil {
		return err
	}
	raw, ok := snaps[name]
	if !ok {
		return state.ErrNoState
	}
	err = json.Unmarshal([]byte(*raw), &snapst)
	if err != nil {
		return fmt.Errorf("cannot unmarshal snap state: %v", err)
	}
	return nil
}

func setSnapState(s *state.State, name string, snapst *snapState) {
	var snaps map[string]*json.RawMessage
	err := s.Get("snaps", &snaps)
	if err == state.ErrNoState {
		s.Set("snaps", map[string]*snapState{name: snapst})
		return
	}
	if err != nil {
		panic("internal error: cannot unmarshal snaps state: " + err.Error())
	}
	data, err := json.Marshal(snapst)
	if err != nil {
		panic("internal error: cannot marshal snap state: " + err.Error())
	}
	raw := json.RawMessage(data)
	snaps[name] = &raw
	s.Set("snaps", snaps)
}

func (m *SnapManager) undoMountSnap(t *state.Task, _ *tomb.Tomb) error {
	t.State().Lock()
	ss, err := TaskSnapSetup(t)
	t.State().Unlock()
	if err != nil {
		return err
	}

	return m.backend.UndoSetupSnap(ss.placeInfo())
}

func (m *SnapManager) doMountSnap(t *state.Task, _ *tomb.Tomb) error {
	t.State().Lock()
	ss, snapst, err := snapSetupAndState(t)
	t.State().Unlock()
	if err != nil {
		return err
	}

	if err := m.backend.CheckSnap(ss.SnapPath, ss.Flags); err != nil {
		return err
	}

	// TODO Use ss.Revision to obtain the right info to mount
	//      instead of assuming the candidate is the right one.
	return m.backend.SetupSnap(ss.SnapPath, snapst.Candidate, ss.Flags)
}

func (m *SnapManager) undoCopySnapData(t *state.Task, _ *tomb.Tomb) error {
	t.State().Lock()
	ss, err := TaskSnapSetup(t)
	t.State().Unlock()
	if err != nil {
		return err
	}

	return m.backend.UndoCopySnapData(ss.MountDir(), ss.Flags)
}

func (m *SnapManager) doCopySnapData(t *state.Task, _ *tomb.Tomb) error {
	t.State().Lock()
	ss, err := TaskSnapSetup(t)
	t.State().Unlock()
	if err != nil {
		return err
	}

	return m.backend.CopySnapData(ss.MountDir(), ss.Flags)
}

func (m *SnapManager) doLinkSnap(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()

	// Hold the lock for the full duration of the task here so
	// nobody observes a world where the state engine and
	// the file system are reporting different things.
	st.Lock()
	defer st.Unlock()

	ss, snapst, err := snapSetupAndState(t)
	if err != nil {
		return err
	}
	m.backend.Candidate(snapst.Candidate)
	snapst.Sequence = append(snapst.Sequence, snapst.Candidate)
	snapst.Candidate = nil
	snapst.Active = true

	err = m.backend.LinkSnap(ss.MountDir())
	if err != nil {
		return err
	}

	// Do at the end so we only preserve the new state if it worked.
	setSnapState(st, ss.Name, snapst)
	return nil
}

func (m *SnapManager) undoLinkSnap(t *state.Task, _ *tomb.Tomb) error {
	t.State().Lock()
	ss, snapst, err := snapSetupAndState(t)
	t.State().Unlock()
	if err != nil {
		return err
	}

	// No need to undo "snaps" in state here. The only chance of
	// having the new state there is a working doLinkSnap call.
	newDir := ss.MountDir()
	oldDir := ""
	if len(snapst.Sequence) > 0 {
		latest := snapst.Sequence[len(snapst.Sequence)-1]
		oldDir = snap.MinimalPlaceInfo(ss.Name, latest.Revision).MountDir()
	}
	return m.backend.UndoLinkSnap(oldDir, newDir)
}

func (m *SnapManager) doGarbageCollect(t *state.Task, _ *tomb.Tomb) error {
	t.State().Lock()
	ss, err := TaskSnapSetup(t)
	t.State().Unlock()
	if err != nil {
		return err
	}

	pb := &TaskProgressAdapter{task: t}
	return m.backend.GarbageCollect(ss.Name, ss.Flags, pb)
}

// SnapInfo returns the snap.Info for a snap in the system.
//
// Today this function is looking at data directly from the mounted snap, but soon it will
// be changed so it looks first at the state for the snap details (Revision, Developer, etc),
// and then complements it with information from the snap itself.
func SnapInfo(state *state.State, name string, revision int) (*snap.Info, error) {
	fname := filepath.Join(dirs.SnapSnapsDir, name, strconv.Itoa(revision), "meta", "snap.yaml")
	// XXX: This hacky and should not be needed.
	sn, err := snappy.NewInstalledSnap(fname)
	if err != nil {
		return nil, err
	}
	return sn.Info(), nil
}
