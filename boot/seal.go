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

package boot

import (
	"fmt"
	"path/filepath"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/timings"
)

var (
	secbootSealKey = secboot.SealKey

	seedReadSystemEssential = seed.ReadSystemEssential
)

var (
	roleToBlName = map[bootloader.Role]string{}
)

// sealKeyToModeenv seals the supplied key to the parameters specified
// in modeenv.
func sealKeyToModeenv(key secboot.EncryptionKey, model *asserts.Model, modeenv *Modeenv) error {
	// build the recovery mode boot chain
	rbl, err := bootloader.Find(InitramfsUbuntuSeedDir, &bootloader.Options{
		Role: bootloader.RoleRecovery,
	})
	if err != nil {
		return fmt.Errorf("cannot find the recovery bootloader: %v", err)
	}
	roleToBlName[bootloader.RoleRecovery] = rbl.Name()

	recoveryBootChain, err := buildRecoveryBootChain(rbl, model, modeenv)
	if err != nil {
		return fmt.Errorf("cannot build recovery boot chain: %v", err)
	}

	// build the run mode boot chains
	bl, err := bootloader.Find(InitramfsUbuntuBootDir, &bootloader.Options{
		Role:        bootloader.RoleRunMode,
		NoSlashBoot: true,
	})
	if err != nil {
		return fmt.Errorf("cannot find the bootloader: %v", err)
	}
	roleToBlName[bootloader.RoleRunMode] = bl.Name()

	runModeBootChains, err := buildRunModeBootChains(rbl, bl, model, modeenv)
	if err != nil {
		return fmt.Errorf("cannot build run mode boot chain: %v", err)
	}

	pbc := toPredictableBootChains(append(runModeBootChains, recoveryBootChain))

	// XXX: store the predictable bootchains

	// get parameters from bootchains and seal the key
	params, err := sealKeyParams(pbc)
	if err != nil {
		return fmt.Errorf("cannot build key sealing parameters: %v", err)
	}

	if err := secbootSealKey(key, params); err != nil {
		return fmt.Errorf("cannot seal the encryption key: %v", err)
	}

	return nil
}

func buildRecoveryBootChain(rbl bootloader.Bootloader, model *asserts.Model, modeenv *Modeenv) (bc bootChain, err error) {
	// get the command line
	cmdline, err := ComposeRecoveryCommandLine(model, modeenv.RecoverySystem)
	if err != nil {
		return bc, fmt.Errorf("cannot obtain recovery kernel command line: %v", err)
	}

	// get kernel information from seed
	perf := timings.New(nil)
	_, snaps, err := seedReadSystemEssential(dirs.SnapSeedDir, modeenv.RecoverySystem, []snap.Type{snap.TypeKernel}, perf)
	if err != nil {
		return bc, err
	}
	if len(snaps) != 1 {
		return bc, fmt.Errorf("cannot obtain recovery kernel snap")
	}
	seedKernel := snaps[0]

	var kernelRev string
	if seedKernel.SideInfo.Revision.Store() {
		kernelRev = seedKernel.SideInfo.Revision.String()
	}

	// get asset chains
	assetChain, kbf, err := buildRecoveryAssetChain(rbl, modeenv)
	if err != nil {
		return bc, err
	}

	return bootChain{
		BrandID:        model.BrandID(),
		Model:          model.Model(),
		Grade:          string(model.Grade()),
		ModelSignKeyID: model.SignKeyID(),
		AssetChain:     assetChain,
		Kernel:         seedKernel.Path,
		KernelRevision: kernelRev,
		KernelCmdlines: []string{cmdline},
		model:          model,
		kernelBootFile: bootloader.NewBootFile(seedKernel.Path, kbf.Path, kbf.Role),
	}, nil
}

func buildRunModeBootChains(rbl, bl bootloader.Bootloader, model *asserts.Model, modeenv *Modeenv) ([]bootChain, error) {
	// get the command line
	cmdline, err := ComposeCandidateCommandLine(model)
	if err != nil {
		return nil, fmt.Errorf("cannot obtain kernel command line: %v", err)
	}

	// get asset chains
	assetChain, kbf, err := buildRunModeAssetChain(rbl, bl, modeenv)
	if err != nil {
		return nil, err
	}

	// get run mode kernels
	runModeKernels, err := runModeKernelsFromModeenv(modeenv)
	if err != nil {
		return nil, err
	}

	chains := make([]bootChain, 0, 2)
	for _, k := range runModeKernels {
		chains = append(chains, bootChain{
			BrandID:        model.BrandID(),
			Model:          model.Model(),
			Grade:          string(model.Grade()),
			ModelSignKeyID: model.SignKeyID(),
			AssetChain:     assetChain,
			Kernel:         k,
			// XXX: obtain revision
			KernelRevision: "",
			KernelCmdlines: []string{cmdline},
			model:          model,
			kernelBootFile: bootloader.NewBootFile(k, kbf.Path, kbf.Role),
		})
	}

	return chains, nil
}

