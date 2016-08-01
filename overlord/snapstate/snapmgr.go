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
	"errors"
	"fmt"
	"os"
	"time"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/snapstate/backend"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store"
)

// SnapManager is responsible for the installation and removal of snaps.
type SnapManager struct {
	state   *state.State
	backend managerBackend

	runner *state.TaskRunner
}

// SnapSetupFlags are flags stored in SnapSetup to control snap manager tasks.
type SnapSetupFlags Flags

// SnapSetup holds the necessary snap details to perform most snap manager tasks.
type SnapSetup struct {
	// FIXME: rename to RequestedChannel to convey the meaning better
	Channel string `json:"channel,omitempty"`
	UserID  int    `json:"user-id,omitempty"`

	Flags SnapSetupFlags `json:"flags,omitempty"`

	SnapPath string `json:"snap-path,omitempty"`

	DownloadInfo *snap.DownloadInfo `json:"download-info,omitempty"`
	SideInfo     *snap.SideInfo     `json:"side-info,omitempty"`
}

func (ss *SnapSetup) Name() string {
	if ss.SideInfo.RealName == "" {
		panic("SnapSetup.SideInfo.RealName not set")
	}
	return ss.SideInfo.RealName
}

func (ss *SnapSetup) Revision() snap.Revision {
	return ss.SideInfo.Revision
}

func (ss *SnapSetup) placeInfo() snap.PlaceInfo {
	return snap.MinimalPlaceInfo(ss.Name(), ss.Revision())
}

func (ss *SnapSetup) MountDir() string {
	return snap.MountDir(ss.Name(), ss.Revision())
}

// DevMode returns true if the snap is being installed in developer mode.
func (ss *SnapSetup) DevMode() bool {
	return Flags(ss.Flags).DevMode()
}

func (ss *SnapSetup) JailMode() bool {
	return Flags(ss.Flags).JailMode()
}

func (ss *SnapSetup) DevModeAllowed() bool {
	return Flags(ss.Flags).DevModeAllowed()
}

// TryMode returns true if the snap is being installed in try mode directly from a directory.
func (ss *SnapSetup) TryMode() bool {
	return ss.Flags&TryMode != 0
}

// SnapStateFlags are flags stored in SnapState.
type SnapStateFlags Flags

// SnapState holds the state for a snap installed in the system.
type SnapState struct {
	SnapType string           `json:"type"` // Use Type and SetType
	Sequence []*snap.SideInfo `json:"sequence"`
	Active   bool             `json:"active,omitempty"`
	// Current indicates the current active revision if Active is
	// true or the last active revision if Active is false
	// (usually while a snap is being operated on or disabled)
	Current snap.Revision  `json:"current"`
	Channel string         `json:"channel,omitempty"`
	Flags   SnapStateFlags `json:"flags,omitempty"`
}

// Type returns the type of the snap or an error.
// Should never error if Current is not nil.
func (snapst *SnapState) Type() (snap.Type, error) {
	if snapst.SnapType == "" {
		return snap.Type(""), fmt.Errorf("snap type unset")
	}
	return snap.Type(snapst.SnapType), nil
}

// SetType records the type of the snap.
func (snapst *SnapState) SetType(typ snap.Type) {
	snapst.SnapType = string(typ)
}

// HasCurrent returns whether snapst.Current is set.
func (snapst *SnapState) HasCurrent() bool {
	if snapst.Current.Unset() {
		if len(snapst.Sequence) > 0 {
			panic(fmt.Sprintf("snapst.Current and snapst.Sequence out of sync: %#v %#v", snapst.Current, snapst.Sequence))
		}

		return false
	}
	return true
}

// LocalRevision returns the "latest" local revision. Local revisions
// start at -1 and are counted down.
func (snapst *SnapState) LocalRevision() snap.Revision {
	var local snap.Revision
	for _, si := range snapst.Sequence {
		if si.Revision.Local() && si.Revision.N < local.N {
			local = si.Revision
		}
	}
	return local
}

// TODO: unexport CurrentSideInfo and HasCurrent?

// CurrentSideInfo returns the side info for the revision indicated by snapst.Current in the snap revision sequence if there is one.
func (snapst *SnapState) CurrentSideInfo() *snap.SideInfo {
	if !snapst.HasCurrent() {
		return nil
	}
	seq := snapst.Sequence
	for i := len(seq) - 1; i >= 0; i-- {
		if seq[i].Revision == snapst.Current {
			return seq[i]
		}
	}
	panic("cannot find snapst.Current in the snapst.Sequence")
}

