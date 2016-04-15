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
	"strings"

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
// Extra interfaces can be provided for testing.
func Manager(s *state.State, extra []interfaces.Interface) (*InterfaceManager, error) {
	runner := state.NewTaskRunner(s)
	m := &InterfaceManager{
		state:  s,
		runner: runner,
		repo:   interfaces.NewRepository(),
	}
	if err := m.initialize(extra); err != nil {
		return nil, err
	}
	runner.AddHandler("connect", m.doConnect, nil)
	runner.AddHandler("disconnect", m.doDisconnect, nil)
	runner.AddHandler("setup-profiles", m.doSetupProfiles, m.doRemoveProfiles)
	runner.AddHandler("remove-profiles", m.doRemoveProfiles, m.doSetupProfiles)
	runner.AddHandler("discard-conns", m.doDiscardConns, m.undoDiscardConns)
	return m, nil
}

func (m *InterfaceManager) initialize(extra []interfaces.Interface) error {
	m.state.Lock()
	defer m.state.Unlock()

	if err := m.addInterfaces(extra); err != nil {
		return err
	}
	if err := m.addSnaps(); err != nil {
		return err
	}
	if err := m.reloadConnections(""); err != nil {
		return err
	}
	return nil
}

func (m *InterfaceManager) addInterfaces(extra []interfaces.Interface) error {
	for _, iface := range builtin.Interfaces() {
		if err := m.repo.AddInterface(iface); err != nil {
			return err
		}
	}
	for _, iface := range extra {
		if err := m.repo.AddInterface(iface); err != nil {
			return err
		}
	}
	return nil
}

