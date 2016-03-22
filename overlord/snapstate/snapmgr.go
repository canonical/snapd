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

// Install returns a set of tasks for installing snap.
// Note that the state must be locked by the caller.
func Install(s *state.State, snap, channel string, flags snappy.InstallFlags) (state.TaskSet, error) {
	t := s.NewTask("install-snap", fmt.Sprintf(i18n.G("Installing %q"), snap))
	t.Set("state", installState{
		Name:    snap,
		Channel: channel,
		Flags:   flags,
	})

	return state.NewTaskSet(t), nil
}

// Update initiates a change updating a snap.
// Note that the state must be locked by the caller.
func Update(s *state.State, snap, channel string, flags snappy.InstallFlags) (state.TaskSet, error) {
	t := s.NewTask("update-snap", fmt.Sprintf(i18n.G("Updating %q"), snap))
	t.Set("state", installState{
		Name:    snap,
		Channel: channel,
		Flags:   flags,
	})

	return state.NewTaskSet(t), nil
}

// Remove returns a set of tasks for removing snap.
// Note that the state must be locked by the caller.
func Remove(s *state.State, snap string, flags snappy.RemoveFlags) (state.TaskSet, error) {
	t := s.NewTask("remove-snap", fmt.Sprintf(i18n.G("Removing %q"), snap))
	t.Set("state", removeState{
		Name:  snap,
		Flags: flags,
	})

	return state.NewTaskSet(t), nil
}

// Purge returns a set of tasks for purging a snap.
// Note that the state must be locked by the caller.
func Purge(s *state.State, snap string, flags snappy.PurgeFlags) (state.TaskSet, error) {
	t := s.NewTask("purge-snap", fmt.Sprintf(i18n.G("Purging %q"), snap))
	t.Set("state", purgeState{
		Name:  snap,
		Flags: flags,
	})

	return state.NewTaskSet(t), nil
}

type backendIF interface {
	Install(name, channel string, flags snappy.InstallFlags, meter progress.Meter) (string, error)
	Update(name, channel string, flags snappy.InstallFlags, meter progress.Meter) error
	Remove(name string, flags snappy.RemoveFlags, meter progress.Meter) error
	Purge(name string, flags snappy.PurgeFlags, meter progress.Meter) error
}

type defaultBackend struct{}

func (s *defaultBackend) Install(name, channel string, flags snappy.InstallFlags, meter progress.Meter) (string, error) {
	return snappy.Install(name, channel, flags, meter)
}

func (s *defaultBackend) Update(name, channel string, flags snappy.InstallFlags, meter progress.Meter) error {
	// FIXME: support "channel" in snappy.Update()
	_, err := snappy.Update(name, flags, meter)
	return err
}

func (s *defaultBackend) Remove(name string, flags snappy.RemoveFlags, meter progress.Meter) error {
	return snappy.Remove(name, flags, meter)
}

func (s *defaultBackend) Purge(name string, flags snappy.PurgeFlags, meter progress.Meter) error {
	return snappy.Purge(name, flags, meter)
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

type purgeState struct {
	Name  string            `json:"name"`
	Flags snappy.PurgeFlags `json:"flags,omitempty"`
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

	runner.AddHandler("install-snap", m.doInstallSnap)
	runner.AddHandler("update-snap", m.doUpdateSnap)
	runner.AddHandler("remove-snap", m.doRemoveSnap)
	runner.AddHandler("purge-snap", m.doPurgeSnap)

	// test handlers
	runner.AddHandler("fake-install-snap", func(t *state.Task, _ *tomb.Tomb) error {
		return nil
	})
	runner.AddHandler("fake-install-snap-error", func(t *state.Task, _ *tomb.Tomb) error {
		return fmt.Errorf("fake-install-snap-error errored")
	})

	return m, nil
}

func (m *SnapManager) doInstallSnap(t *state.Task, _ *tomb.Tomb) error {
	var inst installState
	t.State().Lock()
	if err := t.Get("state", &inst); err != nil {
		return err
	}
	t.State().Unlock()

	_, err := m.backend.Install(inst.Name, inst.Channel, inst.Flags, &progress.NullProgress{})
	return err
}

func (m *SnapManager) doUpdateSnap(t *state.Task, _ *tomb.Tomb) error {
	var inst installState
	t.State().Lock()
	if err := t.Get("state", &inst); err != nil {
		return err
	}
	t.State().Unlock()

	err := m.backend.Update(inst.Name, inst.Channel, inst.Flags, &progress.NullProgress{})
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
	return err
}

func (m *SnapManager) doPurgeSnap(t *state.Task, _ *tomb.Tomb) error {
	var purge purgeState

	t.State().Lock()
	if err := t.Get("state", &purge); err != nil {
		return err
	}
	t.State().Unlock()

	name, _ := snappy.SplitDeveloper(purge.Name)
	err := m.backend.Purge(name, purge.Flags, &progress.NullProgress{})
	return err
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
