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
	"fmt"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snappy"
	"github.com/snapcore/snapd/store"
)

// SnapManager is responsible for the installation and removal of snaps.
type SnapManager struct {
	state   *state.State
	backend managerBackend

	runner *state.TaskRunner
}

// SnapSetup holds the necessary snap details to perform most snap manager tasks.
type SnapSetup struct {
	Name     string        `json:"name"`
	Revision snap.Revision `json:"revision,omitempty"`
	Channel  string        `json:"channel,omitempty"`
	UserID   int           `json:"user-id,omitempty"`

	Flags int `json:"flags,omitempty"`

	SnapPath string `json:"snap-path,omitempty"`
}

func (ss *SnapSetup) placeInfo() snap.PlaceInfo {
	return snap.MinimalPlaceInfo(ss.Name, ss.Revision)
}

func (ss *SnapSetup) MountDir() string {
	return snap.MountDir(ss.Name, ss.Revision)
}

func (ss *SnapSetup) DevMode() bool {
	return ss.Flags&int(snappy.DeveloperMode) != 0
}

func (ss *SnapSetup) TryMode() bool {
	return ss.Flags&int(snappy.TryMode) != 0
}

// SnapStateFlags are flags stored in SnapState.
type SnapStateFlags int

const (
	// DevMode switches confinement to non-enforcing mode.
	DevMode = 1 << iota
	TryMode
)

// SnapState holds the state for a snap installed in the system.
type SnapState struct {
	Sequence  []*snap.SideInfo `json:"sequence"` // Last is current
	Candidate *snap.SideInfo   `json:"candidate,omitempty"`
	Active    bool             `json:"active,omitempty"`
	Channel   string           `json:"channel,omitempty"`
	Flags     SnapStateFlags   `json:"flags,omitempty"`
	// incremented revision used for local installs
	LocalRevision snap.Revision `json:"local-revision,omitempty"`
}

// Current returns the side info for the current revision in the snap revision sequence if there is one.
func (snapst *SnapState) Current() *snap.SideInfo {
	n := len(snapst.Sequence)
	if n == 0 {
		return nil
	}
	return snapst.Sequence[n-1]
}

// DevMode returns true if the snap is installed in developer mode.
func (snapst *SnapState) DevMode() bool {
	return snapst.Flags&DevMode != 0
}

// TryMode returns true if the snap is installed in `try` mode as an
// unpacked directory.
func (snapst *SnapState) TryMode() bool {
	return snapst.Flags&TryMode != 0
}

// SetTryMode sets/clears the TryMode flag in the SnapState
func (snapst *SnapState) SetTryMode(active bool) {
	if active {
		snapst.Flags |= TryMode
	} else {
		snapst.Flags &= ^TryMode
	}
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

	// install/update related
	runner.AddHandler("prepare-snap", m.doPrepareSnap, m.undoPrepareSnap)
	runner.AddHandler("download-snap", m.doDownloadSnap, m.undoPrepareSnap)
	runner.AddHandler("mount-snap", m.doMountSnap, m.undoMountSnap)
	runner.AddHandler("unlink-current-snap", m.doUnlinkCurrentSnap, m.undoUnlinkCurrentSnap)
	runner.AddHandler("copy-snap-data", m.doCopySnapData, m.undoCopySnapData)
	runner.AddHandler("link-snap", m.doLinkSnap, m.undoLinkSnap)
	// FIXME: port to native tasks and rename
	//runner.AddHandler("garbage-collect", m.doGarbageCollect, nil)

	// remove related
	runner.AddHandler("unlink-snap", m.doUnlinkSnap, nil)
	runner.AddHandler("clear-snap", m.doClearSnapData, nil)
	runner.AddHandler("discard-snap", m.doDiscardSnap, nil)

	// test handlers
	runner.AddHandler("fake-install-snap", func(t *state.Task, _ *tomb.Tomb) error {
		return nil
	}, nil)
	runner.AddHandler("fake-install-snap-error", func(t *state.Task, _ *tomb.Tomb) error {
		return fmt.Errorf("fake-install-snap-error errored")
	}, nil)

	return m, nil
}

func checkRevisionIsNew(name string, snapst *SnapState, revision snap.Revision) error {
	for _, si := range snapst.Sequence {
		if si.Revision == revision {
			return fmt.Errorf("revision %s of snap %q already installed", revision, name)
		}
	}
	return nil
}

func (m *SnapManager) doPrepareSnap(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	ss, snapst, err := snapSetupAndState(t)
	st.Unlock()
	if err != nil {
		return err
	}

	if ss.Revision.Unset() {
		// Local revisions start at -1 and go down.
		revision := snapst.LocalRevision
		if revision.Unset() {
			revision = snap.R(-1)
		} else {
			revision.N--
		}
		if !revision.Local() {
			panic("internal error: invalid local revision built: " + revision.String())
		}
		snapst.LocalRevision = revision
		ss.Revision = revision
	} else {
		if err := checkRevisionIsNew(ss.Name, snapst, ss.Revision); err != nil {
			return err
		}
	}

	st.Lock()
	t.Set("snap-setup", ss)
	snapst.Candidate = &snap.SideInfo{Revision: ss.Revision}
	Set(st, ss.Name, snapst)
	st.Unlock()
	return nil
}

