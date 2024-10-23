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

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/gadget/device"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
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
	state   *state.State
	initErr error

	preseed bool
	mode    string
}

type fdeMgrKey struct{}

func initModeFromModeenv(m *FDEManager) error {
	mode, explicit, err := boot.SystemMode("")
	if err != nil {
		return err
	}

	if explicit {
		// FDE manager is only relevant on systems where mode set explicitly,
		// that is UC20
		m.mode = mode
	}
	return nil
}

func Manager(st *state.State, runner *state.TaskRunner) (*FDEManager, error) {
	m := &FDEManager{
		state:   st,
		preseed: snapdenv.Preseeding(),
	}

	boot.ResealKeyForBootChains = m.resealKeyForBootChains

	if !m.preseed {
		if err := initModeFromModeenv(m); err != nil {
			return nil, err
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

// StartUp implements StateStarterUp.Startup
func (m *FDEManager) StartUp() error {
	if m.preseed {
		// nothing to do in preseeding mode, but set the init error so that
		// attempts to use fdemgr will fail
		m.initErr = fmt.Errorf("internal error: FDE manager cannot be used in preseeding mode")
		return nil
	}

	m.state.Lock()
	defer m.state.Unlock()

	err := func() error {
		if m.mode == "run" {
			// TODO should we try to initialize the state in
			// install/recover/factory-reset modes?
			if err := initializeState(m.state); err != nil {
				return fmt.Errorf("cannot initialize FDE state: %v", err)
			}
		}
		return nil
	}()
	if err != nil {
		logger.Noticef("cannot complete FDE state manager startup: %v", err)
		// keep track of the error
		m.initErr = err
	}

	return nil
}

func (m *FDEManager) isFunctional() error {
	// TODO use more specific errors to capture different error states
	return m.initErr
}

// ReloadModeenv is a helper function for forcing a reload of modeenv. Only
// useful in integration testing.
func (m *FDEManager) ReloadModeenv() error {
	osutil.MustBeTestBinary("ReloadModeenv can only be called from tests")
	return initModeFromModeenv(m)
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
