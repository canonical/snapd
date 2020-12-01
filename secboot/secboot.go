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

package secboot

// This file must not have a build-constraint and must not import
// the github.com/snapcore/secboot repository. That will ensure
// it can be build as part of the debian build without secboot.
// Debian does run "go list" without any support for passing -tags.

import (
	"crypto/ecdsa"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/bootloader"
)

const (
	// Handles are in the block reserved for TPM owner objects (0x01800000 - 0x01bfffff)
	RunObjectPCRPolicyCounterHandle      = 0x01880001
	FallbackObjectPCRPolicyCounterHandle = 0x01880002
)

type LoadChain struct {
	*bootloader.BootFile
	// Next is a list of alternative chains that can be loaded
	// following the boot file.
	Next []*LoadChain
}

// NewLoadChain returns a LoadChain corresponding to loading the given
// BootFile before any of the given next chains.
func NewLoadChain(bf bootloader.BootFile, next ...*LoadChain) *LoadChain {
	return &LoadChain{
		BootFile: &bf,
		Next:     next,
	}
}

type SealKeyRequest struct {
	// The key to seal
	Key EncryptionKey
	// The path to store the sealed key file
	KeyFile string
}

type SealKeyModelParams struct {
	// The snap model
	Model *asserts.Model
	// The set of EFI binary load chains for the current device
	// configuration
	EFILoadChains []*LoadChain
	// The kernel command line
	KernelCmdlines []string
}

type SealKeysParams struct {
	// The parameters we're sealing the key to
	ModelParams []*SealKeyModelParams
	// The authorization policy update key file (only relevant for TPM)
	TPMPolicyAuthKey *ecdsa.PrivateKey
	// The path to the authorization policy update key file (only relevant for TPM,
	// if empty the key will not be saved)
	TPMPolicyAuthKeyFile string
	// The path to the lockout authorization file (only relevant for TPM and only
	// used if TPMProvision is set to true)
	TPMLockoutAuthFile string
	// Whether we should provision the TPM
	TPMProvision bool
	// The handle at which to create a NV index for dynamic authorization policy revocation support
	PCRPolicyCounterHandle uint32
}

type ResealKeysParams struct {
	// The snap model parameters
	ModelParams []*SealKeyModelParams
	// The path to the sealed key files
	KeyFiles []string
	// The path to the authorization policy update key file (only relevant for TPM)
	TPMPolicyAuthKeyFile string
}

// UnlockVolumeUsingSealedKeyOptions contains options for unlocking encrypted
// volumes using keys sealed to the TPM.
type UnlockVolumeUsingSealedKeyOptions struct {
	// AllowRecoveryKey when true indicates activation with the recovery key
	// will be attempted if activation with the sealed key failed.
	AllowRecoveryKey bool
}

// UnlockMethod is the method that was used to unlock a volume.
type UnlockMethod int

const (
	// NotUnlocked indicates that the device was either not unlocked or is not
	// an encrypted device.
	NotUnlocked UnlockMethod = iota
	// UnlockedWithSealedKey indicates that the device was unlocked with the
	// provided sealed key object.
	UnlockedWithSealedKey
	// UnlockedWithRecoveryKey indicates that the device was unlocked by the
	// user providing the recovery key at the prompt.
	UnlockedWithRecoveryKey
	// UnlockedWithKey indicates that the device was unlocked with the provided
	// key, which is not sealed.
	UnlockedWithKey
	// UnlockStatusUnknown indicates that the unlock status of the device is not clear.
	UnlockStatusUnknown
)

// UnlockResult is the result of trying to unlock a volume.
type UnlockResult struct {
	// FsDevice is the device with filesystem ready to mount.
	// It is the activated device if encrypted or just
	// the underlying device (same as PartDevice) if non-encrypted.
	// FsDevice can be empty when none was found.
	FsDevice string
	// PartDevice is the underlying partition device.
	// PartDevice can be empty when no device was found.
	PartDevice string
	// IsEncrypted indicates that PartDevice is encrypted.
	IsEncrypted bool
	// UnlockMethod is the method used to unlock the device. Valid values are
	// - NotUnlocked
	// - UnlockedWithRecoveryKey
	// - UnlockedWithSealedKey
	// - UnlockedWithKey
	UnlockMethod UnlockMethod
}

// FDEHasReveal is setup by devicestate/fde to support device-specific
// full disk encryption implementations.
var FDEHasRevealKey = func() bool { return false }
