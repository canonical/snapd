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
	"fmt"
	"os"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/gadget/device"
	"github.com/snapcore/snapd/overlord/fdestate/backend"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/snapdenv"
)

var (
	backendResealKeyForBootChains = backend.ResealKeyForBootChains
)

// FDEManager is responsible for managing full disk encryption keys.
type FDEManager struct {
	state *state.State

	preseed bool
	mode    string
}

type fdeMgrKey struct{}

func Manager(st *state.State, runner *state.TaskRunner) (*FDEManager, error) {
	m := &FDEManager{
		state:   st,
		preseed: snapdenv.Preseeding(),
	}

	boot.ResealKeyForBootChains = m.resealKeyForBootChains

	if !m.preseed {
		modeenv, err := maybeReadModeenv()
		if err != nil {
			return nil, err
		}

		if modeenv != nil {
			m.mode = modeenv.Mode
		}
	}

	st.Lock()
	defer st.Unlock()
	st.Cache(fdeMgrKey{}, m)

	return m, nil
}

// Ensure implements StateManager.Ensure
func (m *FDEManager) Ensure() error {
	return nil
}

func maybeReadModeenv() (*boot.Modeenv, error) {
	modeenv, err := boot.ReadModeenv("")
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("cannot read modeenv: %v", err)
	}
	return modeenv, nil
}

// StartUp implements StateStarterUp.Startup
func (m *FDEManager) StartUp() error {
	if m.preseed {
		// nothing to do in preseeding mode
		return nil
	}

	m.state.Lock()
	defer m.state.Unlock()

	if m.mode == "run" {
		// TODO should we try to initialize the state in
		// install/recover/factory-reset modes?
		if err := initializeState(m.state); err != nil {
			return fmt.Errorf("cannot initialize FDE state: %v", err)
		}
	}
	return nil
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
