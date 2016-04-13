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
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/ubuntu-core/snappy/i18n"
	"github.com/ubuntu-core/snappy/osutil"
	"github.com/ubuntu-core/snappy/overlord/state"
	"github.com/ubuntu-core/snappy/snap"
	"github.com/ubuntu-core/snappy/snappy"
)

// allow exchange in the tests
var backend managerBackend = &defaultBackend{}

func doInstall(s *state.State, snapName, channel string, flags snappy.InstallFlags) (*state.TaskSet, error) {
	// download
	var prepare *state.Task
	ss := SnapSetup{
		Channel: channel,
		Flags:   int(flags),
	}
	if osutil.FileExists(snapName) {
		ss.SnapPath = snapName
		prepare = s.NewTask("prepare-snap", fmt.Sprintf(i18n.G("Prepare snap %q"), snapName))
	} else {
		name, developer := snappy.SplitDeveloper(snapName)
		ss.Name = name
		ss.Developer = developer
		prepare = s.NewTask("download-snap", fmt.Sprintf(i18n.G("Download snap %q"), snapName))
	}
	prepare.Set("snap-setup", ss)

	// mount
	mount := s.NewTask("mount-snap", fmt.Sprintf(i18n.G("Mount snap %q"), snapName))
	mount.Set("snap-setup-task", prepare.ID())
	mount.WaitFor(prepare)

	// copy-data (needs to stop services)
	copyData := s.NewTask("copy-snap-data", fmt.Sprintf(i18n.G("Copy snap %q data"), snapName))
	copyData.Set("snap-setup-task", prepare.ID())
	copyData.WaitFor(mount)

	// security
	setupSecurity := s.NewTask("setup-snap-security", fmt.Sprintf(i18n.G("Setup snap %q security profiles"), snapName))
	setupSecurity.Set("snap-setup-task", prepare.ID())
	setupSecurity.WaitFor(copyData)

	// finalize (wrappers+current symlink)
	linkSnap := s.NewTask("link-snap", fmt.Sprintf(i18n.G("Make snap %q available to the system"), snapName))
	linkSnap.Set("snap-setup-task", prepare.ID())
	linkSnap.WaitFor(setupSecurity)

	return state.NewTaskSet(prepare, mount, copyData, setupSecurity, linkSnap), nil
}

// Install returns a set of tasks for installing snap.
// Note that the state must be locked by the caller.
func Install(s *state.State, snap, channel string, flags snappy.InstallFlags) (*state.TaskSet, error) {
	name, _ := snappy.SplitDeveloper(snap)
	info := backend.ActiveSnap(name)
	if info != nil {
		return nil, fmt.Errorf("snap %q already installed", snap)
	}

	return doInstall(s, snap, channel, flags)
}

// Update initiates a change updating a snap.
// Note that the state must be locked by the caller.
func Update(s *state.State, snap, channel string, flags snappy.InstallFlags) (*state.TaskSet, error) {
	name, _ := snappy.SplitDeveloper(snap)
	info := backend.ActiveSnap(name)
	if info == nil {
		return nil, fmt.Errorf("cannot find snap %q", snap)
	}

	return doInstall(s, snap, channel, flags)
}

// parseSnapspec parses a string like: name[.developer][=version]
func parseSnapSpec(snapSpec string) (string, string) {
	l := strings.Split(snapSpec, "=")
	if len(l) == 2 {
		return l[0], l[1]
	}
	return snapSpec, ""
}