func (snapst *SnapState) previousSideInfo() *snap.SideInfo {
	if !snapst.HasCurrent() {
		return nil
	}
	n := len(snapst.Sequence)
	if n < 2 {
		return nil
	}
	// find "current" and return the one before that
	currentIndex := snapst.findIndex(snapst.Current)
	if currentIndex == 0 {
		return nil
	}
	return snapst.Sequence[currentIndex-1]
}

// findIndex returns the index of the given revision rev in the
// snapst.Sequence
func (snapst *SnapState) findIndex(rev snap.Revision) int {
	for i, si := range snapst.Sequence {
		if si.Revision == rev {
			return i
		}
	}
	return -1
}

// Block returns revisions that should be blocked on refreshes,
// computed from Sequence[currentRevisionIndex+1:].
func (snapst *SnapState) Block() []snap.Revision {
	// return revisions from Sequence[currentIndex:]
	currentIndex := snapst.findIndex(snapst.Current)
	if currentIndex < 0 || currentIndex+1 == len(snapst.Sequence) {
		return nil
	}
	out := make([]snap.Revision, len(snapst.Sequence)-currentIndex-1)
	for i, si := range snapst.Sequence[currentIndex+1:] {
		out[i] = si.Revision
	}
	return out
}

var ErrNoCurrent = errors.New("snap has no current revision")

// Retrieval functions
var readInfo = readInfoAnyway

func readInfoAnyway(name string, si *snap.SideInfo) (*snap.Info, error) {
	info, err := snap.ReadInfo(name, si)
	if _, ok := err.(*snap.NotFoundError); ok {
		reason := fmt.Sprintf("cannot read snap %q: %s", name, err)
		info := &snap.Info{SuggestedName: name, Broken: reason}
		if si != nil {
			info.SideInfo = *si
		}
		return info, nil
	}
	return info, err
}

// CurrentInfo returns the information about the current active revision or the last active revision (if the snap is inactive). It returns the ErrNoCurrent error if snapst.Current is unset.
func (snapst *SnapState) CurrentInfo() (*snap.Info, error) {
	cur := snapst.CurrentSideInfo()
	if cur == nil {
		return nil, ErrNoCurrent
	}
	return readInfo(cur.RealName, cur)
}

// DevMode returns true if the snap is installed in developer mode.
func (snapst *SnapState) DevMode() bool {
	return Flags(snapst.Flags).DevMode()
}

// SetDevMode sets/clears the DevMode flag in the SnapState.
func (snapst *SnapState) SetDevMode(active bool) {
	if active {
		snapst.Flags |= DevMode
	} else {
		snapst.Flags &= ^DevMode
	}
}

func (snapst *SnapState) JailMode() bool {
	return Flags(snapst.Flags).JailMode()
}

// SetJailMode sets/clears the JailMode flag in the SnapState.
func (snapst *SnapState) SetJailMode(active bool) {
	if active {
		snapst.Flags |= JailMode
	} else {
		snapst.Flags &= ^JailMode
	}
}

func (snapst *SnapState) DevModeAllowed() bool {
	return Flags(snapst.Flags).DevModeAllowed()
}

// TryMode returns true if the snap is installed in `try` mode as an
// unpacked directory.
func (snapst *SnapState) TryMode() bool {
	return snapst.Flags&TryMode != 0
}

// SetTryMode sets/clears the TryMode flag in the SnapState.
func (snapst *SnapState) SetTryMode(active bool) {
	if active {
		snapst.Flags |= TryMode
	} else {
		snapst.Flags &= ^TryMode
	}
}

func userFromUserID(st *state.State, userID int) (*auth.UserState, error) {
	if userID == 0 {
		return nil, nil
	}
	return auth.User(st, userID)
}

func updateInfo(st *state.State, snapst *SnapState, channel string, userID int, flags Flags) (*snap.Info, error) {
	user, err := userFromUserID(st, userID)
	if err != nil {
		return nil, err
	}
	curInfo, err := snapst.CurrentInfo()
	if err != nil {
		return nil, err
	}

	if curInfo.SnapID == "" { // covers also trymode
		return nil, fmt.Errorf("cannot refresh local snap %q", curInfo.Name())
	}

	refreshCand := &store.RefreshCandidate{
		// the desired channel
		Channel: channel,
		DevMode: flags.DevModeAllowed(),
		Block:   snapst.Block(),

		SnapID:   curInfo.SnapID,
		Revision: curInfo.Revision,
		Epoch:    curInfo.Epoch,
	}

	theStore := Store(st)
	st.Unlock() // calls to the store should be done without holding the state lock
	res, err := theStore.ListRefresh([]*store.RefreshCandidate{refreshCand}, user)
	st.Lock()
	if len(res) == 0 {
		return nil, fmt.Errorf("snap %q has no updates available", curInfo.Name())
	}
	return res[0], nil
}

