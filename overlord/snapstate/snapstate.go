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
	"reflect"
	"sort"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store"
)

// Flags are used to pass additional flags to operations and to keep track of snap modes.
type Flags int

const (
	// DevMode switches confinement to non-enforcing mode.
	DevMode = 1 << iota
	// TryMode is set for snaps installed to try directly from a local directory.
	TryMode

	// JailMode is set when the user has requested confinement
	// always be enforcing, even if the snap requests otherwise.
	JailMode

	// if we need flags for just SnapSetup it may be easier
	// to start a new sequence from the other end with:
	// 0x40000000 >> iota
)

func (f Flags) DevModeAllowed() bool {
	return f&(DevMode|JailMode) != 0
}

func (f Flags) DevMode() bool {
	return f&DevMode != 0
}

func (f Flags) JailMode() bool {
	return f&JailMode != 0
}

func doInstall(s *state.State, snapst *SnapState, ss *SnapSetup) (*state.TaskSet, error) {
	if err := checkChangeConflict(s, ss.Name(), snapst); err != nil {
		return nil, err
	}

	if ss.SnapPath == "" && ss.Channel == "" {
		ss.Channel = "stable"
	}

	revisionStr := ""
	if ss.SideInfo != nil {
		revisionStr = fmt.Sprintf(" (%s)", ss.Revision())
	}

	// check if we already have the revision locally (alters tasks)
	revisionIsLocal := snapst.findIndex(ss.Revision()) >= 0

	var prepare, prev *state.Task
	// if we have a local revision here we go back to that
	if ss.SnapPath != "" || revisionIsLocal {
		prepare = s.NewTask("prepare-snap", fmt.Sprintf(i18n.G("Prepare snap %q%s"), ss.SnapPath, revisionStr))
	} else {
		prepare = s.NewTask("download-snap", fmt.Sprintf(i18n.G("Download snap %q%s from channel %q"), ss.Name(), revisionStr, ss.Channel))
	}
	prepare.Set("snap-setup", ss)

	tasks := []*state.Task{prepare}
	addTask := func(t *state.Task) {
		t.Set("snap-setup-task", prepare.ID())
		t.WaitFor(prev)
		tasks = append(tasks, t)
	}

	// mount
	prev = prepare
	if !revisionIsLocal {
		mount := s.NewTask("mount-snap", fmt.Sprintf(i18n.G("Mount snap %q%s"), ss.Name(), revisionStr))
		addTask(mount)
		prev = mount
	}

	if snapst.Active {
		// unlink-current-snap (will stop services for copy-data)
		unlink := s.NewTask("unlink-current-snap", fmt.Sprintf(i18n.G("Make current revision for snap %q unavailable"), ss.Name()))
		addTask(unlink)
		prev = unlink
	}

	// copy-data (needs stopped services by unlink)
	if !revisionIsLocal {
		copyData := s.NewTask("copy-snap-data", fmt.Sprintf(i18n.G("Copy snap %q data"), ss.Name()))
		addTask(copyData)
		prev = copyData
	}

	// security
	setupSecurity := s.NewTask("setup-profiles", fmt.Sprintf(i18n.G("Setup snap %q%s security profiles"), ss.Name(), revisionStr))
	addTask(setupSecurity)
	prev = setupSecurity

	// finalize (wrappers+current symlink)
	linkSnap := s.NewTask("link-snap", fmt.Sprintf(i18n.G("Make snap %q%s available to the system"), ss.Name(), revisionStr))
	addTask(linkSnap)

	// Do not do that if we are reverting to a local revision
	if snapst.HasCurrent() && !revisionIsLocal {
		prev := linkSnap
		seq := snapst.Sequence
		currentIndex := snapst.findIndex(snapst.Current)

		// discard everything after "current" (we may have reverted to
		// a previous versions earlier)
		for i := currentIndex + 1; i < len(seq); i++ {
			si := seq[i]
			ts := removeInactiveRevision(s, ss.Name(), si.Revision)
			ts.WaitFor(prev)
			tasks = append(tasks, ts.Tasks()...)
			prev = tasks[len(tasks)-1]
		}

		// normal garbage collect
		for i := 0; i <= currentIndex-2; i++ {
			si := seq[i]
			ts := removeInactiveRevision(s, ss.Name(), si.Revision)
			ts.WaitFor(prev)
			tasks = append(tasks, ts.Tasks()...)
			prev = tasks[len(tasks)-1]
		}
	}

	return state.NewTaskSet(tasks...), nil
}

