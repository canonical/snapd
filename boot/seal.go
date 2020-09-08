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

	recoveryBootChain, err := buildRecoveryBootChain(rbl, model, modeenv)
	if err != nil {
		return fmt.Errorf("cannot build recovery boot chain: %v", err)
	}
	recoveryCmdline, recoveryLoadChain := processBootChain(recoveryBootChain)

	// build the run mode boot chains
	bl, err := bootloader.Find(InitramfsUbuntuBootDir, &bootloader.Options{
		Role:        bootloader.RoleRunMode,
		NoSlashBoot: true,
	})
	if err != nil {
		return fmt.Errorf("cannot find the bootloader: %v", err)
	}

	runModeBootChain, err := buildRunModeBootChain(rbl, bl, model, modeenv)
	if err != nil {
		return fmt.Errorf("cannot build run mode boot chain: %v", err)
	}
	//runModeCmdline, runModeLoadChain := processBootChain(runModeBootChain)

	_ = recoveryLoadChain
	_ = runModeBootChain

	// TODO:UC20: binaries are EFI/bootloader-specific, hardcoded for now
	loadChain := []bootloader.BootFile{
		// the path to the shim EFI binary
		bootloader.NewBootFile("", filepath.Join(InitramfsUbuntuSeedDir, "EFI/boot/bootx64.efi"), bootloader.RoleRecovery),
		// the path to the recovery grub EFI binary
		bootloader.NewBootFile("", filepath.Join(InitramfsUbuntuSeedDir, "EFI/boot/grubx64.efi"), bootloader.RoleRecovery),
		// the path to the run mode grub EFI binary
		bootloader.NewBootFile("", filepath.Join(InitramfsUbuntuBootDir, "EFI/boot/grubx64.efi"), bootloader.RoleRunMode),
	}
	kernelPath := filepath.Join(InitramfsUbuntuBootDir, "EFI/ubuntu/kernel.efi")
	loadChain = append(loadChain, bootloader.NewBootFile("", kernelPath, bootloader.RoleRunMode))

	// Get the expected kernel command line for the system that is currently being installed
	cmdline, err := ComposeCandidateCommandLine(model)
	if err != nil {
		return fmt.Errorf("cannot obtain kernel command line: %v", err)
	}

	kernelCmdlines := []string{
		cmdline,
		recoveryCmdline,
	}

	sealKeyParams := secboot.SealKeyParams{
		ModelParams: []*secboot.SealKeyModelParams{
			{
				Model:          model,
				KernelCmdlines: kernelCmdlines,
				EFILoadChains:  [][]bootloader.BootFile{loadChain},
			},
		},
		KeyFile:                 filepath.Join(InitramfsEncryptionKeyDir, "ubuntu-data.sealed-key"),
		TPMPolicyUpdateDataFile: filepath.Join(InstallHostFDEDataDir, "policy-update-data"),
		TPMLockoutAuthFile:      filepath.Join(InstallHostFDEDataDir, "tpm-lockout-auth"),
	}

	if err := secbootSealKey(key, &sealKeyParams); err != nil {
		return fmt.Errorf("cannot seal the encryption key: %v", err)
	}

	return nil
}

func buildRecoveryBootChain(rbl bootloader.Bootloader, model *asserts.Model, modeenv *Modeenv) (*bootChain, error) {
	// get the command line
	cmdline, err := ComposeRecoveryCommandLine(model, modeenv.RecoverySystem)
	if err != nil {
		return nil, fmt.Errorf("cannot obtain recovery kernel command line: %v", err)
	}

	// get kernel information from seed
	perf := timings.New(nil)
	_, snaps, err := seed.ReadSystemEssential(dirs.SnapSeedDir, modeenv.RecoverySystem, []snap.Type{snap.TypeKernel}, perf)
	if err != nil {
		return nil, err
	}
	if len(snaps) != 1 {
		return nil, fmt.Errorf("cannot obtain recovery kernel snap")
	}
	kernel := snaps[0]

	var kernelRev string
	if kernel.SideInfo.Revision.Store() {
		kernelRev = kernel.SideInfo.Revision.String()
	}

	// get asset chains
	assetChain, err := buildRecoveryAssetChain(rbl, modeenv)
	if err != nil {
		return nil, err
	}

	return &bootChain{
		BrandID:        model.BrandID(),
		Model:          model.Model(),
		Grade:          string(model.Grade()),
		ModelSignKeyID: model.SignKeyID(),
		AssetChain:     assetChain,
		Kernel:         kernel.Path,
		KernelRevision: kernelRev,
		KernelCmdline:  cmdline,
	}, nil
}

