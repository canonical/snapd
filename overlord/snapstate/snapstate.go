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
	inst := installState{
		Name:    snap,
		Channel: channel,
		Flags:   flags,
	}

	// download (if needed)
	var download *state.Task
	if !osutil.FileExists(snap) {
		download = s.NewTask("download-snap", fmt.Sprintf(i18n.G("Downloading %q"), snap))
		inst.DownloadTaskID = download.ID()
	} else {
		download = s.NewTask("nop", "")
		inst.SnapPath = snap
	}
	download.Set("install-state", inst)

	// check
	check := s.NewTask("check-snap", fmt.Sprintf(i18n.G("Checking %q"), snap))
	check.Set("install-state", inst)
	check.WaitFor(download)

	// mount
	mount := s.NewTask("mount-snap", fmt.Sprintf(i18n.G("Mounting %q"), snap))
	mount.Set("install-state", inst)
	mount.WaitFor(check)

	// security
	generateSecurity := s.NewTask("generate-security", fmt.Sprintf(i18n.G("Generating security profile for %q"), snap))
	generateSecurity.Set("install-state", inst)
	generateSecurity.Set("setup-snap-id", mount.ID())
	generateSecurity.WaitFor(mount)

	// copy-data (needs to stop services)
	copyData := s.NewTask("copy-snap-data", fmt.Sprintf(i18n.G("Copying snap data for %q"), snap))
	copyData.Set("install-state", inst)
	copyData.Set("setup-snap-id", mount.ID())
	copyData.WaitFor(generateSecurity)

	// finalize: update current symlink, start new services
	finalize := s.NewTask("finalize-snap-install", fmt.Sprintf(i18n.G("Finalizing install of %q"), snap))
	finalize.Set("install-state", inst)
	finalize.Set("setup-snap-id", mount.ID())
	finalize.WaitFor(copyData)

	return state.NewTaskSet(download, check, mount, generateSecurity, copyData, finalize), nil
}

// Update initiates a change updating a snap.
// Note that the state must be locked by the caller.
func Update(s *state.State, snap, channel string, flags snappy.InstallFlags) (*state.TaskSet, error) {
	t := s.NewTask("update-snap", fmt.Sprintf(i18n.G("Updating %q"), snap))
	t.Set("update-state", installState{
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

// Purge returns a set of tasks for purging a snap.
// Note that the state must be locked by the caller.
func Purge(s *state.State, snap string, flags snappy.PurgeFlags) (*state.TaskSet, error) {
	t := s.NewTask("purge-snap", fmt.Sprintf(i18n.G("Purging %q"), snap))
	t.Set("purge-state", purgeState{
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
