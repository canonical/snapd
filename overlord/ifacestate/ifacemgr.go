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

// Package ifacestate implements the manager and state aspects
// responsible for the maintenance of interfaces the system.
package ifacestate

import (
	"fmt"
	"io/ioutil"
	"path/filepath"

	"gopkg.in/tomb.v2"

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/i18n"
	"github.com/ubuntu-core/snappy/interfaces"
	"github.com/ubuntu-core/snappy/interfaces/builtin"
	"github.com/ubuntu-core/snappy/overlord/state"
	"github.com/ubuntu-core/snappy/snap"
)

// InterfaceManager is responsible for the maintenance of interfaces in
// the system state.  It maintains interface connections, and also observes
// installed snaps to track the current set of available plugs and slots.
type InterfaceManager struct {
	state  *state.State
	runner *state.TaskRunner
	repo   *interfaces.Repository
}

// Manager returns a new InterfaceManager.
func Manager(s *state.State) (*InterfaceManager, error) {
	repo := interfaces.NewRepository()
	for _, iface := range builtin.Interfaces() {
		if err := repo.AddInterface(iface); err != nil {
			return nil, err
		}
	}
	runner := state.NewTaskRunner(s)
	m := &InterfaceManager{
		state:  s,
		runner: runner,
		repo:   repo,
	}

	runner.AddHandler("connect", m.doConnect)
	runner.AddHandler("disconnect", m.doDisconnect)
	runner.AddHandler("discover-ports", m.doDiscoverPorts)
	return m, nil
}

// DiscoverPorts scans all snaps in the system and updates plug/slot repository.
func DiscoverPorts(s *state.State) (*state.TaskSet, error) {
	t := s.NewTask("discover-ports", i18n.G("Looking for plugs and slots"))
	return state.NewTaskSet(t), nil
}

// Connect returns a set of tasks for connecting an interface.
//
func Connect(s *state.State, plugSnap, plugName, slotSnap, slotName string) (*state.TaskSet, error) {
	// TODO: Store the intent-to-connect in the state so that we automatically
	// try to reconnect on reboot (reconnection can fail or can connect with
	// different parameters so we cannot store the actual connection details).
	summary := fmt.Sprintf(i18n.G("Connect %s:%s to %s:%s"),
		plugSnap, plugName, slotSnap, slotName)
	task := s.NewTask("connect", summary)
	task.Set("slot", interfaces.SlotRef{Snap: slotSnap, Name: slotName})
	task.Set("plug", interfaces.PlugRef{Snap: plugSnap, Name: plugName})
	return state.NewTaskSet(task), nil
}

// Disconnect returns a set of tasks for  disconnecting an interface.
func Disconnect(s *state.State, plugSnap, plugName, slotSnap, slotName string) (*state.TaskSet, error) {
	// TODO: Remove the intent-to-connect from the state so that we no longer
	// automatically try to reconnect on reboot.
	summary := fmt.Sprintf(i18n.G("Disconnect %s:%s from %s:%s"),
		plugSnap, plugName, slotSnap, slotName)
	task := s.NewTask("disconnect", summary)
	task.Set("slot", interfaces.SlotRef{Snap: slotSnap, Name: slotName})
	task.Set("plug", interfaces.PlugRef{Snap: plugSnap, Name: plugName})
	return state.NewTaskSet(task), nil
}

func getPlugAndSlotRefs(task *state.Task) (*interfaces.PlugRef, *interfaces.SlotRef, error) {
	var plugRef interfaces.PlugRef
	var slotRef interfaces.SlotRef
	if err := task.Get("plug", &plugRef); err != nil {
		return nil, nil, err
	}
	if err := task.Get("slot", &slotRef); err != nil {
		return nil, nil, err
	}
	return &plugRef, &slotRef, nil
}