func buildRecoveryAssetChain(rbl bootloader.Bootloader, modeenv *Modeenv) (assets []bootAsset, kernel bootloader.BootFile, err error) {
	tbl, ok := rbl.(bootloader.TrustedAssetsBootloader)
	if !ok {
		return nil, kernel, fmt.Errorf("bootloader doesn't support trusted assets")
	}

	recoveryBootChain, err := tbl.RecoveryBootChain("")
	if err != nil {
		return nil, kernel, err
	}

	// the last entry is the kernel
	numAssets := len(recoveryBootChain) - 1
	assets = make([]bootAsset, numAssets)

	for i := 0; i < numAssets; i++ {
		name := filepath.Base(recoveryBootChain[i].Path)
		hashes, ok := modeenv.CurrentTrustedRecoveryBootAssets[name]
		if !ok {
			return nil, kernel, fmt.Errorf("cannot find asset %s in modeenv", name)
		}
		assets[i] = bootAsset{
			Role:   recoveryBootChain[i].Role,
			Name:   name,
			Hashes: hashes,
		}
	}

	return assets, recoveryBootChain[numAssets], nil
}

func buildRunModeAssetChain(rbl, bl bootloader.Bootloader, modeenv *Modeenv) (assets []bootAsset, kernel bootloader.BootFile, err error) {
	tbl, ok := rbl.(bootloader.TrustedAssetsBootloader)
	if !ok {
		return nil, kernel, fmt.Errorf("recovery bootloader doesn't support trusted assets")
	}

	recoveryBootChain, err := tbl.RecoveryBootChain("")
	if err != nil {
		return nil, kernel, err
	}
	// the last entry is the kernel
	numRecoveryAssets := len(recoveryBootChain) - 1

	runModeBootChain, err := tbl.BootChain(bl, "")
	if err != nil {
		return nil, kernel, err
	}
	// the last entry is the kernel
	numRunModeAssets := len(runModeBootChain) - 1

	assets = make([]bootAsset, numRunModeAssets)

	for i := 0; i < numRunModeAssets; i++ {
		name := filepath.Base(runModeBootChain[i].Path)
		var hashes []string
		var ok bool
		if i < numRecoveryAssets {
			hashes, ok = modeenv.CurrentTrustedRecoveryBootAssets[name]
		} else {
			hashes, ok = modeenv.CurrentTrustedBootAssets[name]
		}
		if !ok {
			return nil, kernel, fmt.Errorf("cannot find asset %s in modeenv", name)
		}
		assets[i] = bootAsset{
			Role:   runModeBootChain[i].Role,
			Name:   name,
			Hashes: hashes,
		}
	}

	return assets, runModeBootChain[numRunModeAssets], nil
}

// runModeKernelsFromModeenv obtains the current and next kernels
// listed in modeenv.
func runModeKernelsFromModeenv(modeenv *Modeenv) ([]string, error) {
	switch len(modeenv.CurrentKernels) {
	case 1:
		current := filepath.Join(dirs.SnapBlobDir, modeenv.CurrentKernels[0])
		return []string{current}, nil
	case 2:
		current := filepath.Join(dirs.SnapBlobDir, modeenv.CurrentKernels[0])
		next := filepath.Join(dirs.SnapBlobDir, modeenv.CurrentKernels[1])
		return []string{current, next}, nil
	}
	return nil, fmt.Errorf("invalid number of kernels in modeenv")
}

func sealKeyParams(pbc predictableBootChains) (*secboot.SealKeyParams, error) {
	modelParams := make([]*secboot.SealKeyModelParams, 0, len(pbc))
	for _, bc := range pbc {
		loadChains, err := bootAssetsToLoadChains(bc.AssetChain, bc.kernelBootFile, roleToBlName)
		if err != nil {
			return nil, fmt.Errorf("error building load chains: %s", err)
		}

		modelParams = append(modelParams, &secboot.SealKeyModelParams{
			Model:          bc.model,
			KernelCmdlines: bc.KernelCmdlines,
			EFILoadChains:  loadChains,
		})
	}

	sealKeyParams := &secboot.SealKeyParams{
		ModelParams:             modelParams,
		KeyFile:                 filepath.Join(InitramfsEncryptionKeyDir, "ubuntu-data.sealed-key"),
		TPMPolicyUpdateDataFile: filepath.Join(InstallHostFDEDataDir, "policy-update-data"),
		TPMLockoutAuthFile:      filepath.Join(InstallHostFDEDataDir, "tpm-lockout-auth"),
	}

	return sealKeyParams, nil
}
