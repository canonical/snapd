// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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

package ifacestate

import (
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/backends"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/ifacestate/ifacerepo"
	"github.com/snapcore/snapd/overlord/state"
)

// InterfaceManager is responsible for the maintenance of interfaces in
// the system state.  It maintains interface connections, and also observes
// installed snaps to track the current set of available plugs and slots.
type InterfaceManager struct {
	state *state.State
	repo  *interfaces.Repository
}

// Manager returns a new InterfaceManager.
// Extra interfaces can be provided for testing.
func Manager(s *state.State, hookManager *hookstate.HookManager, runner *state.TaskRunner, extraInterfaces []interfaces.Interface, extraBackends []interfaces.SecurityBackend) (*InterfaceManager, error) {
	delayedCrossMgrInit()

	// NOTE: hookManager is nil only when testing.
	if hookManager != nil {
		setupHooks(hookManager)
	}

	m := &InterfaceManager{
		state: s,
		repo:  interfaces.NewRepository(),
	}

	if err := m.initialize(extraInterfaces, extraBackends); err != nil {
		return nil, err
	}

	s.Lock()
	ifacerepo.Replace(s, m.repo)
	s.Unlock()

	taskKinds := map[string]bool{}
	addHandler := func(kind string, do, undo state.HandlerFunc) {
		taskKinds[kind] = true
		runner.AddHandler(kind, do, undo)
	}

	addHandler("connect", m.doConnect, m.undoConnect)
	addHandler("disconnect", m.doDisconnect, nil)
	addHandler("setup-profiles", m.doSetupProfiles, m.undoSetupProfiles)
	addHandler("remove-profiles", m.doRemoveProfiles, m.doSetupProfiles)
	addHandler("discard-conns", m.doDiscardConns, m.undoDiscardConns)
	addHandler("auto-connect", m.doAutoConnect, m.undoAutoConnect)
	addHandler("gadget-connect", m.doGadgetConnect, nil)

	// helper for ubuntu-core -> core
	addHandler("transition-ubuntu-core", m.doTransitionUbuntuCore, m.undoTransitionUbuntuCore)

	// interface tasks might touch more than the immediate task target snap, serialize them
	runner.AddBlocked(func(t *state.Task, running []*state.Task) bool {
		if !taskKinds[t.Kind()] {
			return false
		}

		for _, t := range running {
			if taskKinds[t.Kind()] {
				return true
			}
		}

		return false
	})

	return m, nil
}

func (m *InterfaceManager) KnownTaskKinds() []string {
	return nil
}

// Ensure implements StateManager.Ensure.
func (m *InterfaceManager) Ensure() error {
	return nil
}

// Wait implements StateManager.Wait.
func (m *InterfaceManager) Wait() {
}

// Stop implements StateManager.Stop.
func (m *InterfaceManager) Stop() {
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
