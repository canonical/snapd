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
type initramfsMountsState interface {
	// Model returns the verified model from the seed (only for
	// modes other than run).
	Model() (*asserts.Model, error)

	// RecoverySystemEssentialSnaps returns the verified essential
	// snaps from the recoverySystem, if that is "" the implied one
	// will be used (only for modes other than run).
	RecoverySystemEssentialSnaps(recoverySystem string, essentialTypes []snap.Type) ([]*seed.Snap, error)

	// UnverifiedBootModel returns the unverified model from the boot
	// partition for run mode.
	UnverifiedBootModel() (*asserts.Model, error)
}

var newInitramfsMountsState = func(mode, recoverySystem string) initramfsMountsState {
	return &initramfsMountsStateImpl{
		mode:           mode,
		recoverySystem: recoverySystem,
	}
}

type initramfsMountsStateImpl struct {
	mode           string
	recoverySystem string

	seed seed.EssentialMetaLoaderSeed
}

var errRunModeNoImpliedRecoverySystem = errors.New("internal error: no implied recovery system in run mode")

// loadSeed open the seed and reads assertions once, setting mst.seed
func (mst *initramfsMountsStateImpl) loadSeed(recoverySystem string) error {
	if mst.seed != nil {
		return nil
	}

	if recoverySystem == "" {
		if mst.mode == "run" {
			return errRunModeNoImpliedRecoverySystem
		}
		recoverySystem = mst.recoverySystem
	}

	systemSeed, err := seed.Open(boot.InitramfsUbuntuSeedDir, recoverySystem)
	if err != nil {
		return err
	}

	seed20, ok := systemSeed.(seed.EssentialMetaLoaderSeed)
	if !ok {
		return fmt.Errorf("internal error: UC20 seed must implement EssentialMetaLoaderSeed")
	}

	// load assertions into a temporary database
	if err := seed20.LoadAssertions(nil, nil); err != nil {
		return err
	}

	mst.seed = seed20
	return nil

}

func (mst *initramfsMountsStateImpl) Model() (*asserts.Model, error) {
	if mst.mode == "run" {
		return nil, errRunModeNoImpliedRecoverySystem
	}
	if err := mst.loadSeed(""); err != nil {
		return nil, err
	}
	mod, _ := mst.seed.Model()
	return mod, nil
}

func (mst *initramfsMountsStateImpl) RecoverySystemEssentialSnaps(recoverySystem string, essentialTypes []snap.Type) ([]*seed.Snap, error) {
	if err := mst.loadSeed(recoverySystem); err != nil {
		return nil, err
	}

	// load and verify metadata only for the relevant essential snaps
	perf := timings.New(nil)
	if err := mst.seed.LoadEssentialMeta(essentialTypes, perf); err != nil {
		return nil, err
	}

	return mst.seed.EssentialSnaps(), nil
}

func (mst *initramfsMountsStateImpl) UnverifiedBootModel() (*asserts.Model, error) {
	if mst.mode != "run" {
		return nil, fmt.Errorf("internal error: unverified boot model access is for limited run mode use")
	}

	mf, err := os.Open(filepath.Join(boot.InitramfsUbuntuBootDir, "model"))
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