func (m *InterfaceManager) addSnaps() error {
	snaps, err := snapstate.ActiveInfos(m.state)
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

// reloadConnections reloads connections stored in the state in the repository.
// Using non-empty snapName the operation can be scoped to connections
// affecting a given snap.
func (m *InterfaceManager) reloadConnections(snapName string) error {
	conns, err := getConns(m.state)
	if err != nil {
		return err
	}
	for id := range conns {
		plugRef, slotRef, err := parseConnID(id)
		if err != nil {
			return err
		}
		if snapName != "" && plugRef.Snap != snapName && slotRef.Snap != snapName {
			continue
		}
		err = m.repo.Connect(plugRef.Snap, plugRef.Name, slotRef.Snap, slotRef.Name)
		if err != nil {
			logger.Noticef("%s", err)
		}
	}
	return nil
}

func setupSnapSecurity(task *state.Task, snapInfo *snap.Info, repo *interfaces.Repository) error {
	var snapState snapstate.SnapState
	snapName := snapInfo.Name()
	if err := snapstate.Get(task.State(), snapName, &snapState); err != nil {
		task.Errorf("cannot get state of snap %q: %s", snapName, err)
		return err
	}
	for _, backend := range securityBackends {
		if err := backend.Setup(snapInfo, snapState.DevMode(), repo); err != nil {
			task.Errorf("cannot setup %s for snap %q: %s", backend.Name(), snapName, err)
			return err
		}
	}
	return nil
}

func removeSnapSecurity(task *state.Task, snapName string) error {
	for _, backend := range securityBackends {
		if err := backend.Remove(snapName); err != nil {
			task.Errorf("cannot setup %s for snap %q: %s", backend.Name(), snapName, err)
			return err
		}
	}
	return nil
}

func (m *InterfaceManager) doSetupProfiles(task *state.Task, _ *tomb.Tomb) error {
	task.State().Lock()
	defer task.State().Unlock()

	// Get snap.Info from bits handed by the snap manager.
	ss, err := snapstate.TaskSnapSetup(task)
	if err != nil {
		return err
	}
	snapInfo, err := snapstate.Info(task.State(), ss.Name, ss.Revision)
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
	blacklist := m.repo.AutoConnectBlacklist(snapName)
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
	if err := m.reloadConnections(snapName); err != nil {
		return err
	}
	if err := m.autoConnect(task, snapName, blacklist); err != nil {
		return err
	}
	if len(affectedSnaps) == 0 {
		affectedSnaps = append(affectedSnaps, snapInfo)
	}
	for _, snapInfo := range affectedSnaps {
		if err := setupSnapSecurity(task, snapInfo, m.repo); err != nil {
			return state.Retry
		}
	}
	return nil
}

type connState struct {
	Auto      bool   `json:"auto,omitempty"`
	Interface string `json:"interface,omitempty"`
}

func connID(plug *interfaces.PlugRef, slot *interfaces.SlotRef) string {
	return fmt.Sprintf("%s:%s %s:%s", plug.Snap, plug.Name, slot.Snap, slot.Name)
}

func parseConnID(conn string) (*interfaces.PlugRef, *interfaces.SlotRef, error) {
	parts := strings.SplitN(conn, " ", 2)
	if len(parts) != 2 {
		return nil, nil, fmt.Errorf("malformed connection identifier: %q", conn)
	}
	plugParts := strings.SplitN(parts[0], ":", 2)
	slotParts := strings.SplitN(parts[1], ":", 2)
	if len(plugParts) != 2 || len(slotParts) != 2 {
		return nil, nil, fmt.Errorf("malformed connection identifier: %q", conn)
	}
	plugRef := &interfaces.PlugRef{Snap: plugParts[0], Name: plugParts[1]}
	slotRef := &interfaces.SlotRef{Snap: slotParts[0], Name: slotParts[1]}
	return plugRef, slotRef, nil
}

func (m *InterfaceManager) autoConnect(task *state.Task, snapName string, blacklist map[string]bool) error {
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
		if blacklist[plug.Name] {
			continue
		}
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

func (m *InterfaceManager) doRemoveProfiles(task *state.Task, _ *tomb.Tomb) error {
	task.State().Lock()
	defer task.State().Unlock()

	snapSetup, err := snapstate.TaskSnapSetup(task)
	if err != nil {
		return err
	}
	snapName := snapSetup.Name

	affectedSnaps, err := m.repo.DisconnectSnap(snapName)
	if err != nil {
		return err
	}
	for _, snapInfo := range affectedSnaps {
		if snapInfo.Name() == snapName {
			continue
		}
		if err := setupSnapSecurity(task, snapInfo, m.repo); err != nil {
			return state.Retry
		}
	}

	if err := m.repo.RemoveSnap(snapName); err != nil {
		return err
	}
	if err := removeSnapSecurity(task, snapName); err != nil {
		return state.Retry
	}

	return nil
}

func (m *InterfaceManager) doDiscardConns(task *state.Task, _ *tomb.Tomb) error {
	st := task.State()
	st.Lock()
	defer st.Unlock()

	snapSetup, err := snapstate.TaskSnapSetup(task)
	if err != nil {
		return err
	}

	snapName := snapSetup.Name

	var snapState snapstate.SnapState
	err = snapstate.Get(st, snapName, &snapState)
	if err != nil && err != state.ErrNoState {
		return err
	}

	if err == nil && len(snapState.Sequence) != 0 {
		return fmt.Errorf("cannot discard connections for snap %q while it is present", snapName)
	}
	conns, err := getConns(st)
	if err != nil {
		return err
	}
	removed := make(map[string]connState)
	for id := range conns {
		plugRef, slotRef, err := parseConnID(id)
		if err != nil {
			return err
		}
		if plugRef.Snap == snapName || slotRef.Snap == snapName {
			removed[id] = conns[id]
			delete(conns, id)
		}
	}
	task.Set("removed", removed)
	setConns(st, conns)
	return nil
}

func (m *InterfaceManager) undoDiscardConns(task *state.Task, _ *tomb.Tomb) error {
	st := task.State()
	st.Lock()
	defer st.Unlock()

	var removed map[string]connState
	err := task.Get("removed", &removed)
	if err != nil && err != state.ErrNoState {
		return err
	}

	conns, err := getConns(st)
	if err != nil {
		return err
	}

	for id, connState := range removed {
		conns[id] = connState
	}
	setConns(st, conns)
	task.Set("removed", nil)
	return nil
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

func getConns(st *state.State) (map[string]connState, error) {
	// Get information about connections from the state
	var conns map[string]connState
	err := st.Get("conns", &conns)
	if err != nil && err != state.ErrNoState {
		return nil, fmt.Errorf("cannot obtain data about existing connections: %s", err)
	}
	if conns == nil {
		conns = make(map[string]connState)
	}
	return conns, nil
}

func setConns(st *state.State, conns map[string]connState) {
	st.Set("conns", conns)
}

func (m *InterfaceManager) doConnect(task *state.Task, _ *tomb.Tomb) error {
	st := task.State()
	st.Lock()
	defer st.Unlock()

	plugRef, slotRef, err := getPlugAndSlotRefs(task)
	if err != nil {
		return err
	}

	conns, err := getConns(st)
	if err != nil {
		return err
	}

	err = m.repo.Connect(plugRef.Snap, plugRef.Name, slotRef.Snap, slotRef.Name)
	if err != nil {
		return err
	}

	plug := m.repo.Plug(plugRef.Snap, plugRef.Name)
	slot := m.repo.Slot(slotRef.Snap, slotRef.Name)
	if err := setupSnapSecurity(task, plug.Snap, m.repo); err != nil {
		return state.Retry
	}
	if err := setupSnapSecurity(task, slot.Snap, m.repo); err != nil {
		return state.Retry
	}

	conns[connID(plugRef, slotRef)] = connState{Interface: plug.Interface}
	setConns(st, conns)

	return nil
}

func (m *InterfaceManager) doDisconnect(task *state.Task, _ *tomb.Tomb) error {
	st := task.State()
	st.Lock()
	defer st.Unlock()

	plugRef, slotRef, err := getPlugAndSlotRefs(task)
	if err != nil {
		return err
	}

	conns, err := getConns(st)
	if err != nil {
		return err
	}

	err = m.repo.Disconnect(plugRef.Snap, plugRef.Name, slotRef.Snap, slotRef.Name)
	if err != nil {
		return err
	}

	plug := m.repo.Plug(plugRef.Snap, plugRef.Name)
	slot := m.repo.Slot(slotRef.Snap, slotRef.Name)
	if err := setupSnapSecurity(task, plug.Snap, m.repo); err != nil {
		return state.Retry
	}
	if err := setupSnapSecurity(task, slot.Snap, m.repo); err != nil {
		return state.Retry
	}

	delete(conns, connID(plugRef, slotRef))
	setConns(st, conns)
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

// Repository returns the interface repository used internally by the manager.
//
// This method has two use-cases:
// - it is needed for setting up state in daemon tests
// - it is needed to return the set of known interfaces in the daemon api
//
// In the second case it is only informational and repository has internal
// locks to ensure consistency.
func (m *InterfaceManager) Repository() *interfaces.Repository {
	return m.repo
}

// MockSecurityBackends mocks the list of security backends that are used for setting up security.
//
// This function is public because it is referenced in the daemon
func MockSecurityBackends(backends []interfaces.SecurityBackend) func() {
	old := securityBackends
	securityBackends = backends
	return func() { securityBackends = old }
}

var securityBackends = []interfaces.SecurityBackend{
	&apparmor.Backend{}, &seccomp.Backend{}, &dbus.Backend{}, &udev.Backend{},
}
