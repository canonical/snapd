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

	recoveryBootChains, err := buildRecoveryBootChainsForSystems([]string{modeenv.RecoverySystem}, rbl, model, modeenv)
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
	cmdline, err := ComposeCandidateCommandLine(model)
	if err != nil {
		return fmt.Errorf("cannot compose the candidate command line: %v", err)
	}

	runModeBootChains, err := buildRunModeBootChains(rbl, bl, model, modeenv, cmdline)
	if err != nil {
		return fmt.Errorf("cannot build run mode boot chain: %v", err)
	}

	pbc := toPredictableBootChains(append(runModeBootChains, recoveryBootChains...))

	roleToBlName := map[bootloader.Role]string{
		bootloader.RoleRecovery: rbl.Name(),
		bootloader.RoleRunMode:  bl.Name(),
	}

	// get model parameters from bootchains
	modelParams, err := sealKeyModelParams(pbc, roleToBlName)
	if err != nil {
		return fmt.Errorf("cannot build key sealing parameters: %v", err)
	}
	sealKeyParams := &secboot.SealKeyParams{
		ModelParams:             modelParams,
		KeyFile:                 filepath.Join(InitramfsEncryptionKeyDir, "ubuntu-data.sealed-key"),
		TPMPolicyUpdateDataFile: filepath.Join(InstallHostFDEDataDir, "policy-update-data"),
		TPMLockoutAuthFile:      filepath.Join(InstallHostFDEDataDir, "tpm-lockout-auth"),
	}
	// finally, seal the key
	if err := secbootSealKey(key, sealKeyParams); err != nil {
		return fmt.Errorf("cannot seal the encryption key: %v", err)
	}

	// TODO:UC20: store the predictable bootchains

	return nil
}

func buildRecoveryBootChainsForSystems(systems []string, rbl bootloader.Bootloader, model *asserts.Model, modeenv *Modeenv) (chains []bootChain, err error) {
	tbl, ok := rbl.(bootloader.TrustedAssetsBootloader)
	if !ok {
		return nil, fmt.Errorf("bootloader doesn't support trusted assets")
	}

	for _, system := range systems {
		// get the command line
		cmdline, err := ComposeRecoveryCommandLine(model, system)
		if err != nil {
			return nil, fmt.Errorf("cannot obtain recovery kernel command line: %v", err)
		}

		// get kernel information from seed
		perf := timings.New(nil)
		_, snaps, err := seedReadSystemEssential(dirs.SnapSeedDir, system, []snap.Type{snap.TypeKernel}, perf)
		if err != nil {
			return nil, err
		}
		if len(snaps) != 1 {
			return nil, fmt.Errorf("cannot obtain recovery kernel snap")
		}
		seedKernel := snaps[0]

		var kernelRev string
		if seedKernel.SideInfo.Revision.Store() {
			kernelRev = seedKernel.SideInfo.Revision.String()
		}

		recoveryBootChain, err := tbl.RecoveryBootChain(seedKernel.Path)
		if err != nil {
			return nil, err
		}

		// get asset chains
		assetChain, kbf, err := buildBootAssets(recoveryBootChain, modeenv)
		if err != nil {
			return nil, err
		}

		chains = append(chains, bootChain{
			BrandID:        model.BrandID(),
			Model:          model.Model(),
			Grade:          model.Grade(),
			ModelSignKeyID: model.SignKeyID(),
			AssetChain:     assetChain,
			Kernel:         seedKernel.SnapName(),
			KernelRevision: kernelRev,
			KernelCmdlines: []string{cmdline},
			model:          model,
			kernelBootFile: kbf,
		})
	}
	return chains, nil
}

func buildRunModeBootChains(rbl, bl bootloader.Bootloader, model *asserts.Model, modeenv *Modeenv, cmdline string) ([]bootChain, error) {
	tbl, ok := rbl.(bootloader.TrustedAssetsBootloader)
	if !ok {
		return nil, fmt.Errorf("recovery bootloader doesn't support trusted assets")
	}

	chains := make([]bootChain, 0, len(modeenv.CurrentKernels))
	for _, k := range modeenv.CurrentKernels {
		info, err := snap.ParsePlaceInfoFromSnapFileName(k)
		if err != nil {
			return nil, err
		}
		runModeBootChain, err := tbl.BootChain(bl, info.MountFile())
		if err != nil {
			return nil, err
		}

		// get asset chains
		assetChain, kbf, err := buildBootAssets(runModeBootChain, modeenv)
		if err != nil {
			return nil, err
		}
		var kernelRev string
		if info.SnapRevision().Store() {
			kernelRev = info.SnapRevision().String()
		}
		chains = append(chains, bootChain{
			BrandID:        model.BrandID(),
			Model:          model.Model(),
			Grade:          model.Grade(),
			ModelSignKeyID: model.SignKeyID(),
			AssetChain:     assetChain,
			Kernel:         info.SnapName(),
			KernelRevision: kernelRev,
			KernelCmdlines: []string{cmdline},
			model:          model,
			kernelBootFile: kbf,
		})
	}
	return chains, nil
}

func buildBootAssets(bootFiles []bootloader.BootFile, modeenv *Modeenv) (assets []bootAsset, kernel bootloader.BootFile, err error) {
	assets = make([]bootAsset, len(bootFiles)-1)

	for i, bf := range bootFiles[:len(bootFiles)-1] {
		name := filepath.Base(bf.Path)
		var hashes []string
		var ok bool
		if bf.Role == bootloader.RoleRecovery {
			hashes, ok = modeenv.CurrentTrustedRecoveryBootAssets[name]
		} else {
			hashes, ok = modeenv.CurrentTrustedBootAssets[name]
		}
		if !ok {
			return nil, kernel, fmt.Errorf("cannot find boot asset %s in modeenv", name)
		}
		assets[i] = bootAsset{
			Role:   bf.Role,
			Name:   name,
			Hashes: hashes,
		}
	}

	return assets, bootFiles[len(bootFiles)-1], nil
}

func sealKeyModelParams(pbc predictableBootChains, roleToBlName map[bootloader.Role]string) ([]*secboot.SealKeyModelParams, error) {
	// TODO:UC20: try to make one SealKeyModelParams per model, boot
	// chains are ordered, and chains sharing the model are grouped
	// together
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

	return modelParams, nil
}
