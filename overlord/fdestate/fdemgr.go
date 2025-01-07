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
	"github.com/snapcore/snapd/overlord/snapstate"
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

	if !secboot.WithSecbootSupport {
		m.initErr = fmt.Errorf("FDE manager is not operational in builds without secboot support")
	} else if m.preseed {
		// nothing to do in preseeding mode, but set the init error so that
		// attempts to use fdemgr will fail
		m.initErr = fmt.Errorf("internal error: FDE manager cannot be used in preseeding mode")
	} else {
		if err := initModeFromModeenv(m); err != nil {
			return nil, err
		}
	}

	st.Lock()
	defer st.Unlock()
	st.Cache(fdeMgrKey{}, m)

	snapstate.RegisterAffectedSnapsByKind("efi-secureboot-db-update", dbxUpdateAffectedSnaps)

	runner.AddHandler("efi-secureboot-db-update-prepare",
		m.doEFISecurebootDBUpdatePrepare, m.undoEFISecurebootDBUpdatePrepare)
	runner.AddCleanup("efi-secureboot-db-update-prepare", m.doEFISecurebootDBUpdatePrepareCleanup)
	runner.AddHandler("efi-secureboot-db-update", m.doEFISecurebootDBUpdate, nil)
	runner.AddBlocked(func(t *state.Task, running []*state.Task) bool {
		switch t.Kind() {
		case "efi-secureboot-db-update":
			return isEFISecurebootDBUpdateBlocked(t)
		}

		return false
	})

	return m, nil
}

// Ensure implements StateManager.Ensure
func (m *FDEManager) Ensure() error {
	return nil
}

// StartUp implements StateStarterUp.Startup
func (m *FDEManager) StartUp() error {
	if m.initErr != nil {
		// FDE manager was already disabled in constructor
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

type unlockedStateManager struct {
	*FDEManager
	unlocker boot.Unlocker
}

func (m *unlockedStateManager) Update(role string, containerRole string, parameters *backend.SealingParameters) error {
	return m.UpdateParameters(role, containerRole, parameters.BootModes, parameters.Models, parameters.TpmPCRProfile)
}

func (m *unlockedStateManager) Get(role string, containerRole string) (parameters *backend.SealingParameters, err error) {
	hasParamters, bootModes, models, tpmPCRProfile, err := m.GetParameters(role, containerRole)
	if err != nil || !hasParamters {
		return nil, err
	}

	return &backend.SealingParameters{
		BootModes:     bootModes,
		Models:        models,
		TpmPCRProfile: tpmPCRProfile,
	}, nil
}

func (m *unlockedStateManager) Unlock() (relock func()) {
	if m.unlocker != nil {
		return m.unlocker()
	}
	return func() {}
}

var _ backend.FDEStateManager = (*unlockedStateManager)(nil)

func (m *FDEManager) resealKeyForBootChains(unlocker boot.Unlocker, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, expectReseal bool) error {
	wrapped := &unlockedStateManager{
		FDEManager: m,
		unlocker:   unlocker,
	}
	return backendResealKeyForBootChains(wrapped, method, rootdir, params, expectReseal)
}

func fdeMgr(st *state.State) *FDEManager {
	c := st.Cached(fdeMgrKey{})
	if c == nil {
		panic("internal error: FDE manager is not yet associated with state")
	}
	return c.(*FDEManager)
}

func (m *FDEManager) UpdateParameters(role string, containerRole string, bootModes []string, models []secboot.ModelForSealing, tpmPCRProfile []byte) error {
	return updateParameters(m.state, role, containerRole, bootModes, models, tpmPCRProfile)
}

func (m *FDEManager) GetParameters(role string, containerRole string) (hasParameters bool, bootModes []string, models []secboot.ModelForSealing, tpmPCRProfile []byte, err error) {
	var s FdeState
	err = m.state.Get(fdeStateKey, &s)
	if err != nil {
		return false, nil, nil, nil, err
	}

	return s.getParameters(role, containerRole)
}
