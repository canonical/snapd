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

	"gopkg.in/tomb.v2"

	"github.com/ubuntu-core/snappy/i18n"
	"github.com/ubuntu-core/snappy/interfaces"
	"github.com/ubuntu-core/snappy/interfaces/apparmor"
	"github.com/ubuntu-core/snappy/interfaces/builtin"
	"github.com/ubuntu-core/snappy/interfaces/dbus"
	"github.com/ubuntu-core/snappy/interfaces/seccomp"
	"github.com/ubuntu-core/snappy/interfaces/udev"
	"github.com/ubuntu-core/snappy/logger"
	"github.com/ubuntu-core/snappy/overlord/snapstate"
	"github.com/ubuntu-core/snappy/overlord/state"
	"github.com/ubuntu-core/snappy/snap"
	"github.com/ubuntu-core/snappy/snappy"
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
	if err := m.addSnaps(); err != nil {
		return nil, err
	}
	runner.AddHandler("connect", m.doConnect, nil)
	runner.AddHandler("disconnect", m.doDisconnect, nil)
	runner.AddHandler("setup-snap-security", m.doSetupSnapSecurity, m.doRemoveSnapSecurity)
	runner.AddHandler("remove-snap-security", m.doRemoveSnapSecurity, m.doSetupSnapSecurity)
	return m, nil
}

func (m *InterfaceManager) addSnaps() error {
	snaps, err := xxxHackyInstalledSnaps()
	if err != nil {
		return err
	}
	for _, snapInfo := range snaps {
		snap.AddImplicitSlots(snapInfo)
		if err := m.repo.AddSnap(snapInfo); err != nil {
			logger.Noticef("%s", err)
		}
	}
	return nil
}

func xxxHackyInstalledSnaps() ([]*snap.Info, error) {
	installed, err := (&snappy.Overlord{}).Installed()
	if err != nil {
		return nil, err
	}
	snaps := make([]*snap.Info, len(installed))
	for i, legacySnap := range installed {
		snaps[i] = legacySnap.Info()
	}
	return snaps, nil
}

func (m *InterfaceManager) doSetupSnapSecurity(task *state.Task, _ *tomb.Tomb) error {
	task.State().Lock()
	defer task.State().Unlock()

	// Get snap.Info from bits handed by the snap manager.
	ss, err := snapstate.TaskSnapSetup(task)
	if err != nil {
		return err
	}
	snapInfo, err := snapstate.SnapInfo(task.State(), ss.Name, ss.Revision)
	if err != nil {
		return err
	}
	snapName := snapInfo.Name()

	// The snap may have been updated so perform the following operation to
	// ensure that we are always working on the correct state:
	//
	// - disconnect all connections to/from the given snap
	//   - remembering the snaps that were affected by this operation
	// - remove the (old) snap from the interfaces repository
	// - add the (new) snap to the interfaces repository
	// - restore connections based on what is kept in the state
	//   - if a connection cannot be restored then remove it from the state
	// - setup the security of all the affected snaps
	affectedSnaps, err := m.repo.DisconnectSnap(snapName)
	if err != nil {
		return err
	}
	// XXX: what about snap renames? We should remove the old name (or switch
	// to IDs in the interfaces repository)
	if err := m.repo.RemoveSnap(snapName); err != nil {
		return err
	}
	if err := m.repo.AddSnap(snapInfo); err != nil {
		if _, ok := err.(*interfaces.BadInterfacesError); ok {
			logger.Noticef("%s", err)
		} else {
			return err
		}
	}
	if err := m.autoConnect(task, snapName); err != nil {
		return err
	}
	// TODO: re-connect all connection affecting given snap
	// TODO:  - removing failed connections from the state
	if len(affectedSnaps) == 0 {
		affectedSnaps = append(affectedSnaps, snapInfo)
	}
	for _, snapInfo := range affectedSnaps {
		for _, backend := range securityBackendsForSnap(snapInfo) {
			developerMode := false // TODO: move this to snap.Info
			if err := backend.Setup(snapInfo, developerMode, m.repo); err != nil {
				return state.Retry
			}
		}
	}
	return nil
}

type connState struct {
	Auto      bool   `json:"auto,omitempty"`
	Interface string `json:"interface,omitempty"`
}

func (m *InterfaceManager) autoConnect(task *state.Task, snapName string) error {
	var conns map[string]connState
	err := task.State().Get("conns", &conns)
	if err != nil && err != state.ErrNoState {
		return err
	}
	if conns == nil {
		conns = make(map[string]connState)
	}
	// XXX: quick hack, auto-connect everything
	for _, plug := range m.repo.Plugs(snapName) {
		candidates := m.repo.AutoConnectCandidates(snapName, plug.Name)
		if len(candidates) != 1 {
			continue
		}
		slot := candidates[0]
		if err := m.repo.Connect(snapName, plug.Name, slot.Snap.Name(), slot.Name); err != nil {
			task.Logf("cannot auto connect %s:%s to %s:%s: %s",
				snapName, plug.Name, slot.Snap.Name(), slot.Name, err)
		}
		key := fmt.Sprintf("%s:%s %s:%s", snapName, plug.Name, slot.Snap.Name(), slot.Name)
		conns[key] = connState{Interface: plug.Interface, Auto: true}
	}
	task.State().Set("conns", conns)
	return nil
}

func (m *InterfaceManager) doRemoveSnapSecurity(task *state.Task, _ *tomb.Tomb) error {
	task.State().Lock()
	defer task.State().Unlock()

	// Get snap.Info from bits handed by the snap manager.
	ss, err := snapstate.TaskSnapSetup(task)
	if err != nil {
		return err
	}
	snapInfo, err := snapstate.SnapInfo(task.State(), ss.Name, ss.Revision)
	if err != nil {
		return err
	}
	snapName := snapInfo.Name()
	affectedSnaps, err := m.repo.DisconnectSnap(snapName)
	if err != nil {
		return err
	}
	// TODO: remove all connections from the state
	if err := m.repo.RemoveSnap(snapName); err != nil {

		return err
	}
	if len(affectedSnaps) == 0 {
		affectedSnaps = append(affectedSnaps, snapInfo)
	}
	for _, snapInfo := range affectedSnaps {
		for _, backend := range securityBackendsForSnap(snapInfo) {
			if err := backend.Remove(snapInfo.Name()); err != nil {
				return state.Retry
			}
		}
	}
	return nil
}

func securityBackendsForSnap(snapInfo *snap.Info) []interfaces.SecurityBackend {
	aaBackend := &apparmor.Backend{}
	// TODO: Implement special provisions for apparmor and old-security when
	// old-security becomes a real interface. When that happens we nee to call
	// backend.UseLegacyTemplate() with the alternate template offered by the
	// old-security interface.
	return []interfaces.SecurityBackend{
		aaBackend, &seccomp.Backend{}, &dbus.Backend{}, &udev.Backend{}}
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
