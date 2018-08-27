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
	"time"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/backends"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/ifacestate/ifacerepo"
	"github.com/snapcore/snapd/overlord/ifacestate/udevmonitor"
	"github.com/snapcore/snapd/overlord/state"
)

// InterfaceManager is responsible for the maintenance of interfaces in
// the system state.  It maintains interface connections, and also observes
// installed snaps to track the current set of available plugs and slots.
type InterfaceManager struct {
	state *state.State
	repo  *interfaces.Repository

	udevMon             udevmonitor.Interface
	udevInitTime        time.Time
	udevMonitorDisabled bool
}

// Manager returns a new InterfaceManager.
// Extra interfaces can be provided for testing.
func Manager(s *state.State, hookManager *hookstate.HookManager, runner *state.TaskRunner, extraInterfaces []interfaces.Interface, extraBackends []interfaces.SecurityBackend) (*InterfaceManager, error) {
	delayedCrossMgrInit()

	// NOTE: hookManager is nil only when testing.
	if hookManager != nil {
		setupHooks(hookManager)
	}

	// note: leave udevInitTime is at the default value, so that udev is initialized on first Ensure run.
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
	addHandler("disconnect", m.doDisconnect, m.undoDisconnect)
	addHandler("setup-profiles", m.doSetupProfiles, m.undoSetupProfiles)
	addHandler("remove-profiles", m.doRemoveProfiles, m.doSetupProfiles)
	addHandler("discard-conns", m.doDiscardConns, m.undoDiscardConns)
	addHandler("auto-connect", m.doAutoConnect, m.undoAutoConnect)
	addHandler("gadget-connect", m.doGadgetConnect, nil)
	addHandler("auto-disconnect", m.doAutoDisconnect, nil)

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

// Ensure implements StateManager.Ensure.
func (m *InterfaceManager) Ensure() error {
	if m.udevMon != nil || m.udevMonitorDisabled {
		return nil
	}

	// retry udev monitor initialization every 15 minutes
	now := time.Now()
	if now.After(m.udevInitTime) {
		err := m.initUdevMonitor()
		if err != nil {
			m.udevInitTime = now.Add(udevInitRetryTimeout)
		}
		return err
	}
	return nil
}

// Stop implements StateWaiterStopper.Stop. It stops
// the udev monitor, if running.
func (m *InterfaceManager) Stop() {
	if m.udevMon == nil {
		return
	}
	if err := m.udevMon.Stop(); err != nil {
		logger.Noticef("Failed to stop udev monitor: %s", err)
	}
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

// DisableUdevMonitor disables the instantiation of udev monitor, but has no effect
// if udev is already created; it should be called after creating InterfaceManager, before
// first Ensure.
// This method is meant for tests only.
func (m *InterfaceManager) DisableUdevMonitor() {
	if m.udevMon != nil {
		logger.Noticef("Udev Monitor already created, cannot be disabled")
		return
	}
	m.udevMonitorDisabled = true
}

var (
	udevInitRetryTimeout = time.Minute * 15
	createUdevMonitor    = udevmonitor.CreateUDevMonitor
)

func (m *InterfaceManager) initUdevMonitor() error {
	mon := createUdevMonitor(m.HotplugDeviceAdded, m.HotplugDeviceRemoved)
	if err := mon.Connect(); err != nil {
		return err
	}
	if err := mon.Run(); err != nil {
		mon.Disconnect()
		return err
	}
	m.udevMon = mon
	return nil
}

// MockSecurityBackends mocks the list of security backends that are used for setting up security.
//
// This function is public because it is referenced in the daemon
func MockSecurityBackends(be []interfaces.SecurityBackend) func() {
	old := backends.All
	backends.All = be
	return func() { backends.All = old }
}