// Remove returns a set of tasks for removing snap.
// Note that the state must be locked by the caller.
func Remove(s *state.State, snapSpec string, flags snappy.RemoveFlags) (*state.TaskSet, error) {
	// allow remove by version so that we can remove snaps that are
	// not active
	name, version := parseSnapSpec(snapSpec)
	name, developer := snappy.SplitDeveloper(name)
	revision := 0
	if version == "" {
		info := backend.ActiveSnap(name)
		if info == nil {
			return nil, fmt.Errorf("cannot find active snap for %q", name)
		}
		revision = info.Revision
	} else {
		info := backend.SnapByNameAndVersion(name, version)
		if info == nil {
			return nil, fmt.Errorf("cannot find snap for %q and version %q", name, version)
		}
		revision = info.Revision
	}

	ss := SnapSetup{
		Name:      name,
		Developer: developer,
		Revision:  revision,
		Flags:     int(flags),
	}
	// check if this is something that can be removed
	if err := backend.CanRemove(ss.MountDir()); err != nil {
		return nil, err
	}

	// trigger remove
	unlink := s.NewTask("unlink-snap", fmt.Sprintf(i18n.G("Deactivating %q"), snapSpec))
	unlink.Set("snap-setup", ss)

	removeSecurity := s.NewTask("remove-snap-security", fmt.Sprintf(i18n.G("Removing security profile for %q"), snapSpec))
	removeSecurity.WaitFor(unlink)
	removeSecurity.Set("snap-setup-task", unlink.ID())

	removeData := s.NewTask("remove-snap-data", fmt.Sprintf(i18n.G("Removing data for %q"), snapSpec))
	removeData.Set("snap-setup-task", unlink.ID())
	removeData.WaitFor(removeSecurity)

	removeFiles := s.NewTask("remove-snap-files", fmt.Sprintf(i18n.G("Removing files for %q"), snapSpec))
	removeFiles.Set("snap-setup-task", unlink.ID())
	removeFiles.WaitFor(removeData)

	return state.NewTaskSet(unlink, removeSecurity, removeData, removeFiles), nil
}

// Rollback returns a set of tasks for rolling back a snap.
// Note that the state must be locked by the caller.
func Rollback(s *state.State, snap, ver string) (*state.TaskSet, error) {
	t := s.NewTask("rollback-snap", fmt.Sprintf(i18n.G("Rolling back %q"), snap))
	t.Set("snap-setup", SnapSetup{
		Name:            snap,
		RollbackVersion: ver,
	})

	return state.NewTaskSet(t), nil
}

// Activate returns a set of tasks for activating a snap.
// Note that the state must be locked by the caller.
func Activate(s *state.State, snap string) (*state.TaskSet, error) {
	msg := fmt.Sprintf(i18n.G("Set active %q"), snap)
	t := s.NewTask("activate-snap", msg)
	t.Set("snap-setup", SnapSetup{
		Name: snap,
	})

	return state.NewTaskSet(t), nil
}

// Activate returns a set of tasks for activating a snap.
// Note that the state must be locked by the caller.
func Deactivate(s *state.State, snap string) (*state.TaskSet, error) {
	msg := fmt.Sprintf(i18n.G("Set inactive %q"), snap)
	t := s.NewTask("deactivate-snap", msg)
	t.Set("snap-setup", SnapSetup{
		Name: snap,
	})

	return state.NewTaskSet(t), nil
}

// Retrieval functions

func retrieveInfo(name string, si *snap.SideInfo) (*snap.Info, error) {
	// XXX: move some of this in snap as helper?
	snapYamlFn := filepath.Join(snap.MountDir(name, si.Revision), "meta", "snap.yaml")
	meta, err := ioutil.ReadFile(snapYamlFn)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("cannot find mounted snap %q at revision %d", name, si.Revision)
	}
	if err != nil {
		return nil, err
	}

	info, err := snap.InfoFromSnapYaml(meta)
	if err != nil {
		return nil, err
	}

	info.SideInfo = *si

	return info, nil
}

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
			return retrieveInfo(name, si)
		}
	}

	if snapst.Candidate != nil && snapst.Candidate.Revision == revision {
		return retrieveInfo(name, snapst.Candidate)
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

// Set sets the SnapState of the given snap, overwriting any earlier state.
func Set(s *state.State, name string, snapst *SnapState) {
	var snaps map[string]*json.RawMessage
	err := s.Get("snaps", &snaps)
	if err == state.ErrNoState {
		s.Set("snaps", map[string]*SnapState{name: snapst})
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
