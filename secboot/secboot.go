// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021-2022 Canonical Ltd
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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget/device"
	"github.com/snapcore/snapd/secboot/keys"
)

const (
	// Handles are in the block reserved for TPM owner objects (0x01800000 - 0x01bfffff).
	//
	// Handles are rotated during factory reset, depending on the PCR handle
	// thet was used when sealing key objects during installation (or a
	// previous factory reset).
	RunObjectPCRPolicyCounterHandle         = uint32(0x01880001)
	FallbackObjectPCRPolicyCounterHandle    = uint32(0x01880002)
	AltRunObjectPCRPolicyCounterHandle      = uint32(0x01880003)
	AltFallbackObjectPCRPolicyCounterHandle = uint32(0x01880004)
)

// WithSecbootSupport is true if this package was built with githbu.com/snapcore/secboot.
var WithSecbootSupport = false

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
	Key keys.EncryptionKey
	// The key name; identical keys should have identical names
	KeyName string
	// The path to store the sealed key file. The same Key/KeyName
	// can be stored under multiple KeyFile names for safety.
	KeyFile string
}

// ModelForSealing provides information about the model for use in the context
// of (re)sealing the encryption keys.
type ModelForSealing interface {
	Series() string
	BrandID() string
	Model() string
	Classic() bool
	Grade() asserts.ModelGrade
	SignKeyID() string
}

type SealKeyModelParams struct {
	// The snap model
	Model ModelForSealing
	// The set of EFI binary load chains for the current device
	// configuration
	EFILoadChains []*LoadChain
	// The kernel command line
	KernelCmdlines []string
}

type TPMProvisionMode int

const (
	TPMProvisionNone TPMProvisionMode = iota
	// TPMProvisionFull indicates a full provisioning of the TPM
	TPMProvisionFull
	// TPMPartialReprovision indicates a partial reprovisioning of the TPM
	// which was previously already provisioned by secboot. Existing lockout
	// authorization data from TPMLockoutAuthFile will be used to authorize
	// provisioning and will get overwritten in the process.
	TPMPartialReprovision
	// TPMProvisionFullWithoutLockout indicates full provisioning
	// without using lockout authorization data, as currently used
	// by Azure CVM
	TPMProvisionFullWithoutLockout
)

type SealKeysParams struct {
	// The parameters we're sealing the key to
	ModelParams []*SealKeyModelParams
	// The authorization policy update key file (only relevant for TPM)
	TPMPolicyAuthKey *ecdsa.PrivateKey
	// The path to the authorization policy update key file (only relevant for TPM,
	// if empty the key will not be saved)
	TPMPolicyAuthKeyFile string
	// The handle at which to create a NV index for dynamic authorization policy revocation support
	PCRPolicyCounterHandle uint32
}

type SealKeysWithFDESetupHookParams struct {
	// Initial model to bind sealed keys to.
	Model ModelForSealing
	// AuxKey is the auxiliary key used to bind models.
	AuxKey keys.AuxKey
	// The path to the aux key file (if empty the key will not be
	// saved)
	AuxKeyFile string
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
	// WhichModel if invoked should return the device model
	// assertion for which the disk is being unlocked.
	WhichModel func() (*asserts.Model, error)
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

// EncryptedPartitionName returns the name/label used by an encrypted partition
// corresponding to a given name.
func EncryptedPartitionName(name string) string {
	return name + "-enc"
}

// MarkSuccessful marks the secure boot parts of the boot as
// successful.
//
// This means that the dictionary attack (DA) lockout counter is reset.
func MarkSuccessful() error {
	sealingMethod := mylog.Check2(device.SealedKeysMethod(dirs.GlobalRootDir))
	if err != nil && err != device.ErrNoSealedKeys {
		return err
	}
	if sealingMethod == device.SealingMethodTPM {
		lockoutAuthFile := device.TpmLockoutAuthUnder(dirs.SnapFDEDirUnderSave(dirs.SnapSaveDir))
		mylog.Check(
			// each unclean shtutdown will increase the DA lockout
			// counter. So on a successful boot we need to clear
			// this counter to avoid eventually hitting the
			// snapcore/secboot:tpm2/provisioning.go limit of
			// maxTries=32. Note that on a clean shtudown linux
			// will call TPM2_Shutdown which ensure no DA lockout
			// is increased.
			resetLockoutCounter(lockoutAuthFile))

	}

	return nil
}
