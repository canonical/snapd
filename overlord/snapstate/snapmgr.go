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

	"github.com/ubuntu-core/snappy/i18n"
	"github.com/ubuntu-core/snappy/overlord/state"
	"github.com/ubuntu-core/snappy/progress"
	"github.com/ubuntu-core/snappy/snappy"
)

// Install initiates a change installing snap.
// Note that the state must be locked by the caller.
func Install(change *state.Change, snap, channel string, flags snappy.InstallFlags) error {
	t := change.NewTask("install-snap", fmt.Sprintf(i18n.G("Installing %q"), snap))
	t.Set("state", installState{
		Name:    snap,
		Channel: channel,
		Flags:   flags,
	})

	return nil
}

// Remove initiates a change removing snap.
// Note that the state must be locked by the caller.
func Remove(change *state.Change, snap string, flags snappy.RemoveFlags) error {
	t := change.NewTask("remove-snap", fmt.Sprintf(i18n.G("Removing %q"), snap))
	t.Set("state", removeState{
		Name:  snap,
		Flags: flags,
	})

	return nil
}

type backendIF interface {
	Install(name, channel string, flags snappy.InstallFlags, meter progress.Meter) (string, error)
	Remove(name string, flags snappy.RemoveFlags, meter progress.Meter) error
}

type defaultBackend struct{}

func (s *defaultBackend) Install(name, channel string, flags snappy.InstallFlags, meter progress.Meter) (string, error) {
	return snappy.Install(name, channel, flags, meter)
}

func (s *defaultBackend) Remove(name string, flags snappy.RemoveFlags, meter progress.Meter) error {
	return snappy.Remove(name, flags, meter)
}

// SnapManager is responsible for the installation and removal of snaps.
type SnapManager struct {
	state   *state.State
	backend backendIF

	runner *state.TaskRunner
}

type installState struct {
	Name    string              `json:"name"`
	Channel string              `json:"channel"`
	Flags   snappy.InstallFlags `json:"flags,omitempty"`
}

type removeState struct {
	Name  string             `json:"name"`
	Flags snappy.RemoveFlags `json:"flags,omitempty"`
}

// Manager returns a new snap manager.
func Manager() (*SnapManager, error) {
	return &SnapManager{}, nil
}

func (m *SnapManager) doInstallSnap(t *state.Task, _ *tomb.Tomb) error {
	var inst installState
	t.State().Lock()
	if err := t.Get("state", &inst); err != nil {
		return err
	}
	t.State().Unlock()

	_, err := m.backend.Install(inst.Name, inst.Channel, inst.Flags, &progress.NullProgress{})
	t.Logf("doInstallSnap: %s from %s: %v", inst.Name, inst.Channel, err)
	return err
}

func (m *SnapManager) doRemoveSnap(t *state.Task, _ *tomb.Tomb) error {
	var rm removeState

	t.State().Lock()
	if err := t.Get("state", &rm); err != nil {
		return err
	}
	t.State().Unlock()

	name, _ := snappy.SplitDeveloper(rm.Name)
	err := m.backend.Remove(name, rm.Flags, &progress.NullProgress{})
	t.Logf("doRemoveSnap: %s: %v", name, err)
	return err
}

// Init implements StateManager.Init.
func (m *SnapManager) Init(s *state.State) error {
	m.state = s
	m.runner = state.NewTaskRunner(s)
	m.backend = &defaultBackend{}

	m.runner.AddHandler("install-snap", m.doInstallSnap)
	m.runner.AddHandler("remove-snap", m.doRemoveSnap)

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
