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
	"github.com/snapcore/snapd/secboot"
)

var (
	secbootSealKey = secboot.SealKey
)

// sealKeyToModeenv seals the supplied key to the parameters specified
// in modeenv.
func sealKeyToModeenv(key secboot.EncryptionKey, model *asserts.Model, modeenv *Modeenv) error {
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
