// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/timings"
)

// initramfsMountsState helps tracking the state and progress
// of the mounting driving process.
type initramfsMountsState struct {
	mode           string
	recoverySystem string
}

var errRunModeNoImpliedRecoverySystem = errors.New("internal error: no implied recovery system in run mode")

// ReadEssential returns the model and verified essential
// snaps from the recoverySystem. If recoverySystem is "" the
// implied one will be used (only for modes other than run).
func (mst *initramfsMountsState) ReadEssential(recoverySystem string, essentialTypes []snap.Type) (*asserts.Model, []*seed.Snap, error) {
	if recoverySystem == "" {
		if mst.mode == "run" {
			return nil, nil, errRunModeNoImpliedRecoverySystem
		}
		recoverySystem = mst.recoverySystem
	}

	perf := timings.New(nil)
	return seed.ReadSystemEssential(boot.InitramfsUbuntuSeedDir, recoverySystem, essentialTypes, perf)
}

// UnverifiedBootModel returns the unverified model from the
// boot partition for run mode. The current and only use case
// is measuring the model for run mode. Otherwise no decisions
// should be based on an unverified model. Note that the model
// is verified at the time the key auth policy is computed.
func (mst *initramfsMountsState) UnverifiedBootModel() (*asserts.Model, error) {
	if mst.mode != "run" {
		return nil, fmt.Errorf("internal error: unverified boot model access is for limited run mode use")
	}

	mf, err := os.Open(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"))
	if err != nil {
		return nil, fmt.Errorf("cannot read model assertion: %v", err)
	}
	defer mf.Close()
	ma, err := asserts.NewDecoder(mf).Decode()
	if err != nil {
		return nil, fmt.Errorf("cannot decode assertion: %v", err)
	}
	if ma.Type() != asserts.ModelType {
		return nil, fmt.Errorf("unexpected assertion: %q", ma.Type().Name)
	}
	return ma.(*asserts.Model), nil
}