func buildRunModeBootChain(rbl, bl bootloader.Bootloader, model *asserts.Model, modeenv *Modeenv) ([]*bootChain, error) {
	// get the command line
	cmdline, err := ComposeCandidateCommandLine(model)
	if err != nil {
		return nil, fmt.Errorf("cannot obtain kernel command line: %v", err)
	}

	// get asset chains
	assetChain, err := buildRunModeAssetChain(rbl, bl, modeenv)
	if err != nil {
		return nil, err
	}

	// get run mode kernels
	runModeKernels, err := runModeKernelsFromModeenv(modeenv)
	if err != nil {
		return nil, err
	}

	chains := make([]*bootChain, 0, 2)
	for _, k := range runModeKernels {
		chains = append(chains, &bootChain{
			BrandID:        model.BrandID(),
			Model:          model.Model(),
			Grade:          string(model.Grade()),
			ModelSignKeyID: model.SignKeyID(),
			AssetChain:     assetChain,
			Kernel:         k,
			// XXX: obtain revision
			KernelRevision: "",
			KernelCmdline:  cmdline,
		})
	}

	return chains, nil
}

func buildRecoveryAssetChain(rbl bootloader.Bootloader, modeenv *Modeenv) ([]bootAsset, error) {
	tbl, ok := rbl.(bootloader.TrustedAssetsBootloader)
	if !ok {
		return nil, fmt.Errorf("bootloader doesn't support trusted assets")
	}

	recoveryBootChain, err := tbl.RecoveryBootChain("")
	if err != nil {
		return nil, err
	}

	// the last entry in the bootChain is the kernel
	numAssets := len(recoveryBootChain) - 1
	assets := make([]bootAsset, numAssets)

	for i := 0; i < numAssets; i++ {
		name := filepath.Base(recoveryBootChain[i].Path)
		hashes, ok := modeenv.CurrentTrustedRecoveryBootAssets[name]
		if !ok {
			return nil, fmt.Errorf("cannot find asset %s in modeenv", name)
		}
		assets[i] = bootAsset{
			Role:   string(recoveryBootChain[i].Role),
			Name:   name,
			Hashes: hashes,
		}
	}

	return assets, nil
}

func buildRunModeAssetChain(rbl, bl bootloader.Bootloader, modeenv *Modeenv) ([]bootAsset, error) {
	tbl, ok := rbl.(bootloader.TrustedAssetsBootloader)
	if !ok {
		return nil, fmt.Errorf("recovery bootloader doesn't support trusted assets")
	}

	recoveryBootChain, err := tbl.RecoveryBootChain("")
	if err != nil {
		return nil, err
	}
	numRecoveryAssets := len(recoveryBootChain) - 1

	runModeBootChain, err := tbl.BootChain(bl, "")
	if err != nil {
		return nil, err
	}
	numRunModeAssets := len(runModeBootChain) - 1

	assets := make([]bootAsset, numRecoveryAssets+numRunModeAssets)

	for i := 0; i < numRecoveryAssets; i++ {
		name := filepath.Base(recoveryBootChain[i].Path)
		hashes, ok := modeenv.CurrentTrustedRecoveryBootAssets[name]
		if !ok {
			return nil, fmt.Errorf("cannot find asset %s in modeenv", name)
		}
		assets[i] = bootAsset{
			Role:   string(recoveryBootChain[i].Role),
			Name:   name,
			Hashes: hashes,
		}
	}
	for i := numRecoveryAssets; i < numRecoveryAssets+numRunModeAssets; i++ {
		name := filepath.Base(runModeBootChain[i].Path)
		hashes, ok := modeenv.CurrentTrustedBootAssets[name]
		if !ok {
			return nil, fmt.Errorf("cannot find asset %s in modeenv", name)
		}
		assets[i] = bootAsset{
			Role:   string(runModeBootChain[i].Role),
			Name:   name,
			Hashes: hashes,
		}
	}

	return assets, nil
}

func cachedAssetHashes(blName, name string, assetsMap bootAssetsMap) ([]string, error) {
	hashes, ok := assetsMap[name]
	if !ok {
		return nil, fmt.Errorf("cannot find asset %s in modeenv", name)
	}

	return hashes, nil
}

func processBootChain(bc *bootChain) (string, []bootloader.BootFile) {
	seq := []bootloader.BootFile{}
	return bc.KernelCmdline, seq
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
