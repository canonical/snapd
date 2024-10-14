// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

// Package fdestate implements the manager and state responsible for
// managing full disk encryption keys.
package fdestate

import (
	"time"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/gadget/device"
	"github.com/snapcore/snapd/overlord/fdestate/backend"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/secboot"
)

var (
	backendResealKeyForBootChains = backend.ResealKeyForBootChains
)

// FDEManager is responsible for managing full disk encryption keys.
type FDEManager struct {
	state *state.State
}

type fdeMgrKey struct{}

func isFDETaskKind(kind string) bool {
	switch kind {
	case "efi-secureboot-update-db":
	default:
		return false
	}
	return true
}

func Manager(st *state.State, runner *state.TaskRunner) *FDEManager {
	m := &FDEManager{
		state: st,
	}

	boot.ResealKeyForBootChains = m.resealKeyForBootChains

	st.Lock()
	defer st.Unlock()
	st.Cache(fdeMgrKey{}, m)

	runner.AddHandler("efi-secureboot-db-update", m.doEFISecurebootDBUpdate, nil)

	runner.AddBlocked(func(t *state.Task, running []*state.Task) bool {
		// TODO be more selective about other blocking FDE tasks
		if !isFDETaskKind(t.Kind()) {
			// non FDE tasks aren't blocked
			return false
		}

		for _, runningT := range running {
			if isFDETaskKind(runningT.Kind()) {
				// we have other FDE tasks running
				return true
			}
		}

		return false
	})

	return m
}

// Ensure implements StateManager.Ensure
func (m *FDEManager) Ensure() error {
	return nil
}

// StartUp implements StateStarterUp.Startup
func (m *FDEManager) StartUp() error {
	m.state.Lock()
	defer m.state.Unlock()
	return initializeState(m.state)
}

func (m *FDEManager) resealKeyForBootChains(unlocker boot.Unlocker, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, expectReseal bool) error {
	doUpdate := func(role string, containerRole string, bootModes []string, models []secboot.ModelForSealing, tpmPCRProfile []byte) error {
		if unlocker != nil {
			m.state.Lock()
			defer m.state.Unlock()
		}
		return updateParameters(m.state, role, containerRole, bootModes, models, tpmPCRProfile)
	}
	if unlocker != nil {
		locker := unlocker()
		defer locker()
	}
	return backendResealKeyForBootChains(doUpdate, method, rootdir, params, expectReseal)
}

func fdeMgr(st *state.State) *FDEManager {
	c := st.Cached(fdeMgrKey{})
	if c == nil {
		panic("internal error: FDE manager is not yet associated with state")
	}
	return c.(*FDEManager)
}

func (m *FDEManager) doEFISecurebootDBUpdate(task *state.Task, tomb *tomb.Tomb) error {
	// the handler does not do anything, it merely represents an action running externally

	// TODO should this keep returning state.Retry?
	return &state.Retry{After: time.Minute}
}