func checkChangeConflict(s *state.State, snapName string, snapst *SnapState) error {
	for _, task := range s.Tasks() {
		k := task.Kind()
		chg := task.Change()
		if (k == "link-snap" || k == "unlink-snap") && (chg == nil || !chg.Status().Ready()) {
			ss, err := TaskSnapSetup(task)
			if err != nil {
				return fmt.Errorf("internal error: cannot obtain snap setup from task: %s", task.Summary())
			}
			if ss.Name() == snapName {
				return fmt.Errorf("snap %q has changes in progress", snapName)
			}
		}
	}

	if snapst != nil {
		// caller wants us to also make sure the SnapState in state
		// matches the one they provided. Necessary because we need to
		// unlock while talking to the store, during which a change can
		// sneak in (if it's before the taskset is created) (e.g. for
		// install, while getting the snap info; for refresh, when
		// getting what needs refreshing).
		var cursnapst SnapState
		if err := Get(s, snapName, &cursnapst); err != nil && err != state.ErrNoState {
			return err
		}

		// TODO: implement the rather-boring-but-more-performant SnapState.Equals
		if !reflect.DeepEqual(snapst, &cursnapst) {
			return fmt.Errorf("snap %q state changed during install preparations", snapName)
		}
	}

	return nil
}

// InstallPath returns a set of tasks for installing snap from a file path.
// Note that the state must be locked by the caller.
func InstallPath(s *state.State, name, path, channel string, flags Flags) (*state.TaskSet, error) {
	var snapst SnapState
	err := Get(s, name, &snapst)
	if err != nil && err != state.ErrNoState {
		return nil, err
	}

	ss := &SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: name,
		},
		SnapPath: path,
		Channel:  channel,
		Flags:    SnapSetupFlags(flags),
	}

	return doInstall(s, &snapst, ss)
}

// TryPath returns a set of tasks for trying a snap from a file path.
// Note that the state must be locked by the caller.
func TryPath(s *state.State, name, path string, flags Flags) (*state.TaskSet, error) {
	flags |= TryMode

	return InstallPath(s, name, path, "", flags)
}

// Install returns a set of tasks for installing snap.
// Note that the state must be locked by the caller.
func Install(s *state.State, name, channel string, userID int, flags Flags) (*state.TaskSet, error) {
	var snapst SnapState
	err := Get(s, name, &snapst)
	if err != nil && err != state.ErrNoState {
		return nil, err
	}
	if snapst.HasCurrent() {
		return nil, fmt.Errorf("snap %q already installed", name)
	}

	snapInfo, err := snapInfo(s, name, channel, userID, flags)
	if err != nil {
		return nil, err
	}

	ss := &SnapSetup{
		Channel:      channel,
		UserID:       userID,
		Flags:        SnapSetupFlags(flags),
		DownloadInfo: &snapInfo.DownloadInfo,
		SideInfo:     &snapInfo.SideInfo,
	}

	return doInstall(s, &snapst, ss)
}

// contains determines whether the given string is contained in the
// given list of strings, which must have been previously sorted using
// sort.Strings.
func contains(ns []string, n string) bool {
	i := sort.SearchStrings(ns, n)
	if i >= len(ns) {
		return false
	}
	return ns[i] == n
}

// RefreshCandidates gets a list of candidates for update
// Note that the state must be locked by the caller.
func RefreshCandidates(st *state.State, user *auth.UserState) ([]*snap.Info, error) {
	updates, _, err := refreshCandidates(st, nil, user)
	return updates, err
}

func refreshCandidates(st *state.State, names []string, user *auth.UserState) ([]*snap.Info, map[string]*SnapState, error) {
	snapStates, err := All(st)
	if err != nil {
		return nil, nil, err
	}

	sort.Strings(names)

	stateByID := make(map[string]*SnapState, len(snapStates))
	candidatesInfo := make([]*store.RefreshCandidate, 0, len(snapStates))
	for _, snapst := range snapStates {
		if snapst.TryMode() || snapst.DevMode() {
			// no automatic refreshes for trymode nor devmode
			continue
		}

		// FIXME: snaps that are not active are skipped for now
		//        until we know what we want to do
		if !snapst.Active {
			continue
		}

		snapInfo, err := snapst.CurrentInfo()
		if err != nil {
			// log something maybe?
			continue
		}

		if len(names) > 0 && !contains(names, snapInfo.Name()) {
			continue
		}

		stateByID[snapInfo.SnapID] = snapst

		// get confinement preference from the snapstate
		candidatesInfo = append(candidatesInfo, &store.RefreshCandidate{
			// the desired channel (not info.Channel!)
			Channel: snapst.Channel,
			DevMode: snapst.DevModeAllowed(),
			Block:   snapst.Block(),

			SnapID:   snapInfo.SnapID,
			Revision: snapInfo.Revision,
			Epoch:    snapInfo.Epoch,
		})
	}

	theStore := Store(st)

	st.Unlock()
	updates, err := theStore.ListRefresh(candidatesInfo, user)
	st.Lock()
	if err != nil {
		return nil, nil, err
	}

	return updates, stateByID, nil
}

