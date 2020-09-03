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
	// Build the recover mode load sequences
	rbl, err := bootloader.Find(InitramfsUbuntuSeedDir, &bootloader.Options{
		Role: bootloader.RoleRecovery,
	})
	if err != nil {
		return fmt.Errorf("cannot find the recovery bootloader: %v", err)
	}

	recoverModeChains, err := recoverModeLoadSequences(rbl, modeenv)
	if err != nil {
		return fmt.Errorf("cannot build recover mode load sequences: %v", err)
	}

	bl, err := bootloader.Find(InitramfsUbuntuBootDir, &bootloader.Options{
		Role:        bootloader.RoleRunMode,
		NoSlashBoot: true,
	})
	if err != nil {
		return fmt.Errorf("cannot find the bootloader: %v", err)
	}

	runModeChains, err := runModeLoadSequences(rbl, bl, modeenv)
	if err != nil {
		return fmt.Errorf("cannot build run mode load sequences: %v", err)
	}

	// TODO:UC20: retrieve command lines from modeenv, the format is still TBD
	// Get the expected kernel command line for the system that is currently being installed
	cmdline, err := ComposeCandidateCommandLine(model)
	if err != nil {
		return fmt.Errorf("cannot obtain kernel command line: %v", err)
	}
	// Get the expected kernel command line of the recovery system we're installing from
	recoveryCmdline, err := ComposeRecoveryCommandLine(model, modeenv.RecoverySystem)
	if err != nil {
		return fmt.Errorf("cannot obtain recovery kernel command line: %v", err)
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
				EFILoadChains:  append(runModeChains, recoverModeChains...),
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

// recoverModeLoadSequences builds the recovery boot file list based on
// the boot chain retrieved from the bootloader and trusted assets listed
// in modeenv.
func recoverModeLoadSequences(rbl bootloader.Bootloader, modeenv *Modeenv) ([][]bootloader.BootFile, error) {
	tbl, ok := rbl.(bootloader.TrustedAssetsBootloader)
	if !ok {
		return nil, fmt.Errorf("bootloader doesn't support trusted assets")
	}

	kernelPath, err := kernelPathForRecoverySystem(modeenv.RecoverySystem)
	if err != nil {
		return nil, err
	}

	bootChain, err := tbl.RecoveryBootChain(kernelPath)
	if err != nil {
		return nil, err
	}

	seq0 := make([]bootloader.BootFile, 0, len(bootChain))
	seq1 := make([]bootloader.BootFile, 0, len(bootChain))

	// walk the chain and get cache entries for the trusted assets
	for i := 0; i < len(bootChain)-1; i++ {
		bf := bootChain[i]
		name := filepath.Base(bf.Path)

		if bf.Role != bootloader.RoleRecovery {
			return nil, fmt.Errorf("internal error: recovery boot chain contains invalid role %v", bf.Role)
		}

		p0, p1, err := cachedAssetPathnames(rbl.Name(), name, modeenv.CurrentTrustedRecoveryBootAssets)
		if err != nil {
			return nil, err
		}
		seq0 = append(seq0, bf.WithPath(p0))
		seq1 = append(seq1, bf.WithPath(p1))
	}

	// add the recovery kernel
	bf := bootChain[len(bootChain)-1]
	seq0 = append(seq0, bf)
	seq1 = append(seq1, bf)

	if sequenceEqual(seq0, seq1) {
		return [][]bootloader.BootFile{seq0}, nil
	}

	return [][]bootloader.BootFile{seq0, seq1}, nil
}

// kernelPathForRecoverySystem validates the kernel for the specified
// recovery system and returns the path to the kernel snap.
func kernelPathForRecoverySystem(recoverySystem string) (string, error) {
	systemSeed, err := seed.Open(dirs.SnapSeedDir, recoverySystem)
	if err != nil {
		return "", err
	}
	seed20, ok := systemSeed.(seed.EssentialMetaLoaderSeed)
	if !ok {
		return "", fmt.Errorf("internal error: UC20 seed must implement EssentialMetaLoaderSeed")
	}

	// load assertions into a temporary database
	if err := seed20.LoadAssertions(nil, nil); err != nil {
		return "", err
	}

	// load and verify the kernel metadata
	perf := timings.New(nil)
	if err := seed20.LoadEssentialMeta([]snap.Type{snap.TypeKernel}, perf); err != nil {
		return "", err
	}

	snaps := seed20.EssentialSnaps()
	if len(snaps) != 1 {
		return "", fmt.Errorf("cannot obtain recovery kernel snap")
	}

	return snaps[0].Path, nil
}

// runModeLoadSequences builds the run mode boot file list based on
// the boot chains retrieved from the bootloaders and trusted assets
// listed in modeenv.
func runModeLoadSequences(rbl, bl bootloader.Bootloader, modeenv *Modeenv) ([][]bootloader.BootFile, error) {
	trbl, ok := rbl.(bootloader.TrustedAssetsBootloader)
	if !ok {
		return nil, fmt.Errorf("recovery bootloader doesn't support trusted assets")
	}

	k0, k1, err := runModeKernelsFromModeenv(modeenv)
	if err != nil {
		return nil, err
	}

	bootChain, err := trbl.BootChain(bl, k0)
	if err != nil {
		return nil, err
	}

	seq0 := make([]bootloader.BootFile, 0, len(bootChain))
	seq1 := make([]bootloader.BootFile, 0, len(bootChain))

	// walk the chain and get cache entries for the trusted assets
	for i := 0; i < len(bootChain)-1; i++ {
		bf := bootChain[i]
		name := filepath.Base(bf.Path)

		var bootAssets bootAssetsMap
		switch bf.Role {
		case bootloader.RoleRecovery:
			bootAssets = modeenv.CurrentTrustedRecoveryBootAssets
		case bootloader.RoleRunMode:
			bootAssets = modeenv.CurrentTrustedBootAssets
		default:
			return nil, fmt.Errorf("internal error: the run mode boot chain contains invalid role %v", bf.Role)
		}

		p0, p1, err := cachedAssetPathnames(bl.Name(), name, bootAssets)
		if err != nil {
			return nil, err
		}
		seq0 = append(seq0, bf.WithPath(p0))
		seq1 = append(seq1, bf.WithPath(p1))
	}

	// add the run mode kernel
	bf := bootChain[len(bootChain)-1]
	seq0 = append(seq0, bf)
	seq1 = append(seq1, bootloader.NewBootFile(k1, bf.Path, bf.Role))

	if sequenceEqual(seq0, seq1) {
		return [][]bootloader.BootFile{seq0}, nil
	}

	return [][]bootloader.BootFile{seq0, seq1}, nil
}

// runModeKernelsFromModeenv obtains the current and next kernels
// listed in modeenv.
func runModeKernelsFromModeenv(modeenv *Modeenv) (string, string, error) {
	switch len(modeenv.CurrentKernels) {
	case 1:
		current := filepath.Join(dirs.SnapBlobDir, modeenv.CurrentKernels[0])
		return current, current, nil
	case 2:
		current := filepath.Join(dirs.SnapBlobDir, modeenv.CurrentKernels[0])
		next := filepath.Join(dirs.SnapBlobDir, modeenv.CurrentKernels[1])
		return current, next, nil
	}
	return "", "", fmt.Errorf("invalid number of kernels in modeenv")
}

// cachedAssetPathnames returns the pathnames of the files corresponding
// to the current and next instances of a given boot asset.
func cachedAssetPathnames(blName, name string, assetsMap bootAssetsMap) (current, next string, err error) {
	cacheEntry := func(hash string) string {
		return filepath.Join(dirs.SnapBootAssetsDir, blName, fmt.Sprintf("%s-%s", name, hash))
	}

	hashList, ok := assetsMap[name]
	if !ok {
		return "", "", fmt.Errorf("cannot find asset %s in modeenv", name)
	}

	switch len(hashList) {
	case 1:
		current = cacheEntry(hashList[0])
		next = current
	case 2:
		current = cacheEntry(hashList[0])
		next = cacheEntry(hashList[1])
	default:
		return "", "", fmt.Errorf("invalid number of hashes for asset %s in modeenv", name)
	}
	return current, next, nil
}

func sequenceEqual(a, b []bootloader.BootFile) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
