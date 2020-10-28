// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package exportstate

import (
	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

// doExportContent is the do handler of export-content task.
func (m *ExportManager) doExportContent(task *state.Task, tomb *tomb.Tomb) error {
	st := task.State()
	st.Lock()
	defer st.Unlock()

	perfTimings := state.TimingsForTask(task)
	defer perfTimings.Save(st)

	// Compute the snap revision export state for the snap revision we are working with.
	snapsup, err := snapstate.TaskSnapSetup(task)
	if err != nil {
		return err
	}
	info, err := snap.ReadInfo(snapsup.InstanceName(), snapsup.SideInfo)
	if err != nil {
		return err
	}
	manifest := NewManifestForSnap(info)
	if manifest.IsEmpty() {
		// Most snaps do not export any content.
		return nil
	}

	// Create exported files, removing partial state on failure.
	if err := createExportedFiles(manifest); err != nil {
		removeExportedFiles(manifest)
		return err
	}

	// Remember what we stored in the state for both undo and unexport.
	Set(st, info.InstanceName(), info.Revision, manifest)
	return nil
}

// undoExportContent is the undo handler of export-content task.
func (m *ExportManager) undoExportContent(task *state.Task, tomb *tomb.Tomb) error {
	// To undo export content we just unexport the content.
	// XXX: storing old-manifest seems wasteful but harmless.
	return m.doUnexportContent(task, tomb)
}

// doUnexportContent is the do handler of unexport-content task.
func (m *ExportManager) doUnexportContent(task *state.Task, tomb *tomb.Tomb) error {
	st := task.State()
	st.Lock()
	defer st.Unlock()

	perfTimings := state.TimingsForTask(task)
	defer perfTimings.Save(st)

	// Get the snap revision export state for the snap revision we are working with.
	snapsup, err := snapstate.TaskSnapSetup(task)
	if err != nil {
		return err
	}
	var manifest Manifest
	err = Get(st, snapsup.InstanceName(), snapsup.Revision(), &manifest)
	if err == state.ErrNoState {
		// Most snaps do not export any content.
		return nil
	}
	if err != nil {
		return err
	}
	if err := removeExportedFiles(&manifest); err != nil {
		return err
	}

	// Forget what was exported, keeping it in task state for undo.
	task.Set("old-manifest", &manifest)
	Set(st, snapsup.InstanceName(), snapsup.Revision(), nil)
	task.SetStatus(state.DoneStatus)
	return nil
}

// undoUnexportContent is the undo handler of unexport-content task.
func (m *ExportManager) undoUnexportContent(task *state.Task, tomb *tomb.Tomb) error {
	st := task.State()
	st.Lock()
	defer st.Unlock()

	perfTimings := state.TimingsForTask(task)
	defer perfTimings.Save(st)

	// Get the snap revision export state from the task state information.
	snapsup, err := snapstate.TaskSnapSetup(task)
	if err != nil {
		return err
	}
	var manifest Manifest
	err = task.Get("old-manifest", &manifest)
	if err == state.ErrNoState {
		// Most snaps do not export any content.
		return nil
	}
	if err != nil {
		return err
	}
	if err := createExportedFiles(&manifest); err != nil {
		return err
	}
	// Remember that undo worked.
	Set(st, snapsup.InstanceName(), snapsup.Revision(), &manifest)
	return nil
}
