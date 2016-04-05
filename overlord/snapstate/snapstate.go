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

	"github.com/ubuntu-core/snappy/i18n"
	"github.com/ubuntu-core/snappy/osutil"
	"github.com/ubuntu-core/snappy/overlord/state"
	"github.com/ubuntu-core/snappy/snappy"
)

// Install returns a set of tasks for installing snap.
// Note that the state must be locked by the caller.
func Install(s *state.State, snap, channel string, flags snappy.InstallFlags) (*state.TaskSet, error) {
	// download
	var download *state.Task
	dl := downloadState{
		Name:    snap,
		Channel: channel,
		Flags:   flags,
	}
	if !osutil.FileExists(snap) {
		download = s.NewTask("download-snap", fmt.Sprintf(i18n.G("Downloading %q"), snap))
	} else {
		download = s.NewTask("nop", "")
		dl.SnapPath = snap
	}
	download.Set("download-state", dl)

	// mount
	mount := s.NewTask("mount-snap", fmt.Sprintf(i18n.G("Mounting %q"), snap))
	mount.Set("download-snap-id", download.ID())
	mount.WaitFor(download)

	// copy-data (needs to stop services)
	copyData := s.NewTask("copy-snap-data", fmt.Sprintf(i18n.G("Copying snap data for %q"), snap))
	copyData.Set("mount-snap-id", mount.ID())
	copyData.WaitFor(mount)

	// security
	setupSecurity := s.NewTask("setup-snap-security", fmt.Sprintf(i18n.G("Setting up security profile for %q"), snap))
	setupSecurity.Set("mount-snap-id", mount.ID())
	setupSecurity.WaitFor(copyData)

	// finalize (wrappers+current symlink)
	linkSnap := s.NewTask("link-snap", fmt.Sprintf(i18n.G("Final step for %q"), snap))
	linkSnap.Set("mount-snap-id", mount.ID())
	linkSnap.WaitFor(setupSecurity)

	return state.NewTaskSet(download, mount, copyData, setupSecurity, linkSnap), nil
}

// Update initiates a change updating a snap.
// Note that the state must be locked by the caller.
func Update(s *state.State, snap, channel string, flags snappy.InstallFlags) (*state.TaskSet, error) {
	t := s.NewTask("update-snap", fmt.Sprintf(i18n.G("Updating %q"), snap))
	t.Set("update-state", downloadState{
		Name:    snap,
		Channel: channel,
		Flags:   flags,
	})

	return state.NewTaskSet(t), nil
}

// Remove returns a set of tasks for removing snap.
// Note that the state must be locked by the caller.
func Remove(s *state.State, snap string, flags snappy.RemoveFlags) (*state.TaskSet, error) {
	t := s.NewTask("remove-snap", fmt.Sprintf(i18n.G("Removing %q"), snap))
	t.Set("remove-state", removeState{
		Name:  snap,
		Flags: flags,
	})

	return state.NewTaskSet(t), nil
}

// Rollback returns a set of tasks for rolling back a snap.
// Note that the state must be locked by the caller.
func Rollback(s *state.State, snap, ver string) (*state.TaskSet, error) {
	t := s.NewTask("rollback-snap", fmt.Sprintf(i18n.G("Rolling back %q"), snap))
	t.Set("rollback-state", rollbackState{
		Name:    snap,
		Version: ver,
	})

	return state.NewTaskSet(t), nil
}

// Activate returns a set of tasks for activating/deactivating a snap.
// Note that the state must be locked by the caller.
func Activate(s *state.State, snap string, active bool) (*state.TaskSet, error) {
	var msg string
	if active {
		msg = fmt.Sprintf(i18n.G("Set active %q"), snap)
	} else {
		msg = fmt.Sprintf(i18n.G("Set inactive %q"), snap)
	}
	t := s.NewTask("activate-snap", msg)
	t.Set("activate-state", activateState{
		Name:   snap,
		Active: active,
	})

	return state.NewTaskSet(t), nil
}