func (m *SnapManager) undoPrepareSnap(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	ss, snapst, err := snapSetupAndState(t)
	if err != nil {
		return err
	}
	snapst.Candidate = nil
	Set(st, ss.Name, snapst)
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

	checker := func(info *snap.Info) error {
		return checkRevisionIsNew(ss.Name, snapst, info.Revision)
	}

	pb := &TaskProgressAdapter{task: t}

	var auther store.Authenticator
	if ss.UserID > 0 {
		st.Lock()
		user, err := auth.User(st, ss.UserID)
		st.Unlock()
		if err != nil {
			return err
		}
		auther = user.Authenticator()
	}

	storeInfo, downloadedSnapFile, err := m.backend.Download(ss.Name, ss.Channel, checker, pb, auther)
	if err != nil {
		return err
	}
	ss.SnapPath = downloadedSnapFile
	ss.Revision = storeInfo.Revision

	// update the snap setup and state for the follow up tasks
	st.Lock()
	t.Set("snap-setup", ss)
	snapst.Candidate = &storeInfo.SideInfo
	Set(st, ss.Name, snapst)
	st.Unlock()

	return nil
}

func (m *SnapManager) doUnlinkSnap(t *state.Task, _ *tomb.Tomb) error {
	// invoked only if snap has a current active revision

	st := t.State()

	st.Lock()
	defer st.Unlock()

	ss, snapst, err := snapSetupAndState(t)
	if err != nil {
		return err
	}

	info, err := Info(t.State(), ss.Name, ss.Revision)
	if err != nil {
		return err
	}

	pb := &TaskProgressAdapter{task: t}
	st.Unlock() // pb itself will ask for locking
	err = m.backend.UnlinkSnap(info, pb)
	st.Lock()
	if err != nil {
		return err
	}

	// mark as inactive
	snapst.Active = false
	Set(st, ss.Name, snapst)
	return nil
}

func (m *SnapManager) doClearSnapData(t *state.Task, _ *tomb.Tomb) error {
	t.State().Lock()
	ss, snapst, err := snapSetupAndState(t)
	t.State().Unlock()
	if err != nil {
		return err
	}

	t.State().Lock()
	info, err := Info(t.State(), ss.Name, ss.Revision)
	t.State().Unlock()
	if err != nil {
		return err
	}

	if err = m.backend.RemoveSnapData(info); err != nil {
		return err
	}

	// Only remove data common between versions if this is the last version
	if len(snapst.Sequence) == 1 {
		if err = m.backend.RemoveSnapCommonData(info); err != nil {
			return err
		}
	}

	return nil
}