// UpdateMany updates everything from the given list of names that the
// store says is updateable. If the list is empty, update everything.
// Note that the state must be locked by the caller.
func UpdateMany(st *state.State, names []string, userID int) ([]string, []*state.TaskSet, error) {
	user, err := userFromUserID(st, userID)
	if err != nil {
		return nil, nil, err
	}

	updates, stateByID, err := refreshCandidates(st, names, user)
	if err != nil {
		return nil, nil, err
	}

	updated := make([]string, 0, len(updates))
	tasksets := make([]*state.TaskSet, 0, len(updates))
	for _, update := range updates {
		snapst := stateByID[update.SnapID]
		// XXX: this check goes away when update-to-local is done
		if err := checkRevisionIsNew(update.Name(), snapst, update.Revision); err != nil {
			continue
		}

		ss := &SnapSetup{
			Channel:      snapst.Channel,
			UserID:       userID,
			Flags:        SnapSetupFlags(snapst.Flags),
			DownloadInfo: &update.DownloadInfo,
			SideInfo:     &update.SideInfo,
		}

		ts, err := doInstall(st, snapst, ss)
		if err != nil {
			// log?
			continue
		}
		updated = append(updated, update.Name())
		tasksets = append(tasksets, ts)
	}

	return updated, tasksets, nil
}

// Update initiates a change updating a snap.
// Note that the state must be locked by the caller.
func Update(s *state.State, name, channel string, userID int, flags Flags) (*state.TaskSet, error) {
	var snapst SnapState
	err := Get(s, name, &snapst)
	if err != nil && err != state.ErrNoState {
		return nil, err
	}
	if !snapst.HasCurrent() {
		return nil, fmt.Errorf("cannot find snap %q", name)
	}

	// FIXME: snaps that are no active are skipped for now
	//        until we know what we want to do
	if !snapst.Active {
		return nil, fmt.Errorf("refreshing disabled snap %q not supported", name)
	}

	if channel == "" {
		channel = snapst.Channel
	}

	updateInfo, err := updateInfo(s, &snapst, channel, userID, flags)
	if err != nil {
		return nil, err
	}
	if err := checkRevisionIsNew(name, &snapst, updateInfo.Revision); err != nil {
		return nil, err
	}

	ss := &SnapSetup{
		Channel:      channel,
		UserID:       userID,
		Flags:        SnapSetupFlags(flags),
		DownloadInfo: &updateInfo.DownloadInfo,
		SideInfo:     &updateInfo.SideInfo,
	}

	return doInstall(s, &snapst, ss)
}

// Enable sets a snap to the active state
func Enable(s *state.State, name string) (*state.TaskSet, error) {
	var snapst SnapState
	err := Get(s, name, &snapst)
	if err == state.ErrNoState {
		return nil, fmt.Errorf("cannot find snap %q", name)
	}
	if err != nil {
		return nil, err
	}

	if snapst.Active {
		return nil, fmt.Errorf("snap %q already enabled", name)
	}

	if err := checkChangeConflict(s, name, nil); err != nil {
		return nil, err
	}

	ss := &SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: name,
			Revision: snapst.Current,
		},
	}

	prepareSnap := s.NewTask("prepare-snap", fmt.Sprintf(i18n.G("Prepare snap %q (%s)"), ss.Name(), snapst.Current))
	prepareSnap.Set("snap-setup", &ss)

	linkSnap := s.NewTask("link-snap", fmt.Sprintf(i18n.G("Make snap %q (%s) available to the system%s"), ss.Name(), snapst.Current))
	linkSnap.Set("snap-setup", &ss)
	linkSnap.WaitFor(prepareSnap)

	return state.NewTaskSet(prepareSnap, linkSnap), nil
}

