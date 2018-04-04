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
	"encoding/json"
	"fmt"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/backends"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/ifacestate/ifacerepo"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
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
	delayedCrossMgrInit()

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

	s.Lock()
	ifacerepo.Replace(s, m.repo)
	s.Unlock()

	// interface tasks might touch more than the immediate task target snap, serialize them
	runner.SetBlocked(func(t *state.Task, running []*state.Task) bool {
		if t.Kind() == "auto-connect" {
			return false
		}
		return len(running) != 0
	})

	runner.AddHandler("connect", m.doConnect, nil)
	runner.AddHandler("disconnect", m.doDisconnect, nil)
	runner.AddHandler("setup-profiles", m.doSetupProfiles, m.undoSetupProfiles)
	runner.AddHandler("remove-profiles", m.doRemoveProfiles, m.doSetupProfiles)
	runner.AddHandler("discard-conns", m.doDiscardConns, m.undoDiscardConns)
	runner.AddHandler("auto-connect", m.doAutoConnect, m.undoAutoConnect)

	// helper for ubuntu-core -> core
	runner.AddHandler("transition-ubuntu-core", m.doTransitionUbuntuCore, m.undoTransitionUbuntuCore)

	return m, nil
}

func (m *InterfaceManager) KnownTaskKinds() []string {
	return m.runner.KnownTaskKinds()
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

// SnapInterfaceState describes state of interfaces of a single snap.
type SnapInterfaceState struct {
	// Revision is the revision of the that snap was loaded into the
	// interface repository. When snapd restarts it may have added a snap to
	// the interface repository even when that snap was not yet active. To
	// ensure that we can reload the interface repository correctly, for
	// inactive snaps, this is the revision to load on startup.
	Revision snap.Revision `json:"revision"`
}

// ifaceRepoKey is the key used to store state of the interface repository in the snapd state.
const ifaceRepoKey = "repo"

// Get retrieves the SnapInterfaceState of the given snap or ErrNoState if missing.
func Get(st *state.State, snapName string, snapifst *SnapInterfaceState) error {
	var repoSt map[string]*json.RawMessage
	if err := st.Get(ifaceRepoKey, &repoSt); err != nil {
		return err
	}
	rawSnapifst, ok := repoSt[snapName]
	if !ok {
		return state.ErrNoState
	}
	if err := json.Unmarshal([]byte(*rawSnapifst), &snapifst); err != nil {
		return fmt.Errorf("cannot unmarshal snap interface state: %v", err)
	}
	return nil
}

// Set sets the SnapInterfaceState of the given snap, overwriting any earlier state.
func Set(st *state.State, snapName string, snapifst *SnapInterfaceState) {
	var repoSt map[string]*json.RawMessage
	if err := st.Get(ifaceRepoKey, &repoSt); err != nil && err != state.ErrNoState {
		panic("internal error: cannot unmarshal interface repo state: " + err.Error())
	}
	if repoSt == nil {
		repoSt = make(map[string]*json.RawMessage)
	}
	if snapifst == nil {
		delete(repoSt, snapName)
	} else {
		data, err := json.Marshal(snapifst)
		if err != nil {
			panic("internal error: cannot marshal snap interface state: " + err.Error())
		}
		raw := json.RawMessage(data)
		repoSt[snapName] = &raw
	}
	st.Set(ifaceRepoKey, repoSt)
}