func (m *SnapManager) doDiscardSnap(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()

	st.Lock()
	ss, snapst, err := snapSetupAndState(t)
	st.Unlock()
	if err != nil {
		return err
	}

	if len(snapst.Sequence) == 1 {
		snapst.Sequence = nil
	} else {
		newSeq := make([]*snap.SideInfo, 0, len(snapst.Sequence))
		for _, si := range snapst.Sequence {
			if si.Revision == ss.Revision {
				// leave out
				continue
			}
			newSeq = append(newSeq, si)
		}
		snapst.Sequence = newSeq
	}

	pb := &TaskProgressAdapter{task: t}
	err = m.backend.RemoveSnapFiles(ss.placeInfo(), pb)
	if err != nil {
		st.Lock()
		t.Errorf("cannot remove snap file %q, will retry: %s", ss.Name, err)
		st.Unlock()
		return state.Retry
	}

	st.Lock()
	Set(st, ss.Name, snapst)
	st.Unlock()
	return nil
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

func snapSetupAndState(t *state.Task) (*SnapSetup, *SnapState, error) {
	ss, err := TaskSnapSetup(t)
	if err != nil {
		return nil, nil, err
	}
	var snapst SnapState
	err = Get(t.State(), ss.Name, &snapst)
	if err != nil && err != state.ErrNoState {
		return nil, nil, err
	}
	return ss, &snapst, nil
}

func (m *SnapManager) undoMountSnap(t *state.Task, _ *tomb.Tomb) error {
	t.State().Lock()
	ss, _, err := snapSetupAndState(t)
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

	var curInfo *snap.Info
	if cur := snapst.Current(); cur != nil {
		var err error
		curInfo, err = readInfo(ss.Name, cur)
		if err != nil {
			return err
		}

	}

	m.backend.Current(curInfo)

	if err := checkSnap(t.State(), ss.SnapPath, curInfo, snappy.InstallFlags(ss.Flags)); err != nil {
		return err
	}

	// TODO Use ss.Revision to obtain the right info to mount
	//      instead of assuming the candidate is the right one.
	return m.backend.SetupSnap(ss.SnapPath, snapst.Candidate, ss.Flags)
}

func (m *SnapManager) undoUnlinkCurrentSnap(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()

	st.Lock()
	defer st.Unlock()

	ss, snapst, err := snapSetupAndState(t)
	if err != nil {
		return err
	}

	oldInfo, err := readInfo(ss.Name, snapst.Current())
	if err != nil {
		return err
	}

	snapst.Active = true
	st.Unlock()
	err = m.backend.LinkSnap(oldInfo)
	st.Lock()
	if err != nil {
		return err
	}

	// mark as active again
	Set(st, ss.Name, snapst)
	return nil

}

func (m *SnapManager) doUnlinkCurrentSnap(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()

	st.Lock()
	defer st.Unlock()

	ss, snapst, err := snapSetupAndState(t)
	if err != nil {
		return err
	}

	oldInfo, err := readInfo(ss.Name, snapst.Current())
	if err != nil {
		return err
	}

	snapst.Active = false

	pb := &TaskProgressAdapter{task: t}
	st.Unlock() // pb itself will ask for locking
	err = m.backend.UnlinkSnap(oldInfo, pb)
	st.Lock()
	if err != nil {
		return err
	}

	// mark as inactive
	Set(st, ss.Name, snapst)
	return nil
}

func (m *SnapManager) undoCopySnapData(t *state.Task, _ *tomb.Tomb) error {
	t.State().Lock()
	ss, snapst, err := snapSetupAndState(t)
	t.State().Unlock()
	if err != nil {
		return err
	}

	newInfo, err := readInfo(ss.Name, snapst.Candidate)
	if err != nil {
		return err
	}

	return m.backend.UndoCopySnapData(newInfo, ss.Flags)
}

func (m *SnapManager) doCopySnapData(t *state.Task, _ *tomb.Tomb) error {
	t.State().Lock()
	ss, snapst, err := snapSetupAndState(t)
	t.State().Unlock()
	if err != nil {
		return err
	}

	newInfo, err := readInfo(ss.Name, snapst.Candidate)
	if err != nil {
		return err
	}

	var oldInfo *snap.Info
	if cur := snapst.Current(); cur != nil {
		var err error
		oldInfo, err = readInfo(ss.Name, cur)
		if err != nil {
			return err
		}

	}

	return m.backend.CopySnapData(newInfo, oldInfo, ss.Flags)
}

func (m *SnapManager) doLinkSnap(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()

	st.Lock()
	defer st.Unlock()

	ss, snapst, err := snapSetupAndState(t)
	if err != nil {
		return err
	}

	cand := snapst.Candidate

	m.backend.Candidate(snapst.Candidate)
	snapst.Sequence = append(snapst.Sequence, snapst.Candidate)
	snapst.Candidate = nil
	snapst.Active = true
	oldChannel := snapst.Channel
	if ss.Channel != "" {
		snapst.Channel = ss.Channel
	}
	oldTryMode := snapst.TryMode()
	snapst.SetTryMode(ss.TryMode())

	newInfo, err := readInfo(ss.Name, cand)
	if err != nil {
		return err
	}

	st.Unlock()
	// XXX: this block is slightly ugly, find a pattern when we have more examples
	err = m.backend.LinkSnap(newInfo)
	if err != nil {
		pb := &TaskProgressAdapter{task: t}
		err := m.backend.UnlinkSnap(newInfo, pb)
		if err != nil {
			st.Lock()
			t.Errorf("cannot cleanup failed attempt at making snap %q available to the system: %v", ss.Name, err)
			st.Unlock()
		}
	}
	st.Lock()
	if err != nil {
		return err
	}

	// save for undoLinkSnap
	t.Set("old-trymode", oldTryMode)
	t.Set("old-channel", oldChannel)
	// Do at the end so we only preserve the new state if it worked.
	Set(st, ss.Name, snapst)
	// Make sure if state commits and snapst is mutated we won't be rerun
	t.SetStatus(state.DoneStatus)
	return nil
}

func (m *SnapManager) undoLinkSnap(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()

	st.Lock()
	defer st.Unlock()

	ss, snapst, err := snapSetupAndState(t)
	if err != nil {
		return err
	}

	var oldChannel string
	err = t.Get("old-channel", &oldChannel)
	if err != nil {
		return err
	}
	var oldTryMode bool
	err = t.Get("old-trymode", &oldTryMode)
	if err != nil {
		return err
	}

	// relinking of the old snap is done in the undo of unlink-current-snap

	snapst.Candidate = snapst.Sequence[len(snapst.Sequence)-1]
	snapst.Sequence = snapst.Sequence[:len(snapst.Sequence)-1]
	snapst.Active = false
	snapst.Channel = oldChannel
	snapst.SetTryMode(oldTryMode)

	newInfo, err := readInfo(ss.Name, snapst.Candidate)
	if err != nil {
		return err
	}

	pb := &TaskProgressAdapter{task: t}
	st.Unlock() // pb itself will ask for locking
	err = m.backend.UnlinkSnap(newInfo, pb)
	st.Lock()
	if err != nil {
		return err
	}

	// mark as inactive
	Set(st, ss.Name, snapst)
	// Make sure if state commits and snapst is mutated we won't be rerun
	t.SetStatus(state.UndoneStatus)
	return nil
}
