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

	"github.com/ubuntu-core/snappy/overlord/state"
	"github.com/ubuntu-core/snappy/progress"
	"github.com/ubuntu-core/snappy/snappy"
)

// Install initiates a change installing snap.
func Install(change *state.Change, snap string) error {
	change.State().Lock()
	defer change.State().Unlock()

	tIns := change.NewTask("install-snap", fmt.Sprintf("Installing %q", snap))
	tIns.Set("name", snap)

	return nil
}

// Remove initiates a change removing snap.
func Remove(change *state.Change, snap string) error {
	change.State().Lock()
	defer change.State().Unlock()

	t := change.NewTask("remove-snap", fmt.Sprintf("Removing %q", snap))
	t.Set("name", snap)

	return nil
}

// SnapManager is responsible for the installation and removal of snaps.
type SnapManager struct {
	state *state.State

	runner *state.TaskRunner
}

// Manager returns a new snap manager.
func Manager() (*SnapManager, error) {
	return &SnapManager{}, nil
}

func (m *SnapManager) doInstallSnap(t *state.Task) error {
	var name string
	t.Get("name", &name)
	channel := ""
	_, err := snappy.Install(name, channel, 0, &progress.NullProgress{})
	return err
}

func (m *SnapManager) doRemoveSnap(t *state.Task) error {
	var name string
	t.Get("name", &name)
	return snappy.Remove(name, 0, &progress.NullProgress{})
}

// Init implements StateManager.Init.
func (m *SnapManager) Init(s *state.State) error {
	m.state = s
	m.runner = state.NewTaskRunner(s)

	m.runner.AddHandler("install-snap", m.doInstallSnap)
	m.runner.AddHandler("remove-snap", m.doRemoveSnap)

	return nil
}

// Ensure implements StateManager.Ensure.
func (m *SnapManager) Ensure() error {
	m.runner.Ensure()
	return nil
}

// Stop implements StateManager.Stop.
func (m *SnapManager) Stop() error {
	m.runner.Stop()
	return nil
}
