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
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/timings"
)

var (
	osutilIsMounted = osutil.IsMounted
)

// initramfsMountsState helps tracking the state and progress
// of the mounting driving process.
type initramfsMountsState struct {
	mode           string
	recoverySystem string

	seed seed.EssentialMetaLoaderSeed
}

func newInitramfsMountsState(mode, recoverySystem string) *initramfsMountsState {
	return &initramfsMountsState{
		mode:           mode,
		recoverySystem: recoverySystem,
	}
}

var errRunModeNoImpliedRecoverySystem = errors.New("internal error: no implied recovery system in run mode")

// loadSeed opens the seed and reads its assertions; it does not
// re-open or re-read the seed when called multiple times.
// The opened seed is available is mst.seed
func (mst *initramfsMountsState) loadSeed(recoverySystem string) error {
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

// Model returns the verified model from the seed (only for
// modes other than run).
func (mst *initramfsMountsState) Model() (*asserts.Model, error) {
	if mst.mode == "run" {
		return nil, errRunModeNoImpliedRecoverySystem
	}
	if err := mst.loadSeed(""); err != nil {
		return nil, err
	}
	mod, _ := mst.seed.Model()
	return mod, nil
}

// RecoverySystemEssentialSnaps returns the verified essential
// snaps from the recoverySystem. If recoverySystem is "" the
// implied one will be used (only for modes other than run).
func (mst *initramfsMountsState) RecoverySystemEssentialSnaps(recoverySystem string, essentialTypes []snap.Type) ([]*seed.Snap, error) {
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

// UnverifiedBootModel returns the unverified model from the
// boot partition for run mode. The current and only use case
// is measuring the model for run mode. Otherwise no decisions
// should be based on an unverified model. Note that the model
// is verified at the time the key auth policy is computed.
func (mst *initramfsMountsState) UnverifiedBootModel() (*asserts.Model, error) {
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

func (mst *initramfsMountsState) IsMounted(dir string) (bool, error) {
	return osutilIsMounted(dir)
}