// Disable sets a snap to the inactive state
func Disable(s *state.State, name string) (*state.TaskSet, error) {
	var snapst SnapState
	err := Get(s, name, &snapst)
	if err == state.ErrNoState {
		return nil, fmt.Errorf("cannot find snap %q", name)
	}
	if err != nil {
		return nil, err
	}
	if !snapst.Active {
		return nil, fmt.Errorf("snap %q already disabled", name)
	}

	if err := checkChangeConflict(s, name, nil); err != nil {
		return nil, err
	}

	ss := &SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: name,
			Revision: snapst.Current,
		},
	}

	unlinkSnap := s.NewTask("unlink-snap", fmt.Sprintf(i18n.G("Make snap %q (%s) unavailable to the system"), ss.Name(), snapst.Current))
	unlinkSnap.Set("snap-setup", &ss)

	return state.NewTaskSet(unlinkSnap), nil
}

func removeInactiveRevision(s *state.State, name string, revision snap.Revision) *state.TaskSet {
	ss := SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: name,
			Revision: revision,
		},
	}

	clearData := s.NewTask("clear-snap", fmt.Sprintf(i18n.G("Remove data for snap %q (%s)"), name, revision))
	clearData.Set("snap-setup", ss)

	discardSnap := s.NewTask("discard-snap", fmt.Sprintf(i18n.G("Remove snap %q (%s) from the system"), name, revision))
	discardSnap.WaitFor(clearData)
	discardSnap.Set("snap-setup-task", clearData.ID())

	return state.NewTaskSet(clearData, discardSnap)
}

// canRemove verifies that a snap can be removed.
func canRemove(s *snap.Info, active bool) bool {
	// Gadget snaps should not be removed as they are a key
	// building block for Gadgets. Pruning non active ones
	// is acceptable.
	if s.Type == snap.TypeGadget && active {
		return false
	}

	// You never want to remove an active kernel or OS
	if (s.Type == snap.TypeKernel || s.Type == snap.TypeOS) && active {
		return false
	}
	// TODO: on classic likely let remove core even if active if it's only snap left.

	return true
}

// Remove returns a set of tasks for removing snap.
// Note that the state must be locked by the caller.
func Remove(s *state.State, name string) (*state.TaskSet, error) {
	var snapst SnapState
	err := Get(s, name, &snapst)
	if err != nil && err != state.ErrNoState {
		return nil, err
	}

	if !snapst.HasCurrent() {
		return nil, fmt.Errorf("cannot find snap %q", name)
	}

	if err := checkChangeConflict(s, name, nil); err != nil {
		return nil, err
	}

	revision := snapst.Current
	active := snapst.Active

	info, err := Info(s, name, revision)
	if err != nil {
		return nil, err
	}

	// check if this is something that can be removed
	if !canRemove(info, active) {
		return nil, fmt.Errorf("snap %q is not removable", name)
	}

	// main/current SnapSetup
	ss := SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: name,
			Revision: revision,
		},
	}

	// trigger remove

	full := state.NewTaskSet()
	var chain *state.TaskSet

	addNext := func(ts *state.TaskSet) {
		if chain != nil {
			ts.WaitAll(chain)
		}
		full.AddAll(ts)
		chain = ts
	}

	if active { // unlink
		unlink := s.NewTask("unlink-snap", fmt.Sprintf(i18n.G("Make snap %q unavailable to the system"), name))
		unlink.Set("snap-setup", ss)

		removeSecurity := s.NewTask("remove-profiles", fmt.Sprintf(i18n.G("Remove security profile for snap %q (%s)"), name, revision))
		removeSecurity.WaitFor(unlink)

		removeSecurity.Set("snap-setup-task", unlink.ID())

		addNext(state.NewTaskSet(unlink, removeSecurity))
	}

	seq := snapst.Sequence
	for i := len(seq) - 1; i >= 0; i-- {
		si := seq[i]
		addNext(removeInactiveRevision(s, name, si.Revision))
	}

	discardConns := s.NewTask("discard-conns", fmt.Sprintf(i18n.G("Discard interface connections for snap %q (%s)"), name, revision))
	discardConns.Set("snap-setup", &SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: name,
		},
	})
	addNext(state.NewTaskSet(discardConns))

	return full, nil
}

// Revert returns a set of tasks for reverting to the pervious version of the snap.
// Note that the state must be locked by the caller.
func Revert(s *state.State, name string) (*state.TaskSet, error) {
	var snapst SnapState
	err := Get(s, name, &snapst)
	if err != nil && err != state.ErrNoState {
		return nil, err
	}

	pi := snapst.previousSideInfo()
	if pi == nil {
		return nil, fmt.Errorf("no revision to revert to")
	}
	return RevertToRevision(s, name, pi.Revision)
}

