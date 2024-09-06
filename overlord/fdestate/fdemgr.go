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

package fdestate

import (
	"fmt"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget/device"
	"github.com/snapcore/snapd/overlord/state"
)

// ServiceManager is responsible for starting and stopping snap services.
type FDEManager struct {
	state *state.State
}

type fdeManagerKey struct{}

func Manager(st *state.State, runner *state.TaskRunner) *FDEManager {
	m := &FDEManager{
		state: st,
	}

	boot.ProvideResealKeyToModeenv(func(rootdir string, modeenv *boot.Modeenv, expectReseal bool, unlocker boot.Unlocker) error {
		return resealLocked(m.state, modeenv, expectReseal)
	})

	st.Lock()
	st.Cache(fdeManagerKey{}, m)
	st.Unlock()

	return m
}

func (m *FDEManager) Ensure() error {
	return nil
}

func (m *FDEManager) Stop() {
	boot.ProvideResealKeyToModeenv(func(rootdir string, modeenv *boot.Modeenv, expectReseal bool, unlocker boot.Unlocker) error {
		return fmt.Errorf("fde manager is disabled")
	})
}

/*
func getManager(st *state.State) (*FDEManager, error) {
	c := st.Cached(fdeManagerKey{})
	if c == nil {
		return nil, fmt.Errorf("no FDE manager found")
	}
	manager := c.(*FDEManager)
	if manager == nil {
		return nil, fmt.Errorf("FDE manager found has wrong type")
	}

	return manager, nil
}
*/

func resealWithHookLocked(st *state.State, modeenv *boot.Modeenv, expectReseal bool) error {
	return boot.ResealKeyToModeenvUsingFDESetupHook(dirs.GlobalRootDir, modeenv, expectReseal)
}

func resealWithSecbootLocked(st *state.State, modeenv *boot.Modeenv, expectReseal bool) error {
	st.Unlock()
	defer st.Lock()
	return boot.ResealKeyToModeenvSecboot(dirs.GlobalRootDir, modeenv, expectReseal)
}

func resealNextGenLocked(st *state.State, modeenv *boot.Modeenv, expectReseal bool) error {
	st.Unlock()
	defer st.Lock()

	return boot.ResealKeyToModeenvNextGeneration(dirs.GlobalRootDir, modeenv, expectReseal)
}

func resealLocked(st *state.State, modeenv *boot.Modeenv, expectReseal bool) error {
	/*
		manager, err := getManager(st)
		if err != nil {
			return err
		}
	*/

	if !boot.IsModeeenvLocked() {
		return fmt.Errorf("modeenv is not locked")
	}

	method, err := device.SealedKeysMethod(dirs.GlobalRootDir)
	if err == device.ErrNoSealedKeys {
		return nil
	}
	if err != nil {
		return err
	}
	switch method {
	case device.SealingMethodFDESetupHook:
		return resealWithHookLocked(st, modeenv, expectReseal)
	case device.SealingMethodTPM, device.SealingMethodLegacyTPM:
		return resealWithSecbootLocked(st, modeenv, expectReseal)
	case device.SealingMethodNextGeneration:
		return resealNextGenLocked(st, modeenv, expectReseal)
	default:
		return fmt.Errorf("unknown key sealing method: %q", method)
	}
}

func Reseal(st *state.State, modeenv *boot.Modeenv, expectReseal bool) error {
	st.Lock()
	defer st.Unlock()

	return resealLocked(st, modeenv, expectReseal)
}

func init() {
	boot.ProvideResealKeyToModeenv(func(rootdir string, modeenv *boot.Modeenv, expectReseal bool, unlocker boot.Unlocker) error {
		return fmt.Errorf("fde manager is disabled")
	})
}
