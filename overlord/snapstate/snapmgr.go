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

	"github.com/ubuntu-core/snappy/overlord/state"
	"github.com/ubuntu-core/snappy/snappy"
)

// SnapManager is responsible for the installation and removal of snaps.
type SnapManager struct {
	state   *state.State
	backend managerBackend

	runner *state.TaskRunner
}

type installState struct {
	Name    string              `json:"name"`
	Channel string              `json:"channel"`
	Flags   snappy.InstallFlags `json:"flags,omitempty"`

	DownloadTaskID string `json:"download-task-id,omitempty"`
	SnapPath       string `json:"snap-path,omitempty"`
}

type downloadState struct {
	Developer string `json:"developer"`
	SnapPath  string `json:"snap-path,omitempty"`
}

type setupState struct {
	InstallPath string `json:"install-path"`
}

type removeState struct {
	Name  string             `json:"name"`
	Flags snappy.RemoveFlags `json:"flags,omitempty"`
}

type purgeState struct {
	Name  string            `json:"name"`
	Flags snappy.PurgeFlags `json:"flags,omitempty"`
}

type rollbackState struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

type activateState struct {
	Name   string `json:"name"`
	Active bool   `json:"active"`
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
	})

	runner.AddHandler("download-snap", m.doDownloadSnap)
	runner.AddHandler("check-snap", m.doCheckSnap)
	runner.AddHandler("mount-snap", m.doMountSnap)
	runner.AddHandler("copy-snap-data", m.doCopySnapData)
	runner.AddHandler("generate-security", m.doGenerateSecurity)
	runner.AddHandler("finalize-snap-install", m.doFinalizeSnap)

	runner.AddHandler("update-snap", m.doUpdateSnap)
	runner.AddHandler("remove-snap", m.doRemoveSnap)
	runner.AddHandler("purge-snap", m.doPurgeSnap)
	runner.AddHandler("rollback-snap", m.doRollbackSnap)
	runner.AddHandler("activate-snap", m.doActivateSnap)

	// test handlers
	runner.AddHandler("fake-install-snap", func(t *state.Task, _ *tomb.Tomb) error {
		return nil
	})
	runner.AddHandler("fake-install-snap-error", func(t *state.Task, _ *tomb.Tomb) error {
		return fmt.Errorf("fake-install-snap-error errored")
	})

	return m, nil
}

func (m *SnapManager) doDownloadSnap(t *state.Task, _ *tomb.Tomb) error {
	var inst installState
	var dl downloadState

	t.State().Lock()
	err := t.Get("install-state", &inst)
	t.State().Unlock()
	if err != nil {
		return err
	}

	pb := &TaskProgressAdapter{task: t}
	downloadedSnapFile, developer, err := m.backend.Download(inst.Name, inst.Channel, pb)
	if err != nil {
		return err
	}
	dl.SnapPath = downloadedSnapFile
	dl.Developer = developer

	// update instState for the next task
	t.State().Lock()
	t.Set("download-state", dl)
	t.State().Unlock()

	return nil
}

func (m *SnapManager) doUpdateSnap(t *state.Task, _ *tomb.Tomb) error {
	var inst installState
	t.State().Lock()
	err := t.Get("update-state", &inst)
	t.State().Unlock()
	if err != nil {
		return err
	}

	pb := &TaskProgressAdapter{task: t}
	return m.backend.Update(inst.Name, inst.Channel, inst.Flags, pb)
}

func (m *SnapManager) doRemoveSnap(t *state.Task, _ *tomb.Tomb) error {
	var rm removeState

	t.State().Lock()
	err := t.Get("remove-state", &rm)
	t.State().Unlock()
	if err != nil {
		return err
	}

	pb := &TaskProgressAdapter{task: t}
	name, _ := snappy.SplitDeveloper(rm.Name)
	return m.backend.Remove(name, rm.Flags, pb)
}

func (m *SnapManager) doPurgeSnap(t *state.Task, _ *tomb.Tomb) error {
	var purge purgeState

	t.State().Lock()
	err := t.Get("purge-state", &purge)
	t.State().Unlock()
	if err != nil {
		return err
	}

	pb := &TaskProgressAdapter{task: t}
	name, _ := snappy.SplitDeveloper(purge.Name)
	return m.backend.Purge(name, purge.Flags, pb)
}

func (m *SnapManager) doRollbackSnap(t *state.Task, _ *tomb.Tomb) error {
	var rollback rollbackState

	t.State().Lock()
	err := t.Get("rollback-state", &rollback)
	t.State().Unlock()
	if err != nil {
		return err
	}

	pb := &TaskProgressAdapter{task: t}
	name, _ := snappy.SplitDeveloper(rollback.Name)
	_, err = m.backend.Rollback(name, rollback.Version, pb)
	return err
}