func snapInfo(st *state.State, name, channel string, userID int, flags Flags) (*snap.Info, error) {
	user, err := userFromUserID(st, userID)
	if err != nil {
		return nil, err
	}
	theStore := Store(st)
	st.Unlock() // calls to the store should be done without holding the state lock
	snap, err := theStore.Snap(name, channel, flags.DevModeAllowed(), user)
	st.Lock()
	return snap, err
}

// Manager returns a new snap manager.
func Manager(s *state.State) (*SnapManager, error) {
	runner := state.NewTaskRunner(s)

	m := &SnapManager{
		state:   s,
		backend: backend.Backend{},
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

	// TODO: refresh-all needs logic at this level, to properly
	// handle the logic for that mode (e.g. skip snaps installed
	// with --devmode, set jailmode from snapstate).

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

type cachedStoreKey struct{}

// ReplaceStore replaces the store used by the manager.
func ReplaceStore(state *state.State, store StoreService) {
	state.Cache(cachedStoreKey{}, store)
}

func cachedStore(s *state.State) StoreService {
	ubuntuStore := s.Cached(cachedStoreKey{})
	if ubuntuStore == nil {
		return nil
	}
	return ubuntuStore.(StoreService)
}

// the store implementation has the interface consumed here
var _ StoreService = (*store.Store)(nil)

// Store returns the store service used by the snapstate package.
func Store(s *state.State) StoreService {
	if cachedStore := cachedStore(s); cachedStore != nil {
		return cachedStore
	}

	storeID := ""
	// TODO: set the store-id here from the model information
	if cand := os.Getenv("UBUNTU_STORE_ID"); cand != "" {
		storeID = cand
	}

	authContext := auth.NewAuthContext(s)
	s.Cache(cachedStoreKey{}, store.New(nil, storeID, authContext))
	return cachedStore(s)
}

func checkRevisionIsNew(name string, snapst *SnapState, revision snap.Revision) error {
	if revisionInSequence(snapst, revision) {
		return fmt.Errorf("revision %s of snap %q already installed", revision, name)
	}
	return nil
}

func revisionInSequence(snapst *SnapState, needle snap.Revision) bool {
	for _, si := range snapst.Sequence {
		if si.Revision == needle {
			return true
		}
	}
	return false
}

func (m *SnapManager) doPrepareSnap(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	ss, snapst, err := snapSetupAndState(t)
	st.Unlock()
	if err != nil {
		return err
	}

	if ss.Revision().Unset() {
		// Local revisions start at -1 and go down.
		revision := snapst.LocalRevision()
		if revision.Unset() || revision.N > 0 {
			revision = snap.R(-1)
		} else {
			revision.N--
		}
		if !revision.Local() {
			panic("internal error: invalid local revision built: " + revision.String())
		}
		ss.SideInfo.Revision = revision
	}

	st.Lock()
	t.Set("snap-setup", ss)
	st.Unlock()
	return nil
}

func (m *SnapManager) undoPrepareSnap(t *state.Task, _ *tomb.Tomb) error {
	// FIXME: remove the entire function
	return nil
}

func (m *SnapManager) doDownloadSnap(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	ss, err := TaskSnapSetup(t)
	st.Unlock()
	if err != nil {
		return err
	}

	meter := &TaskProgressAdapter{task: t}

	st.Lock()
	theStore := Store(st)
	user, err := userFromUserID(st, ss.UserID)
	st.Unlock()
	if err != nil {
		return err
	}

	var downloadedSnapFile string
	if ss.DownloadInfo == nil {
		// COMPATIBILITY - this task was created from an older version
		// of snapd that did not store the DownloadInfo in the state
		// yet.
		storeInfo, err := theStore.Snap(ss.Name(), ss.Channel, ss.DevModeAllowed(), user)
		if err != nil {
			return err
		}
		downloadedSnapFile, err = theStore.Download(ss.Name(), &storeInfo.DownloadInfo, meter, user)
		ss.SideInfo = &storeInfo.SideInfo
	} else {
		downloadedSnapFile, err = theStore.Download(ss.Name(), ss.DownloadInfo, meter, user)
	}
	if err != nil {
		return err
	}

	ss.SnapPath = downloadedSnapFile
	// update the snap setup for the follow up tasks
	st.Lock()
	t.Set("snap-setup", ss)
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

	info, err := Info(t.State(), ss.Name(), ss.Revision())
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
	Set(st, ss.Name(), snapst)
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
	info, err := Info(t.State(), ss.Name(), ss.Revision())
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

	if snapst.Current == ss.Revision() && snapst.Active {
		return fmt.Errorf("internal error: cannot discard snap %q: still active", ss.Name())
	}

	if len(snapst.Sequence) == 1 {
		snapst.Sequence = nil
		snapst.Current = snap.Revision{}
	} else {
		newSeq := make([]*snap.SideInfo, 0, len(snapst.Sequence))
		for _, si := range snapst.Sequence {
			if si.Revision == ss.Revision() {
				// leave out
				continue
			}
			newSeq = append(newSeq, si)
		}
		snapst.Sequence = newSeq
		if snapst.Current == ss.Revision() {
			snapst.Current = newSeq[len(newSeq)-1].Revision
		}
	}

	pb := &TaskProgressAdapter{task: t}
	typ, err := snapst.Type()
	if err != nil {
		return err
	}
	err = m.backend.RemoveSnapFiles(ss.placeInfo(), typ, pb)
	if err != nil {
		st.Lock()
		t.Errorf("cannot remove snap file %q, will retry in 3 mins: %s", ss.Name(), err)
		st.Unlock()
		return &state.Retry{After: 3 * time.Minute}
	}

	st.Lock()
	Set(st, ss.Name(), snapst)
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

// TaskSnapSetup returns the SnapSetup with task params hold by or referred to by the the task.
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
	err = Get(t.State(), ss.Name(), &snapst)
	if err != nil && err != state.ErrNoState {
		return nil, nil, err
	}
	return ss, &snapst, nil
}

func (m *SnapManager) undoMountSnap(t *state.Task, _ *tomb.Tomb) error {
	t.State().Lock()
	ss, err := TaskSnapSetup(t)
	t.State().Unlock()
	if err != nil {
		return err
	}

	t.State().Lock()
	var typ snap.Type
	err = t.Get("snap-type", &typ)
	t.State().Unlock()
	// backward compatibility
	if err == state.ErrNoState {
		typ = "app"
	} else if err != nil {
		return err
	}

	pb := &TaskProgressAdapter{task: t}
	return m.backend.UndoSetupSnap(ss.placeInfo(), typ, pb)
}

func (m *SnapManager) doMountSnap(t *state.Task, _ *tomb.Tomb) error {
	t.State().Lock()
	ss, snapst, err := snapSetupAndState(t)
	t.State().Unlock()
	if err != nil {
		return err
	}
	curInfo, err := snapst.CurrentInfo()
	if err != nil && err != ErrNoCurrent {
		return err
	}

	m.backend.CurrentInfo(curInfo)

	if err := checkSnap(t.State(), ss.SnapPath, curInfo, Flags(ss.Flags)); err != nil {
		return err
	}

	pb := &TaskProgressAdapter{task: t}
	// TODO Use ss.Revision() to obtain the right info to mount
	//      instead of assuming the candidate is the right one.
	if err := m.backend.SetupSnap(ss.SnapPath, ss.SideInfo, pb); err != nil {
		return err
	}

	// set snapst type for undoMountSnap
	newInfo, err := readInfo(ss.Name(), ss.SideInfo)
	if err != nil {
		return err
	}
	t.State().Lock()
	t.Set("snap-type", newInfo.Type)
	t.State().Unlock()

	// cleanup the downloaded snap after it got installed
	// in backend.SetupSnap.
	//
	// Note that we always remove the file because the
	// way sideloading works currently is to always create
	// a temporary file (see daemon/api.go:sideloadSnap()
	if err := os.Remove(ss.SnapPath); err != nil {
		logger.Noticef("Failed to cleanup %q: %s", err)
	}

	return nil
}

func (m *SnapManager) undoUnlinkCurrentSnap(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()

	st.Lock()
	defer st.Unlock()

	ss, snapst, err := snapSetupAndState(t)
	if err != nil {
		return err
	}

	oldInfo, err := snapst.CurrentInfo()
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
	Set(st, ss.Name(), snapst)
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

	oldInfo, err := snapst.CurrentInfo()
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
	Set(st, ss.Name(), snapst)
	return nil
}

func (m *SnapManager) undoCopySnapData(t *state.Task, _ *tomb.Tomb) error {
	t.State().Lock()
	ss, snapst, err := snapSetupAndState(t)
	t.State().Unlock()
	if err != nil {
		return err
	}

	newInfo, err := readInfo(ss.Name(), ss.SideInfo)
	if err != nil {
		return err
	}

	oldInfo, err := snapst.CurrentInfo()
	if err != nil && err != ErrNoCurrent {
		return err
	}

	pb := &TaskProgressAdapter{task: t}
	return m.backend.UndoCopySnapData(newInfo, oldInfo, pb)
}

func (m *SnapManager) doCopySnapData(t *state.Task, _ *tomb.Tomb) error {
	t.State().Lock()
	ss, snapst, err := snapSetupAndState(t)
	t.State().Unlock()
	if err != nil {
		return err
	}

	newInfo, err := readInfo(ss.Name(), ss.SideInfo)
	if err != nil {
		return err
	}

	oldInfo, err := snapst.CurrentInfo()
	if err != nil && err != ErrNoCurrent {
		return err
	}

	pb := &TaskProgressAdapter{task: t}
	return m.backend.CopySnapData(newInfo, oldInfo, pb)
}

func (m *SnapManager) doLinkSnap(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()

	st.Lock()
	defer st.Unlock()

	ss, snapst, err := snapSetupAndState(t)
	if err != nil {
		return err
	}

	cand := ss.SideInfo
	m.backend.Candidate(cand)

	hadCandidate := true
	if snapst.findIndex(cand.Revision) < 0 {
		snapst.Sequence = append(snapst.Sequence, cand)
		hadCandidate = false
	}

	oldCurrent := snapst.Current
	snapst.Current = cand.Revision
	snapst.Active = true
	oldChannel := snapst.Channel
	if ss.Channel != "" {
		snapst.Channel = ss.Channel
	}
	oldTryMode := snapst.TryMode()
	snapst.SetTryMode(ss.TryMode())
	oldDevMode := snapst.DevMode()
	snapst.SetDevMode(ss.DevMode())
	oldJailMode := snapst.JailMode()
	snapst.SetJailMode(ss.JailMode())

	newInfo, err := readInfo(ss.Name(), cand)
	if err != nil {
		return err
	}

	// record type
	snapst.SetType(newInfo.Type)

	st.Unlock()
	// XXX: this block is slightly ugly, find a pattern when we have more examples
	err = m.backend.LinkSnap(newInfo)
	if err != nil {
		pb := &TaskProgressAdapter{task: t}
		err := m.backend.UnlinkSnap(newInfo, pb)
		if err != nil {
			st.Lock()
			t.Errorf("cannot cleanup failed attempt at making snap %q available to the system: %v", ss.Name(), err)
			st.Unlock()
		}
	}
	st.Lock()
	if err != nil {
		return err
	}

	// save for undoLinkSnap
	t.Set("old-trymode", oldTryMode)
	t.Set("old-devmode", oldDevMode)
	t.Set("old-jailmode", oldJailMode)
	t.Set("old-channel", oldChannel)
	t.Set("old-current", oldCurrent)
	t.Set("had-candidate", hadCandidate)
	// Do at the end so we only preserve the new state if it worked.
	Set(st, ss.Name(), snapst)
	// Make sure if state commits and snapst is mutated we won't be rerun
	t.SetStatus(state.DoneStatus)

	// if we just installed a core snap, request a restart
	// so that we switch executing its snapd
	if newInfo.Type == snap.TypeOS && release.OnClassic {
		t.Logf("Restarting snapd...")
		st.Unlock()
		st.RequestRestart()
		st.Lock()
	}

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
	var oldDevMode bool
	err = t.Get("old-devmode", &oldDevMode)
	if err != nil {
		return err
	}
	var oldJailMode bool
	err = t.Get("old-jailmode", &oldJailMode)
	if err != nil {
		return err
	}
	var oldCurrent snap.Revision
	err = t.Get("old-current", &oldCurrent)
	if err != nil {
		return err
	}
	var hadCandidate bool
	err = t.Get("had-candidate", &hadCandidate)
	if err != nil && err != state.ErrNoState {
		return err
	}

	// relinking of the old snap is done in the undo of unlink-current-snap
	currentIndex := snapst.findIndex(snapst.Current)
	if currentIndex < 0 {
		return fmt.Errorf("internal error: cannot find revision %d in %v for undoing the added revision", ss.SideInfo.Revision, snapst.Sequence)
	}
	if !hadCandidate {
		snapst.Sequence = append(snapst.Sequence[:currentIndex], snapst.Sequence[currentIndex+1:]...)
	}
	snapst.Current = oldCurrent
	snapst.Active = false
	snapst.Channel = oldChannel
	snapst.SetTryMode(oldTryMode)
	snapst.SetDevMode(oldDevMode)
	snapst.SetJailMode(oldJailMode)

	newInfo, err := readInfo(ss.Name(), ss.SideInfo)
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
	Set(st, ss.Name(), snapst)
	// Make sure if state commits and snapst is mutated we won't be rerun
	t.SetStatus(state.UndoneStatus)
	return nil
}