func RevertToRevision(s *state.State, name string, rev snap.Revision) (*state.TaskSet, error) {
	var snapst SnapState
	err := Get(s, name, &snapst)
	if err != nil && err != state.ErrNoState {
		return nil, err
	}

	if snapst.Current == rev {
		return nil, fmt.Errorf("already on requested revision")
	}

	if !snapst.Active {
		return nil, fmt.Errorf("cannot revert inactive snaps")
	}
	i := snapst.findIndex(rev)
	if i < 0 {
		return nil, fmt.Errorf("cannot find revision %s for snap %q", rev, name)
	}
	ss := &SnapSetup{
		SideInfo: snapst.Sequence[i],
	}
	return doInstall(s, &snapst, ss)
}

// Info returns the information about the snap with given name and revision.
// Works also for a mounted candidate snap in the process of being installed.
func Info(s *state.State, name string, revision snap.Revision) (*snap.Info, error) {
	var snapst SnapState
	err := Get(s, name, &snapst)
	if err == state.ErrNoState {
		return nil, fmt.Errorf("cannot find snap %q", name)
	}
	if err != nil {
		return nil, err
	}

	for i := len(snapst.Sequence) - 1; i >= 0; i-- {
		if si := snapst.Sequence[i]; si.Revision == revision {
			return readInfo(name, si)
		}
	}

	return nil, fmt.Errorf("cannot find snap %q at revision %s", name, revision.String())
}

// CurrentInfo returns the information about the current revision of a snap with the given name.
func CurrentInfo(s *state.State, name string) (*snap.Info, error) {
	var snapst SnapState
	err := Get(s, name, &snapst)
	if err != nil && err != state.ErrNoState {
		return nil, err
	}
	info, err := snapst.CurrentInfo()
	if err == ErrNoCurrent {
		return nil, fmt.Errorf("cannot find snap %q", name)
	}
	return info, err
}

// Get retrieves the SnapState of the given snap.
func Get(s *state.State, name string, snapst *SnapState) error {
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

// All retrieves return a map from name to SnapState for all current snaps in the system state.
func All(s *state.State) (map[string]*SnapState, error) {
	// XXX: result is a map because sideloaded snaps carry no name
	// atm in their sideinfos
	var stateMap map[string]*SnapState
	if err := s.Get("snaps", &stateMap); err != nil && err != state.ErrNoState {
		return nil, err
	}
	curStates := make(map[string]*SnapState, len(stateMap))
	for snapName, snapState := range stateMap {
		if snapState.HasCurrent() {
			curStates[snapName] = snapState
		}
	}
	return curStates, nil
}

// Set sets the SnapState of the given snap, overwriting any earlier state.
func Set(s *state.State, name string, snapst *SnapState) {
	var snaps map[string]*json.RawMessage
	err := s.Get("snaps", &snaps)
	if err != nil && err != state.ErrNoState {
		panic("internal error: cannot unmarshal snaps state: " + err.Error())
	}
	if snaps == nil {
		snaps = make(map[string]*json.RawMessage)
	}
	if snapst == nil || (len(snapst.Sequence) == 0) {
		delete(snaps, name)
	} else {
		data, err := json.Marshal(snapst)
		if err != nil {
			panic("internal error: cannot marshal snap state: " + err.Error())
		}
		raw := json.RawMessage(data)
		snaps[name] = &raw
	}
	s.Set("snaps", snaps)
}

// ActiveInfos returns information about all active snaps.
func ActiveInfos(s *state.State) ([]*snap.Info, error) {
	var stateMap map[string]*SnapState
	var infos []*snap.Info
	if err := s.Get("snaps", &stateMap); err != nil && err != state.ErrNoState {
		return nil, err
	}
	for snapName, snapState := range stateMap {
		if !snapState.Active {
			continue
		}
		snapInfo, err := snapState.CurrentInfo()
		if err != nil {
			logger.Noticef("cannot retrieve info for snap %q: %s", snapName, err)
			continue
		}
		infos = append(infos, snapInfo)
	}
	return infos, nil
}

// GadgetInfo finds the current gadget snap's info.
func GadgetInfo(s *state.State) (*snap.Info, error) {
	var stateMap map[string]*SnapState
	if err := s.Get("snaps", &stateMap); err != nil && err != state.ErrNoState {
		return nil, err
	}
	for _, snapState := range stateMap {
		if !snapState.HasCurrent() {
			continue
		}
		typ, err := snapState.Type()
		if err != nil {
			return nil, err
		}
		if typ != snap.TypeGadget {
			continue
		}
		return snapState.CurrentInfo()
	}

	return nil, state.ErrNoState
}
