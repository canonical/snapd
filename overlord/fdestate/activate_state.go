// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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
	"errors"
	"os"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/secboot"
)

var (
	bootLoadDiskUnlockState = boot.LoadDiskUnlockState
)

var errNoActivateState = errors.New("snap-bootstrap did not provide an activation state")

func loadActivateState() (*secboot.ActivateState, error) {
	unlockedState, err := bootLoadDiskUnlockState("unlocked.json")
	if err != nil {
		return nil, err
	}
	if unlockedState.State == nil {
		return nil, errNoActivateState
	}
	return unlockedState.State, nil
}

type cachedActivateStateKey struct{}

func getActivateState(st *state.State) (*secboot.ActivateState, error) {
	stateRaw := st.Cached(cachedActivateStateKey{})
	if stateRaw != nil {
		return stateRaw.(*secboot.ActivateState), nil
	}

	state, err := loadActivateState()
	if err != nil {
		return nil, err
	}
	st.Cache(cachedActivateStateKey{}, state)
	return state, nil
}

type FDEStatus string

const (
	// FDEStatusIndeterminate means we had an error while detecting
	// the current status. This will happen if the kernel snap is
	// too old, or we used a non-snap kernel (classic).
	FDEStatusIndeterminate FDEStatus = "indeterminate"
	// FDEStatusActive means disks have been activated and no
	// recovery key was used.
	FDEStatusActive FDEStatus = "active"
	// FDEStatusInactive means no encrypted disk has been
	// activated.
	FDEStatusInactive FDEStatus = "inactive"
	// FDEStatusRecovery means some disk have been activated with
	// a recovery key.
	FDEStatusRecovery FDEStatus = "recovery"
	// FDEStatusDegraded is reported when otherwise active, some
	// keyslots tried were invalid.
	FDEStatusDegraded FDEStatus = "degraded"
)

// FDESystemState is json serializable disk encryption state for the
// current boot.
type FDESystemState struct {
	// Status gives a summary on whether encrypted disks have been
	// activated and whether any recovery key was used.
	Status FDEStatus `json:"status"`
}

// SystemState returns a json serializable FDE state of the booted
// system.
func SystemState(st *state.State) (*FDESystemState, error) {
	ret := &FDESystemState{}

	s, err := getActivateState(st)
	if err == errNoActivateState {
		// We are probably in a case where snap-bootstrap is
		// new enough to have unlocked.json, but too old to provide
		// the activate state.
		ret.Status = FDEStatusIndeterminate
		// As we are in a hybrid/core case, using this new API
		// with old kernel, we can give a warning.
		logger.Noticef("WARNING: while reading activate state: %v", err)
		return ret, nil
	} else if os.IsNotExist(err) {
		// unlocked.json does not exist, we are in either case:
		//  * classic with kernel from deb.
		//  * hybrid/core where snap-bootstrap is too old.
		ret.Status = FDEStatusIndeterminate
		// New classic version will still not support this,
		// so we should be a bit more quiet.
		logger.Debugf("while reading activate state: %v", err)
		return ret, nil
	} else if err != nil {
		// Unexpected errors should fail explicitly.
		return nil, err
	}

	if s.TotalActivatedContainers() == 0 {
		ret.Status = FDEStatusInactive
		return ret, nil
	}

	if s.NumActivatedContainersWithRecoveryKey() != 0 {
		ret.Status = FDEStatusRecovery
	} else if secboot.ActivateStateHasDegradedErrors(s) {
		ret.Status = FDEStatusDegraded
	} else {
		ret.Status = FDEStatusActive
	}

	return ret, nil
}
