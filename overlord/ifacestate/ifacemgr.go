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

	"github.com/snapcore/snapd/i18n/dumb"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/backends"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/snapstate"

	"github.com/snapcore/snapd/overlord/state"
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
func Manager(s *state.State, hookManager *hookstate.HookManager, extraInterfaces []interfaces.Interface, extraBackends []interfaces.SecurityBackend) (*InterfaceManager, error) {
	// NOTE: hookManager is nil only when testing.
	if hookManager != nil {
		setupHooks(hookManager)
	}

	runner := state.NewTaskRunner(s)
	m := &InterfaceManager{
		state:  s,
		runner: runner,
		repo:   interfaces.NewRepository(),
	}
	if err := m.initialize(extraInterfaces, extraBackends); err != nil {
		return nil, err
	}

	// interface tasks might touch more than the immediate task target snap, serialize them
	runner.SetBlocked(func(_ *state.Task, running []*state.Task) bool {
		return len(running) != 0
	})

	runner.AddHandler("connect", m.doConnect, nil)
	runner.AddHandler("disconnect", m.doDisconnect, nil)
	runner.AddHandler("setup-profiles", m.doSetupProfiles, m.undoSetupProfiles)
	runner.AddHandler("remove-profiles", m.doRemoveProfiles, m.doSetupProfiles)
	runner.AddHandler("discard-conns", m.doDiscardConns, m.undoDiscardConns)

	// helper for ubuntu-core -> core
	runner.AddHandler("transition-ubuntu-core", m.doTransitionUbuntuCore, m.undoTransitionUbuntuCore)

	return m, nil
}

// Connect returns a set of tasks for connecting an interface.
//
func Connect(st *state.State, plugSnap, plugName, slotSnap, slotName string) (*state.TaskSet, error) {
	if err := snapstate.CheckChangeConflict(st, plugSnap, nil); err != nil {
		return nil, err
	}
	if err := snapstate.CheckChangeConflict(st, slotSnap, nil); err != nil {
		return nil, err
	}

	// TODO: Store the intent-to-connect in the state so that we automatically
	// try to reconnect on reboot (reconnection can fail or can connect with
	// different parameters so we cannot store the actual connection details).
	plugHookSetup := &hookstate.HookSetup{
		Snap:     plugSnap,
		Hook:     "prepare-plug-" + plugName,
		Optional: true,
	}
	summary := fmt.Sprintf(i18n.G("Prepare connection of plug %s:%s"), plugSnap, plugName)
	preparePlugConnection := hookstate.HookTask(st, summary, plugHookSetup, nil)

	slotHookSetup := &hookstate.HookSetup{
		Snap:     slotSnap,
		Hook:     "prepare-slot-" + slotName,
		Optional: true,
	}
	summary = fmt.Sprintf(i18n.G("Prepare connection of slot %s:%s"), slotSnap, slotName)
	prepareSlotConnection := hookstate.HookTask(st, summary, slotHookSetup, nil)
	prepareSlotConnection.WaitFor(preparePlugConnection)

	summary = fmt.Sprintf(i18n.G("Connect %s:%s to %s:%s"),
		plugSnap, plugName, slotSnap, slotName)
	connectInterface := st.NewTask("connect", summary)
	connectInterface.Set("slot", interfaces.SlotRef{Snap: slotSnap, Name: slotName})
	connectInterface.Set("plug", interfaces.PlugRef{Snap: plugSnap, Name: plugName})
	connectInterface.WaitFor(prepareSlotConnection)

	confirmSlotHookSetup := &hookstate.HookSetup{
		Snap:     slotSnap,
		Hook:     "connect-slot-" + slotName,
		Optional: true,
	}
	summary = fmt.Sprintf(i18n.G("Confirm connection of slot %s:%s"), slotSnap, slotName)
	confirmSlotConnection := hookstate.HookTask(st, summary, confirmSlotHookSetup, nil)
	confirmSlotConnection.WaitFor(connectInterface)

	confirmPlugHookSetup := &hookstate.HookSetup{
		Snap:     plugSnap,
		Hook:     "connect-plug-" + plugName,
		Optional: true,
	}
	summary = fmt.Sprintf(i18n.G("Confirm connection of plug %s:%s"), plugSnap, plugName)
	confirmPlugConnection := hookstate.HookTask(st, summary, confirmPlugHookSetup, nil)
	confirmPlugConnection.WaitFor(confirmSlotConnection)

	return state.NewTaskSet(preparePlugConnection, prepareSlotConnection, connectInterface, confirmSlotConnection, confirmPlugConnection), nil
}

// Disconnect returns a set of tasks for  disconnecting an interface.
func Disconnect(st *state.State, plugSnap, plugName, slotSnap, slotName string) (*state.TaskSet, error) {
	if err := snapstate.CheckChangeConflict(st, plugSnap, nil); err != nil {
		return nil, err
	}
	if err := snapstate.CheckChangeConflict(st, slotSnap, nil); err != nil {
		return nil, err
	}

	summary := fmt.Sprintf(i18n.G("Disconnect %s:%s from %s:%s"),
		plugSnap, plugName, slotSnap, slotName)
	task := st.NewTask("disconnect", summary)
	task.Set("slot", interfaces.SlotRef{Snap: slotSnap, Name: slotName})
	task.Set("plug", interfaces.PlugRef{Snap: plugSnap, Name: plugName})
	return state.NewTaskSet(task), nil
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
func MockSecurityBackends(be []interfaces.SecurityBackend) func() {
	old := backends.All
	backends.All = be
	return func() { backends.All = old }
}
