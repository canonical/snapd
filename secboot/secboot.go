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
	"errors"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget/device"
)

const (
	// The range 0x01880005-0x0188000F
	//
	// TODO:FDEM: we should apply for a subrange from UAPI group once
	// they got a range assigned by TCG.  See
	// https://github.com/uapi-group/specifications/pull/118
	// For now we use a sub range on the unassigned owner handles
	PCRPolicyCounterHandleStart = uint32(0x01880005)
	PCRPolicyCounterHandleRange = uint32(0x01880010 - 0x01880005)

	// These handles are legacy, do not use them in new code.
	//
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

var ErrKernelKeyNotFound = errors.New("kernel key not found")

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
	// The installation key to enroll a new key slot
	BootstrappedContainer BootstrappedContainer
	// The key name; identical keys should have identical names
	KeyName string
	// The name of the slot where they key will be saved.
	SlotName string
	// The file to store the key data. If empty, the key data will
	// be saved to the token.
	KeyFile string
	// The boot modes allowed (i.e. snapd_recovery_mode kernel parameter)
	BootModes []string
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

// TODO:FDEM: rename and drop Model from the name?
type SealKeyModelParams struct {
	// The snap model
	Model ModelForSealing
	// The set of EFI binary load chains for the current device
	// configuration
	EFILoadChains []*LoadChain
	// The kernel command line
	KernelCmdlines []string
	// TODO:FDEM: move this somewhere else?
	// The content of an update to EFI DBX
	EFISignatureDbxUpdate []byte
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
	// The primary key to use, nil if needs to be generated
	PrimaryKey []byte
	// The handle at which to create a NV index for dynamic authorization policy revocation support
	PCRPolicyCounterHandle uint32
	// The path to the authorization policy update key file (only relevant for TPM,
	// if empty the key will not be saved)
	TPMPolicyAuthKeyFile string
	// Optional volume authentication options
	VolumesAuth *device.VolumesAuthOptions
	// The key role (run, run+recover, recover)
	KeyRole string
	// Whether to allow disabled DMA protection
	AllowInsufficientDmaProtection bool
}

type SealKeysWithFDESetupHookParams struct {
	// Initial model to bind sealed keys to.
	Model ModelForSealing
	// The path to the aux key file (if empty the key will not be
	// saved)
	AuxKeyFile string
	// The primary key to use, nil if needs to be generated
	PrimaryKey []byte
}

// KeyDataLocation represents the possible places where key data
// might be saved.
//
// This is used for resealing keys in key data. The resealing will be
// responsible of finding which one is in use. The basic strategy is
// if a key data is found in the token, then this will be used and the
// key file will be ignored.
type KeyDataLocation struct {
	// KeyFile is the path to the file that contains either the key data or sealed key object.
	KeyFile string
	// DevicePath is the LUKS2 device which contains a token with they key data
	DevicePath string
	// SlotName is the name of the token that contains the key data
	SlotName string
}

// KeyData represents a disk unlock key protected by a platform's secure device.
type KeyData interface {
	PlatformName() string
	Roles() []string
	AuthMode() device.AuthMode
	// ChangePassphrase changes passphrase given old passphrase.
	// AuthMode must be device.AuthModePassphrase.
	ChangePassphrase(oldPassphrase, newPassphrase string) error
	// WriteTokenAtomic saves this key data to the specified LUKS2 token.
	WriteTokenAtomic(devicePath, slotName string) error
}

// SerializedPCRProfile wraps a serialized PCR profile which is treated as an
// opaque binary blob outside of secboot package.
type SerializedPCRProfile []byte

type ResealKeysParams struct {
	// The snap model parameters
	PCRProfile SerializedPCRProfile
	// The locations to the key data
	Keys []KeyDataLocation
	// The primary key
	PrimaryKey []byte
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
	// BootMode is the current boot mode (i.e. snapd_recovery_mode kernel parameter)
	BootMode string
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

type ProtectKeyParams struct {
	// The snap model parameter
	PCRProfile SerializedPCRProfile
	// The handle at which to create a NV index for dynamic authorization policy revocation support
	PCRPolicyCounterHandle uint32
	// The key role (run, run+recover, recover)
	KeyRole string
	// Optional volume authentication options
	VolumesAuth *device.VolumesAuthOptions
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
	sealingMethod, err := device.SealedKeysMethod(dirs.GlobalRootDir)
	if err != nil && err != device.ErrNoSealedKeys {
		return err
	}
	if sealingMethod == device.SealingMethodTPM {
		lockoutAuthFile := device.TpmLockoutAuthUnder(dirs.SnapFDEDirUnderSave(dirs.SnapSaveDir))
		// each unclean shtutdown will increase the DA lockout
		// counter. So on a successful boot we need to clear
		// this counter to avoid eventually hitting the
		// snapcore/secboot:tpm2/provisioning.go limit of
		// maxTries=32. Note that on a clean shtudown linux
		// will call TPM2_Shutdown which ensure no DA lockout
		// is increased.
		if err := resetLockoutCounter(lockoutAuthFile); err != nil {
			return err
		}
	}

	return nil
}

const (
	defaultKeyringPrefix = "ubuntu-fde"
)
