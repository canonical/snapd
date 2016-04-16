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

	"github.com/ubuntu-core/snappy/i18n"
	"github.com/ubuntu-core/snappy/logger"
	"github.com/ubuntu-core/snappy/overlord/state"
	"github.com/ubuntu-core/snappy/snap"
	"github.com/ubuntu-core/snappy/snappy"
)

// allow exchange in the tests
var backend managerBackend = &defaultBackend{}

func doInstall(s *state.State, curActive bool, snapName, snapPath, channel string, flags snappy.InstallFlags) (*state.TaskSet, error) {
	if err := checkChangeConflict(s, snapName); err != nil {
		return nil, err
	}

	var prepare *state.Task
	ss := SnapSetup{
		Channel: channel,
		Flags:   int(flags),
	}
	ss.Name = snapName
	ss.SnapPath = snapPath
	if snapPath != "" {
		prepare = s.NewTask("prepare-snap", fmt.Sprintf(i18n.G("Prepare snap %q"), snapPath))
	} else {
		prepare = s.NewTask("download-snap", fmt.Sprintf(i18n.G("Download snap %q"), snapName))
	}
	prepare.Set("snap-setup", ss)

	tasks := []*state.Task{prepare}
	addTask := func(t *state.Task) {
		t.Set("snap-setup-task", prepare.ID())
		tasks = append(tasks, t)
	}

	// mount
	mount := s.NewTask("mount-snap", fmt.Sprintf(i18n.G("Mount snap %q"), snapName))
	addTask(mount)
	mount.WaitFor(prepare)
	precopy := mount

	if curActive {
		// unlink-current-snap (will stop services for copy-data)
		unlink := s.NewTask("unlink-current-snap", fmt.Sprintf(i18n.G("Make current revision for snap %q unavailable"), snapName))
		addTask(unlink)
		unlink.WaitFor(mount)
		precopy = unlink
	}

	// copy-data (needs stopped services by unlink)
	copyData := s.NewTask("copy-snap-data", fmt.Sprintf(i18n.G("Copy snap %q data"), snapName))
	addTask(copyData)
	copyData.WaitFor(precopy)

	// security
	setupSecurity := s.NewTask("setup-profiles", fmt.Sprintf(i18n.G("Setup snap %q security profiles"), snapName))
	addTask(setupSecurity)
	setupSecurity.WaitFor(copyData)

	// finalize (wrappers+current symlink)
	linkSnap := s.NewTask("link-snap", fmt.Sprintf(i18n.G("Make snap %q available to the system"), snapName))
	addTask(linkSnap)
	linkSnap.WaitFor(setupSecurity)

	return state.NewTaskSet(tasks...), nil
}

func readSnapInfo(snapPath string) (*snap.Info, error) {
	// TODO Only open if in devmode or we have the assertion proving content right.
	snapf, err := snap.Open(snapPath)
	if err != nil {
		return nil, err
	}
	return snapf.Info()
}

func checkChangeConflict(s *state.State, snapName string) error {
	for _, task := range s.Tasks() {
		k := task.Kind()
		chg := task.Change()
		if (k == "link-snap" || k == "unlink-snap") && (chg == nil || !chg.Status().Ready()) {
			ss, err := TaskSnapSetup(task)
			if err != nil {
				return fmt.Errorf("internal error: cannot obtain snap setup from task: %s", task.Summary())
			}
			if ss.Name == snapName {
				return fmt.Errorf("snap %q has changes in progress", snapName)
			}
		}
	}
	return nil
}

// Install returns a set of tasks for installing snap.
// Note that the state must be locked by the caller.
func Install(s *state.State, name, channel string, flags snappy.InstallFlags) (*state.TaskSet, error) {
	var snapst SnapState
	err := Get(s, name, &snapst)
	if err != nil && err != state.ErrNoState {
		return nil, err
	}
	if snapst.Current() != nil {
		return nil, fmt.Errorf("snap %q already installed", name)
	}

	return doInstall(s, false, name, "", channel, flags)
}

// InstallPath returns a set of tasks for installing snap from a file path.
// Note that the state must be locked by the caller.
func InstallPath(s *state.State, path, channel string, flags snappy.InstallFlags) (*state.TaskSet, error) {
	info, err := readSnapInfo(path)
	if err != nil {
		return nil, err
	}
	snapName := info.Name()

	var snapst SnapState
	err = Get(s, snapName, &snapst)
	if err != nil && err != state.ErrNoState {
		return nil, err
	}

	return doInstall(s, snapst.Active, snapName, path, channel, flags)
}