func (m *SnapManager) doActivateSnap(t *state.Task, _ *tomb.Tomb) error {
	var activate activateState

	t.State().Lock()
	err := t.Get("activate-state", &activate)
	t.State().Unlock()
	if err != nil {
		return err
	}

	pb := &TaskProgressAdapter{task: t}
	name, _ := snappy.SplitDeveloper(activate.Name)
	return m.backend.Activate(name, activate.Active, pb)
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

// helper to find the path and developer from an installState
// (it may either be a local snap or downloaded from the store)
func snapPathAndDeveloperFromInstState(t *state.Task, inst *installState) (string, string, error) {
	if inst.SnapPath != "" {
		return inst.SnapPath, snappy.SideloadedDeveloper, nil
	} else if inst.DownloadTaskID != "" {
		var dl downloadState

		t.State().Lock()
		tDl := t.State().Task(inst.DownloadTaskID)
		err := tDl.Get("download-state", &dl)
		t.State().Unlock()
		if err != nil {
			return "", "", err
		}
		return dl.SnapPath, dl.Developer, nil
	}

	return "", "", fmt.Errorf("internal error: installState created without a snap path source")
}

func getSnapSetupState(t *state.Task, setup *setupState) error {
	var id string
	t.State().Lock()
	err := t.Get("setup-snap-id", &id)
	t.State().Unlock()
	if err != nil {
		return nil
	}
	t.State().Lock()
	ts := t.State().Task(id)
	err = ts.Get("setup-state", setup)
	t.State().Unlock()

	return err
}

func (m *SnapManager) doCheckSnap(t *state.Task, _ *tomb.Tomb) error {
	var inst installState

	t.State().Lock()
	err := t.Get("install-state", &inst)
	t.State().Unlock()
	if err != nil {
		return err
	}

	snapPath, developer, err := snapPathAndDeveloperFromInstState(t, &inst)
	if err != nil {
		return err
	}

	return m.backend.CheckSnap(snapPath, developer, inst.Flags)
}

func (m *SnapManager) doMountSnap(t *state.Task, _ *tomb.Tomb) error {
	var inst installState

	t.State().Lock()
	err := t.Get("install-state", &inst)
	t.State().Unlock()
	if err != nil {
		return err
	}

	snapPath, developer, err := snapPathAndDeveloperFromInstState(t, &inst)
	if err != nil {
		return err
	}

	instPath, err := m.backend.SetupSnap(snapPath, developer, inst.Flags)
	if err != nil {
		return err
	}

	t.State().Lock()
	t.Set("setup-state", &setupState{InstallPath: instPath})
	t.State().Unlock()

	return nil
}

func (m *SnapManager) doGenerateSecurity(t *state.Task, _ *tomb.Tomb) error {
	var inst installState
	var setup setupState

	t.State().Lock()
	err := t.Get("install-state", &inst)
	t.State().Unlock()
	if err != nil {
		return err
	}
	_, developer, err := snapPathAndDeveloperFromInstState(t, &inst)
	if err != nil {
		return err
	}
	if err := getSnapSetupState(t, &setup); err != nil {
		return err
	}

	return m.backend.GenerateSecurityProfile(setup.InstallPath, developer)
}

func (m *SnapManager) doCopySnapData(t *state.Task, _ *tomb.Tomb) error {
	var inst installState
	var setup setupState

	t.State().Lock()
	err := t.Get("install-state", &inst)
	t.State().Unlock()
	if err != nil {
		return err
	}

	_, developer, err := snapPathAndDeveloperFromInstState(t, &inst)
	if err != nil {
		return err
	}
	if err := getSnapSetupState(t, &setup); err != nil {
		return err
	}

	return m.backend.CopySnapData(setup.InstallPath, developer, inst.Flags)
}

func (m *SnapManager) doFinalizeSnap(t *state.Task, _ *tomb.Tomb) error {
	var inst installState
	var setup setupState

	t.State().Lock()
	err := t.Get("install-state", &inst)
	t.State().Unlock()
	if err != nil {
		return err
	}

	_, developer, err := snapPathAndDeveloperFromInstState(t, &inst)
	if err != nil {
		return err
	}
	if err := getSnapSetupState(t, &setup); err != nil {
		return err
	}

	return m.backend.FinalizeSnap(setup.InstallPath, developer, inst.Flags)
}
