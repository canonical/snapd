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
	"io/ioutil"
	"path/filepath"

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

type snapSetup struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Channel string `json:"channel"`

	OldName    string `json:"old-name"`
	OldVersion string `json:"old-version"`

	InstallFlags snappy.InstallFlags `json:"inst-flags,omitempty"`
	RemoveFlags  snappy.RemoveFlags  `json:"rm-flags,omitempty"`

	SnapPath string `json:"snap-path"`
}

func (s *snapSetup) BaseDir() string {
	name, _ := snappy.SplitDeveloper(s.Name)
	return filepath.Join(dirs.SnapSnapsDir, name, s.Version)
}

func (s *snapSetup) OldBaseDir() string {
	if s.OldName == "" || s.OldVersion == "" {
		return ""
	}
	oldname, _ := snappy.SplitDeveloper(s.OldName)
	return filepath.Join(dirs.SnapSnapsDir, oldname, s.OldVersion)
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

	runner.AddHandler("download-snap", m.doDownloadSnap, nil)
	runner.AddHandler("mount-snap", m.doMountSnap, m.undoMountSnap)
	runner.AddHandler("copy-snap-data", m.doCopySnapData, m.undoCopySnapData)
	runner.AddHandler("setup-snap-security", m.doSetupSnapSecurity, m.undoSetupSnapSecurity)
	runner.AddHandler("link-snap", m.doLinkSnap, m.undoLinkSnap)

	runner.AddHandler("update-snap", m.doUpdateSnap, nil)
	runner.AddHandler("remove-snap", m.doRemoveSnap, nil)
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

func (m *SnapManager) doDownloadSnap(t *state.Task, _ *tomb.Tomb) error {
	var ss snapSetup

	t.State().Lock()
	err := t.Get("snap-setup", &ss)
	t.State().Unlock()
	if err != nil {
		return err
	}

	pb := &TaskProgressAdapter{task: t}
	downloadedSnapFile, version, err := m.backend.Download(ss.Name, ss.Channel, pb)
	if err != nil {
		return err
	}
	ss.SnapPath = downloadedSnapFile
	ss.Version = version

	oldInstPath, _ := filepath.EvalSymlinks(filepath.Join(ss.BaseDir(), "..", "current", "meta", "snap.yaml"))
	if sn, err := snappy.NewInstalledSnap(oldInstPath); err == nil {
		ss.OldName = sn.Name()
		ss.OldVersion = sn.Version()
	}

	// update snap-setup for the following tasks
	t.State().Lock()
	t.Set("snap-setup", ss)
	t.State().Unlock()

	return nil
}

func (m *SnapManager) doUpdateSnap(t *state.Task, _ *tomb.Tomb) error {
	var ss snapSetup

	t.State().Lock()
	err := t.Get("snap-setup", &ss)
	t.State().Unlock()
	if err != nil {
		return err
	}

	pb := &TaskProgressAdapter{task: t}
	return m.backend.Update(ss.Name, ss.Channel, ss.InstallFlags, pb)
}

func (m *SnapManager) doRemoveSnap(t *state.Task, _ *tomb.Tomb) error {
	var ss snapSetup

	t.State().Lock()
	err := t.Get("snap-setup", &ss)
	t.State().Unlock()
	if err != nil {
		return err
	}

	pb := &TaskProgressAdapter{task: t}
	return m.backend.Remove(ss.Name, ss.RemoveFlags, pb)
}

func (m *SnapManager) doRollbackSnap(t *state.Task, _ *tomb.Tomb) error {
	var ss snapSetup

	t.State().Lock()
	err := t.Get("snap-setup", &ss)
	t.State().Unlock()
	if err != nil {
		return err
	}

	pb := &TaskProgressAdapter{task: t}
	_, err = m.backend.Rollback(ss.Name, ss.Version, pb)
	return err
}

func (m *SnapManager) doActivateSnap(t *state.Task, _ *tomb.Tomb) error {
	var ss snapSetup

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
	var ss snapSetup

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

func getSnapSetup(t *state.Task, ss *snapSetup) error {
	var id string

	st := t.State()
	st.Lock()
	err := t.Get("snap-setup-task", &id)
	st.Unlock()
	if err != nil {
		return err
	}

	st.Lock()
	ts := st.Task(id)
	err = ts.Get("snap-setup", ss)
	st.Unlock()

	return err
}

func (m *SnapManager) undoMountSnap(t *state.Task, _ *tomb.Tomb) error {
	var ss snapSetup
	if err := getSnapSetup(t, &ss); err != nil {
		return err
	}

	return m.backend.UndoSetupSnap(ss.BaseDir())
}

func (m *SnapManager) doMountSnap(t *state.Task, _ *tomb.Tomb) error {
	var ss snapSetup
	if err := getSnapSetup(t, &ss); err != nil {
		return err
	}

	if err := m.backend.CheckSnap(ss.SnapPath, ss.InstallFlags); err != nil {
		return err
	}

	return m.backend.SetupSnap(ss.SnapPath, ss.InstallFlags)
}

func (m *SnapManager) undoSetupSnapSecurity(t *state.Task, _ *tomb.Tomb) error {
	var ss snapSetup
	if err := getSnapSetup(t, &ss); err != nil {
		return err
	}

	return m.backend.UndoSetupSnapSecurity(ss.BaseDir())
}

func (m *SnapManager) doSetupSnapSecurity(t *state.Task, _ *tomb.Tomb) error {
	var ss snapSetup
	if err := getSnapSetup(t, &ss); err != nil {
		return err
	}

	return m.backend.SetupSnapSecurity(ss.BaseDir())
}

func (m *SnapManager) undoCopySnapData(t *state.Task, _ *tomb.Tomb) error {
	var ss snapSetup
	if err := getSnapSetup(t, &ss); err != nil {
		return err
	}

	return m.backend.UndoCopySnapData(ss.BaseDir(), ss.InstallFlags)
}

func (m *SnapManager) doCopySnapData(t *state.Task, _ *tomb.Tomb) error {
	var ss snapSetup
	if err := getSnapSetup(t, &ss); err != nil {
		return err
	}

	return m.backend.CopySnapData(ss.BaseDir(), ss.InstallFlags)
}
func (m *SnapManager) doLinkSnap(t *state.Task, _ *tomb.Tomb) error {
	var ss snapSetup
	if err := getSnapSetup(t, &ss); err != nil {
		return err
	}

	return m.backend.LinkSnap(ss.BaseDir())
}

func (m *SnapManager) undoLinkSnap(t *state.Task, _ *tomb.Tomb) error {
	var ss snapSetup
	if err := getSnapSetup(t, &ss); err != nil {
		return err
	}

	return m.backend.UndoLinkSnap(ss.OldBaseDir(), ss.BaseDir())
}

// SnapInfo returns the snap.Info for a snap in the system.
//
// Today this function is looking at data directly from the mounted snap, but soon it will
// be changed so it looks first at the state for the snap details (Revision, Developer, etc),
// and then complements it with information from the snap itself.
func SnapInfo(state *state.State, snapName, snapVersion string) (*snap.Info, error) {
	fname := filepath.Join(dirs.SnapSnapsDir, snapName, snapVersion, "meta", "snap.yaml")
	yamlData, err := ioutil.ReadFile(fname)
	if err != nil {
		return nil, err
	}
	info, err := snap.InfoFromSnapYaml(yamlData)
	if err != nil {
		return nil, err
	}
	// Overwrite the name which doesn't belong in snap.yaml and is actually
	// defined by snap declaration assertion.
	info.Name = snapName
	// TODO: use state to retrieve additional information
	return info, nil
}