// Update initiates a change updating a snap.
// Note that the state must be locked by the caller.
func Update(s *state.State, name, channel string, flags snappy.InstallFlags) (*state.TaskSet, error) {
	var snapst SnapState
	err := Get(s, name, &snapst)
	if err != nil && err != state.ErrNoState {
		return nil, err
	}
	if snapst.Current() == nil {
		return nil, fmt.Errorf("cannot find snap %q", name)
	}

	return doInstall(s, snapst.Active, name, "", channel, flags)
}

// Remove returns a set of tasks for removing snap.
// Note that the state must be locked by the caller.
func Remove(s *state.State, name string, flags snappy.RemoveFlags) (*state.TaskSet, error) {
	if err := checkChangeConflict(s, name); err != nil {
		return nil, err
	}

	var snapst SnapState
	err := Get(s, name, &snapst)
	if err != nil && err != state.ErrNoState {
		return nil, err
	}

	cur := snapst.Current()
	if cur == nil {
		return nil, fmt.Errorf("cannot find snap %q", name)
	}

	revision := snapst.Current().Revision
	active := snapst.Active

	info, err := Info(s, name, revision)
	if err != nil {
		return nil, err
	}

	ss := SnapSetup{
		Name:     name,
		Revision: revision,
		Flags:    int(flags),
	}

	// check if this is something that can be removed
	if !backend.CanRemove(info, active) {
		return nil, fmt.Errorf("snap %q is not removable", ss.Name)
	}

	// trigger remove

	discardSnap := s.NewTask("discard-snap", fmt.Sprintf(i18n.G("Remove snap %q from the system"), name))
	discardSnap.Set("snap-setup", ss)

	discardSnapID := discardSnap.ID()
	tasks := ([]*state.Task)(nil)
	var chain *state.Task
	addNext := func(t *state.Task) {
		if chain != nil {
			t.WaitFor(chain)
		}
		if t.ID() != discardSnapID {
			t.Set("snap-setup-task", discardSnapID)
		}
		tasks = append(tasks, t)
		chain = t
	}

	if active {
		unlink := s.NewTask("unlink-snap", fmt.Sprintf(i18n.G("Make snap %q unavailable to the system"), name))

		addNext(unlink)
	}

	removeSecurity := s.NewTask("remove-profiles", fmt.Sprintf(i18n.G("Remove security profile for snap %q"), name))
	addNext(removeSecurity)

	clearData := s.NewTask("clear-snap", fmt.Sprintf(i18n.G("Remove data for snap %q"), name))
	addNext(clearData)

	addNext(discardSnap)

	if len(snapst.Sequence) == 1 {
		discardConns := s.NewTask("discard-conns", fmt.Sprintf(i18n.G("Discard interface connections for snap %q"), name))
		addNext(discardConns)
	}

	return state.NewTaskSet(tasks...), nil
}

// Rollback returns a set of tasks for rolling back a snap.
// Note that the state must be locked by the caller.
func Rollback(s *state.State, snap, ver string) (*state.TaskSet, error) {
	return nil, fmt.Errorf("rollback not implemented")
}

// Activate returns a set of tasks for activating a snap.
// Note that the state must be locked by the caller.
func Activate(s *state.State, name string) (*state.TaskSet, error) {
	return nil, fmt.Errorf("activate not implemented")
}

// Activate returns a set of tasks for activating a snap.
// Note that the state must be locked by the caller.
func Deactivate(s *state.State, name string) (*state.TaskSet, error) {
	return nil, fmt.Errorf("deactivate not implemented")
}

// Retrieval functions

var readInfo = snap.ReadInfo

// Info returns the information about the snap with given name and revision.
// Works also for a mounted candidate snap in the process of being installed.
func Info(s *state.State, name string, revision int) (*snap.Info, error) {
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

	if snapst.Candidate != nil && snapst.Candidate.Revision == revision {
		return readInfo(name, snapst.Candidate)
	}

	return nil, fmt.Errorf("cannot find snap %q at revision %d", name, revision)
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
		if snapState.Current() != nil {
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
	if snapst == nil || (len(snapst.Sequence) == 0 && snapst.Candidate == nil) {
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
		snapInfo, err := readInfo(snapName, snapState.Current())
		if err != nil {
			logger.Noticef("cannot retrieve info for snap %q: %s", snapName, err)
			continue
		}
		infos = append(infos, snapInfo)
	}
	return infos, nil
}
