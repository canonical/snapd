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
	"runtime"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/timings"
)

var (
	osutilSetTime = osutil.SetTime
	runtimeNumCPU = runtime.NumCPU
)

// initramfsMountsState helps tracking the state and progress
// of the mounting driving process.
type initramfsMountsState struct {
	mode           string
	recoverySystem string

	verifiedModel gadget.Model
	seeds         map[string]seed.Seed
}

var errRunModeNoImpliedRecoverySystem = errors.New("internal error: no implied recovery system in run mode")

// LoadSeed returns the seed for the recoverySystem.
// If recoverySystem is "" the implied one will be used (only for
// modes other than run).
func (mst *initramfsMountsState) LoadSeed(recoverySystem string) (seed.Seed, error) {
	if recoverySystem == "" {
		if mst.mode == "run" {
			return nil, errRunModeNoImpliedRecoverySystem
		}
		recoverySystem = mst.recoverySystem
	}

	if mst.seeds == nil {
		mst.seeds = make(map[string]seed.Seed)
	}
	foundSeed, hasSeed := mst.seeds[recoverySystem]
	if hasSeed {
		return foundSeed, nil
	}

	perf := timings.New(nil)

	// get the current time to pass to ReadSystemEssentialAndBetterEarliestTime
	// note that we trust the time we have from the system, because that time
	// comes from either:
	// * a RTC on the system that the kernel/systemd consulted and used to move
	//   time forward
	// * systemd using a built-in timestamp from the initrd which was stamped
	//   when the initrd was built, giving a lower bound on the current time if
	//   the RTC does not have a battery or is otherwise unreliable, etc.
	now := timeNow()

	jobs := 1
	if runtimeNumCPU() > 1 {
		jobs = 2
	}
	seed20, newTrustedEarliestTime := mylog.Check3(seed.ReadSeedAndBetterEarliestTime(boot.InitramfsUbuntuSeedDir, recoverySystem, now, jobs, perf))

	// set the time on the system to move forward if it is in the future - never
	// move the time backwards
	if newTrustedEarliestTime.After(now) {
		mylog.Check(osutilSetTime(newTrustedEarliestTime))
		// log the error but don't fail on it, we should be able to continue
		// even if the time can't be moved forward
	}

	mst.seeds[recoverySystem] = seed20

	return seed20, nil
}

// SetVerifiedBootModel sets the "verifiedModel" field. It should only
// be called after the model is verified. Either via a successful unlock
// of the encrypted data or after validating the seed in install/recover
// mode.
func (mst *initramfsMountsState) SetVerifiedBootModel(m gadget.Model) {
	mst.verifiedModel = m
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

	mf := mylog.Check2(os.Open(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model")))

	defer mf.Close()
	ma := mylog.Check2(asserts.NewDecoder(mf).Decode())

	if ma.Type() != asserts.ModelType {
		return nil, fmt.Errorf("unexpected assertion: %q", ma.Type().Name)
	}
	return ma.(*asserts.Model), nil
}

// EphemeralModeenvForModel generates a modeenv given the model and the snaps for the
// current mode and recovery system of the initramfsMountsState.
func (mst *initramfsMountsState) EphemeralModeenvForModel(model *asserts.Model, snaps map[snap.Type]*seed.Snap) (*boot.Modeenv, error) {
	if mst.mode == "run" {
		return nil, fmt.Errorf("internal error: initramfs should not write modeenv in run mode")
	}
	return &boot.Modeenv{
		Mode:           mst.mode,
		RecoverySystem: mst.recoverySystem,
		Base:           snaps[snap.TypeBase].PlaceInfo().Filename(),
		Gadget:         snaps[snap.TypeGadget].PlaceInfo().Filename(),
		Model:          model.Model(),
		BrandID:        model.BrandID(),
		Grade:          string(model.Grade()),
		// TODO:UC20: what about current kernel snaps, trusted boot assets and
		//            kernel command lines?
	}, nil
}
