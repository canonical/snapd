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
func Manager(s *state.State, hookManager *hookstate.HookManager, extra []interfaces.Interface) (*InterfaceManager, error) {
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
	if err := m.initialize(extra); err != nil {
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

	return m, nil
}

func initialConnectAttributes(s *state.State, plugSnap string, plugName string, slotSnap string, slotName string) (map[string]interface{}, error) {
	// Combine attributes from plug and slot, store them in connect task.
	// They will serve as initial attributes for the prepare- hooks.
	var snapst snapstate.SnapState
	var err error
	attrs := make(map[string]interface{})

	if err = snapstate.Get(s, plugSnap, &snapst); err != nil {
		return nil, err
	}

	snapInfo, err := snapst.CurrentInfo()
	if err != nil {
		return nil, err
	}
	if plug, ok := snapInfo.Plugs[plugName]; ok {
		for k, v := range plug.Attrs {
			attrs[k] = v
		}
	} else {
		return nil, fmt.Errorf("Snap %q has no plug named %q", plugSnap, plugName)
	}

	if err = snapstate.Get(s, slotSnap, &snapst); err != nil {
		return nil, err
	}
	snapInfo, err = snapst.CurrentInfo()
	if err != nil {
		return nil, err
	}
	if slot, ok := snapInfo.Slots[slotName]; ok {
		for k, v := range slot.Attrs {
			attrs[k] = v
		}
	} else {
		return nil, fmt.Errorf("Snap %q has no slot named %q", slotSnap, slotName)
	}

	return attrs, err
}

// Connect returns a set of tasks for connecting an interface.
//
func Connect(s *state.State, plugSnap, plugName, slotSnap, slotName string) (*state.TaskSet, error) {
	// TODO: Store the intent-to-connect in the state so that we automatically
	// try to reconnect on reboot (reconnection can fail or can connect with
	// different parameters so we cannot store the actual connection details).

	// Create a series of tasks:
	//  - prepare-plug-<plug> hook
	//  - prepare-slot-<slot> hook
	//  - connect task
	//  - connect-slot-<slot> hook
	//  - connect-plug-<plug> hook
	// The tasks run in sequence (are serialized by WaitFor).
	// The prepare- hooks collect attributes set via snapctl iset. The attributes set by prepare-plug
	// hook can be read by both prepare-plug and prepare-slot hooks. All the attributes collected by
	// first two hooks are available to the connect task and both connect- hooks for reading.
	summary := fmt.Sprintf(i18n.G("Connect %s:%s to %s:%s"),
		plugSnap, plugName, slotSnap, slotName)
	connectInterface := s.NewTask("connect", summary)
	initialContext := map[string]interface{}{"connect-task": connectInterface.ID()}

	plugHookSetup := &hookstate.HookSetup{
		Snap:     plugSnap,
		Hook:     "prepare-plug-" + plugName,
		Optional: true,
	}
	summary = fmt.Sprintf(i18n.G("Run hook %s of snap %q"), plugHookSetup.Hook, plugHookSetup.Snap)
	preparePlugConnection := hookstate.HookTask(s, summary, plugHookSetup, initialContext)

	slotHookSetup := &hookstate.HookSetup{
		Snap:     slotSnap,
		Hook:     "prepare-slot-" + slotName,
		Optional: true,
	}
	summary = fmt.Sprintf(i18n.G("Run hook %s of snap %q"), slotHookSetup.Hook, slotHookSetup.Snap)

	prepareSlotConnection := hookstate.HookTask(s, summary, slotHookSetup, initialContext)
	prepareSlotConnection.WaitFor(preparePlugConnection)

	connectInterface.Set("slot", interfaces.SlotRef{Snap: slotSnap, Name: slotName})
	connectInterface.Set("plug", interfaces.PlugRef{Snap: plugSnap, Name: plugName})
	attrs, _ := initialConnectAttributes(s, plugSnap, plugName, slotSnap, slotName)
	if attrs != nil {
		connectInterface.Set("attributes", attrs)
	}
	connectInterface.WaitFor(prepareSlotConnection)

	connectSlotHookSetup := &hookstate.HookSetup{
		Snap:     slotSnap,
		Hook:     "connect-slot-" + slotName,
		Optional: true,
	}
	summary = fmt.Sprintf(i18n.G("Run hook %s of snap %q"), connectSlotHookSetup.Hook, connectSlotHookSetup.Snap)
	connectSlotConnection := hookstate.HookTask(s, summary, connectSlotHookSetup, initialContext)
	connectSlotConnection.WaitFor(connectInterface)

	connectPlugHookSetup := &hookstate.HookSetup{
		Snap:     plugSnap,
		Hook:     "connect-plug-" + plugName,
		Optional: true,
	}
	summary = fmt.Sprintf(i18n.G("Run hook %s of snap %q"), connectPlugHookSetup.Hook, connectPlugHookSetup.Snap)
	connectPlugConnection := hookstate.HookTask(s, summary, connectPlugHookSetup, initialContext)
	connectPlugConnection.WaitFor(connectSlotConnection)

	connectInterface.Set("connect-plug-task", connectPlugConnection.ID())
	connectInterface.Set("connect-slot-task", connectSlotConnection.ID())

	return state.NewTaskSet(preparePlugConnection, prepareSlotConnection, connectInterface, connectSlotConnection, connectPlugConnection), nil
}

// Disconnect returns a set of tasks for  disconnecting an interface.
func Disconnect(s *state.State, plugSnap, plugName, slotSnap, slotName string) (*state.TaskSet, error) {
	summary := fmt.Sprintf(i18n.G("Disconnect %s:%s from %s:%s"),
		plugSnap, plugName, slotSnap, slotName)
	task := s.NewTask("disconnect", summary)
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