func (m *InterfaceManager) doConnect(task *state.Task, _ *tomb.Tomb) error {
	task.State().Lock()
	defer task.State().Unlock()

	plugRef, slotRef, err := getPlugAndSlotRefs(task)
	if err != nil {
		return err
	}
	return m.repo.Connect(plugRef.Snap, plugRef.Name, slotRef.Snap, slotRef.Name)
}

func (m *InterfaceManager) doDisconnect(task *state.Task, _ *tomb.Tomb) error {
	task.State().Lock()
	defer task.State().Unlock()

	plugRef, slotRef, err := getPlugAndSlotRefs(task)
	if err != nil {
		return err
	}
	return m.repo.Disconnect(plugRef.Snap, plugRef.Name, slotRef.Snap, slotRef.Name)
}

func (m *InterfaceManager) doDiscoverPorts(task *state.Task, _ *tomb.Tomb) error {
	task.State().Lock()
	defer task.State().Unlock()

	// XXX: This is a hack until we can ask the state for a list of snaps.
	glob := filepath.Join(dirs.SnapSnapsDir, "*", "*", "meta", "snap.yaml")
	matches, err := filepath.Glob(glob)
	if err != nil {
		task.Errorf("cannot enumerate snaps from %s: %s", dirs.SnapSnapsDir, err)
		return err
	}
	for _, match := range matches {
		yaml, err := ioutil.ReadFile(match)
		if err != nil {
			task.Logf("cannot read snap.yaml from %q: %s", match, err)
			continue
		}
		snapInfo, err := snap.InfoFromSnapYaml(yaml)
		if err != nil {
			task.Logf("cannot parse snap.yaml read from %q: %s", match, err)
			continue
		}
		err = m.doRefreshSnap(task, snapInfo)
		if err != nil {
			task.Errorf("cannot refresh snap %s: %s", snapInfo.Name, err)
			return err
		}
	}
	return nil
}

// doRefreshSnap synchronizes plugs and slots in the repository with what is defined in a given snap.
func (m *InterfaceManager) doRefreshSnap(task *state.Task, snapInfo *snap.Info) error {
	if err := m.doRefreshPlugs(task, snapInfo); err != nil {
		return err
	}
	if err := m.doRefreshSlots(task, snapInfo); err != nil {
		return err
	}
	return nil
}

// doRefreshPlugs synchronizes plugs in the repository with the plugs defined in a given snap.
func (m *InterfaceManager) doRefreshPlugs(task *state.Task, snapInfo *snap.Info) error {
	// Inspect each plug in the repository to see if they need changes as
	// compared to what is in the snap.
	for _, plug := range m.repo.Plugs(snapInfo.Name) {
		plugInfo := snapInfo.Plugs[plug.Name]
		switch {
		case plugInfo == nil:
			// The plug in the repository is no longer in the snap.
			// Remove the plug in the repository.
			if err := m.repo.Disconnect(plug.Snap.Name, plug.Name, "", ""); err != nil {
				task.Errorf("cannot disconnect plug removed from snap %s.%s: %s", plug.Snap.Name, plug.Name, err)
				return err
			}
			if err := m.repo.RemovePlug(plug.Snap.Name, plug.Name); err != nil {
				task.Errorf("cannot remove plug removed from snap %s.%s: %s", plug.Snap.Name, plug.Name, err)
				return err
			}
		case plug.Interface == plugInfo.Interface:
			// The plug in the repository and the plug in the snap have the same interface.
			// Don't disconnect anything, just swap plug information.
			//
			// XXX: This will be bad once we do $attributes or hooks. Perhaps
			// we should disconnect the old one, connect the new one and let
			// those operations fail if necessary.
			plug.PlugInfo = plugInfo
		case plug.Interface != plugInfo.Interface:
			// The plug in the repository and the plug in the snap have changed.
			// Disconnect and replace the plug in the repository.
			if err := m.repo.Disconnect(plug.Snap.Name, plug.Name, "", ""); err != nil {
				task.Errorf("cannot disconnect plug changed in snap %s.%s: %s", plug.Snap.Name, plug.Name, err)
				return err
			}
			plug.PlugInfo = plugInfo
			// TODO: consider auto-connecting the plug again.
		}
	}
	// Inspect each plug in the snap and add plugs to the repository if they
	// are missing.
	for _, plugInfo := range snapInfo.Plugs {
		if plug := m.repo.Plug(plugInfo.Snap.Name, plugInfo.Name); plug == nil {
			plug := &interfaces.Plug{PlugInfo: plugInfo}
			if err := m.repo.AddPlug(plug); err != nil {
				// NOTE: If we cannot add a plug then so be it, it is not a
				// fatal error.  Maybe it is using an interface we don't
				// support. Maybe it is just bogus in some way. In either case
				// just act as if this plug wasn't there.
				task.Logf("cannot add plug %s.%s: %s", plug.Snap.Name, plug.Name, err)
				continue
			}
			// TODO: consider auto-connecting the plug.
		}
	}
	return nil
}

// doRefreshSlots synchronizes slots in the repository with the slots defined in a given snap.
func (m *InterfaceManager) doRefreshSlots(task *state.Task, snapInfo *snap.Info) error {
	// Inspect each slot in the repository to see if they need changes as
	// compared to what is in the snap.
	for _, slot := range m.repo.Slots(snapInfo.Name) {
		slotInfo := snapInfo.Slots[slot.Name]
		switch {
		case slotInfo == nil:
			// The slot in the repository is no longer in the snap.
			// Remove the slot in the repository.
			if err := m.repo.Disconnect("", "", slot.Snap.Name, slot.Name); err != nil {
				task.Errorf("cannot disconnect slot removed from snap %s.%s: %s", slot.Snap.Name, slot.Name, err)
				return err
			}
			if err := m.repo.RemoveSlot(slot.Snap.Name, slot.Name); err != nil {
				task.Errorf("cannot remove slot removed from snap %s.%s: %s", slot.Snap.Name, slot.Name, err)
				return err
			}
		case slot.Interface == slotInfo.Interface:
			// The slot in the repository and the slot in the snap have the same interface.
			// Don't disconnect anything, just swap slot information.
			//
			// XXX: This will be bad once we do $attributes or hooks. Perhaps
			// we should disconnect the old one, connect the new one and let
			// those operations fail if necessary.
			slot.SlotInfo = slotInfo
		case slot.Interface != slotInfo.Interface:
			// The slot in the repository and the slot in the snap have changed.
			// Disconnect and replace the slot in the repository.
			if err := m.repo.Disconnect("", "", slot.Snap.Name, slot.Name); err != nil {
				task.Errorf("cannot disconnect slot changed in snap %s.%s: %s", slot.Snap.Name, slot.Name, err)
				return err
			}
			slot.SlotInfo = slotInfo
			// TODO: consider auto-connecting the slot again.
		}
	}
	// Inspect each slot in the snap and add slots to the repository if they
	// are missing.
	for _, slotInfo := range snapInfo.Slots {
		if slot := m.repo.Slot(slotInfo.Snap.Name, slotInfo.Name); slot == nil {
			slot := &interfaces.Slot{SlotInfo: slotInfo}
			if err := m.repo.AddSlot(slot); err != nil {
				// NOTE: If we cannot add a slot then so be it, it is not a
				// fatal error.  Maybe it is using an interface we don't
				// support. Maybe it is just bogus in some way. In either case
				// just act as if this slot wasn't there.
				task.Logf("cannot add slot %s.%s: %s", slot.Snap.Name, slot.Name, err)
				continue
			}
			// TODO: consider auto-connecting the slot.
		}
	}
	return nil
}

// Ensure implements StateManager.Ensure.
func (m *InterfaceManager) Ensure() error {
	m.runner.Ensure()
	return nil
}

// Wait implements StateManager.Wait.
func (m *InterfaceManager) Wait() {
	m.runner.Wait()
}

// Stop implements StateManager.Stop.
func (m *InterfaceManager) Stop() {
	m.runner.Stop()

}
