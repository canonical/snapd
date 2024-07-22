// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nosecboot

/*
 * Copyright (C) 2021-2024 Canonical Ltd
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

import (
	"crypto/rand"
	"errors"
	"fmt"
	"path/filepath"

	sb "github.com/snapcore/secboot"
	sb_hooks "github.com/snapcore/secboot/hooks"
	sb_plainkey "github.com/snapcore/secboot/plainkey"
	sb_tpm2 "github.com/snapcore/secboot/tpm2"
	"golang.org/x/xerrors"

	"github.com/snapcore/snapd/kernel/fde"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/secboot/keys"
)

var (
	sbActivateVolumeWithKey         = sb.ActivateVolumeWithKey
	sbActivateVolumeWithKeyData     = sb.ActivateVolumeWithKeyData
	sbActivateVolumeWithRecoveryKey = sb.ActivateVolumeWithRecoveryKey
	sbDeactivateVolume              = sb.DeactivateVolume
	sbAddLUKS2ContainerUnlockKey    = sb.AddLUKS2ContainerUnlockKey
)

func init() {
	WithSecbootSupport = true
}

type DiskUnlockKey sb.DiskUnlockKey
type ActivateVolumeOptions sb.ActivateVolumeOptions

// LockSealedKeys manually locks access to the sealed keys. Meant to be
// called in place of passing lockKeysOnFinish as true to
// UnlockVolumeUsingSealedKeyIfEncrypted for cases where we don't know if a
// given call is the last one to unlock a volume like in degraded recover mode.
func LockSealedKeys() error {
	if fdeHasRevealKey() {
		return fde.LockSealedKeys()
	}
	return lockTPMSealedKeys()
}

// UnlockVolumeUsingSealedKeyIfEncrypted verifies whether an encrypted volume
// with the specified name exists and unlocks it using a sealed key in a file
// with a corresponding name. The options control activation with the
// recovery key will be attempted if a prior activation attempt with
// the sealed key fails.
//
// Note that if the function proceeds to the point where it knows definitely
// whether there is an encrypted device or not, IsEncrypted on the return
// value will be true, even if error is non-nil. This is so that callers can be
// robust and try unlocking using another method for example.
func UnlockVolumeUsingSealedKeyIfEncrypted(disk disks.Disk, name string, sealedEncryptionKeyFile string, opts *UnlockVolumeUsingSealedKeyOptions) (UnlockResult, error) {
	res := UnlockResult{}

	// find the encrypted device using the disk we were provided - note that
	// we do not specify IsDecryptedDevice in opts because here we are
	// looking for the encrypted device to unlock, later on in the boot
	// process we will look for the decrypted device to ensure it matches
	// what we expected
	partUUID, err := disk.FindMatchingPartitionUUIDWithFsLabel(EncryptedPartitionName(name))
	if err == nil {
		res.IsEncrypted = true
	} else {
		var errNotFound disks.PartitionNotFoundError
		if !xerrors.As(err, &errNotFound) {
			// some other kind of catastrophic error searching
			return res, fmt.Errorf("error enumerating partitions for disk to find encrypted device %q: %v", name, err)
		}
		// otherwise it is an error not found and we should search for the
		// unencrypted device
		partUUID, err = disk.FindMatchingPartitionUUIDWithFsLabel(name)
		if err != nil {
			return res, fmt.Errorf("error enumerating partitions for disk to find unencrypted device %q: %v", name, err)
		}
	}

	partDevice := filepath.Join("/dev/disk/by-partuuid", partUUID)

	if !res.IsEncrypted {
		// if we didn't find an encrypted device just return, don't try to
		// unlock it the filesystem device for the unencrypted case is the
		// same as the partition device
		res.PartDevice = partDevice
		res.FsDevice = res.PartDevice
		return res, nil
	}

	uuid, err := randutilRandomKernelUUID()
	if err != nil {
		// We failed before we could generate the filsystem device path for
		// the encrypted partition device, so we return FsDevice empty.
		res.PartDevice = partDevice
		return res, err
	}

	// make up a new name for the mapped device
	mapperName := name + "-" + uuid
	sourceDevice := partDevice
	targetDevice := filepath.Join("/dev/mapper", mapperName)

	if osutil.FileExists(sealedEncryptionKeyFile) {
		if fdeHasRevealKey() {
			return unlockVolumeUsingSealedKeyFDERevealKey(sealedEncryptionKeyFile, sourceDevice, targetDevice, mapperName, opts)
		} else {
			return unlockVolumeUsingSealedKeyTPM(name, sealedEncryptionKeyFile, sourceDevice, targetDevice, mapperName, opts)
		}
	} else {
		return unlockVolumeUsingSealedKeyGeneric(name, sourceDevice, targetDevice, mapperName, opts)
	}
}

// UnlockEncryptedVolumeUsingKey unlocks an existing volume using the provided key.
func UnlockEncryptedVolumeUsingPlatformKey(disk disks.Disk, name string, key []byte) (UnlockResult, error) {
	unlockRes := UnlockResult{
		UnlockMethod: NotUnlocked,
	}
	// find the encrypted device using the disk we were provided - note that
	// we do not specify IsDecryptedDevice in opts because here we are
	// looking for the encrypted device to unlock, later on in the boot
	// process we will look for the decrypted device to ensure it matches
	// what we expected
	partUUID, err := disk.FindMatchingPartitionUUIDWithFsLabel(EncryptedPartitionName(name))
	if err != nil {
		return unlockRes, err
	}
	unlockRes.IsEncrypted = true
	// we have a device
	encdev := filepath.Join("/dev/disk/by-partuuid", partUUID)
	unlockRes.PartDevice = encdev

	uuid, err := randutilRandomKernelUUID()
	if err != nil {
		// We failed before we could generate the filsystem device path for
		// the encrypted partition device, so we return FsDevice empty.
		return unlockRes, err
	}

	// make up a new name for the mapped device
	mapperName := name + "-" + uuid

	slots, err := sbListLUKS2ContainerUnlockKeyNames(encdev)
	if err != nil {
		return unlockRes, err
	}
	keyExists := false
	for _, slot := range slots {
		if slot == "default-fallback" {
			keyExists = true
			break
		}
	}

	if keyExists {
		sb_plainkey.SetPlatformKeys(key)

		options := sb.ActivateVolumeOptions{}
		if err := sbActivateVolumeWithKeyData(mapperName, encdev, nil, &options); err != nil {
			return unlockRes, err
		}
	} else {
		if err := unlockEncryptedPartitionWithKey(mapperName, encdev, key); err != nil {
			return unlockRes, err
		}
	}

	unlockRes.FsDevice = filepath.Join("/dev/mapper/", mapperName)
	unlockRes.UnlockMethod = UnlockedWithKey
	return unlockRes, nil
}

// unlockEncryptedPartitionWithKey unlocks encrypted partition with the provided
// key.
func unlockEncryptedPartitionWithKey(name, device string, key []byte) error {
	// no special options set
	options := sb.ActivateVolumeOptions{}
	err := sbActivateVolumeWithKey(name, device, key, &options)
	if err == nil {
		logger.Noticef("successfully activated encrypted device %v using a key", device)
	}
	return err
}

// UnlockEncryptedVolumeWithRecoveryKey prompts for the recovery key and uses it
// to open an encrypted device.
func UnlockEncryptedVolumeWithRecoveryKey(name, device string) error {
	options := sb.ActivateVolumeOptions{
		RecoveryKeyTries: 3,
		KeyringPrefix:    keyringPrefix,
	}

	authRequestor, err := newAuthRequestor()
	if err != nil {
		return fmt.Errorf("cannot build an auth requestor: %v", err)
	}

	if err := sbActivateVolumeWithRecoveryKey(name, device, authRequestor, &options); err != nil {
		return fmt.Errorf("cannot unlock encrypted device %q: %v", device, err)
	}

	return nil
}

func ActivateVolumeWithKey(volumeName, sourceDevicePath string, key []byte, options *ActivateVolumeOptions) error {
	return sb.ActivateVolumeWithKey(volumeName, sourceDevicePath, key, (*sb.ActivateVolumeOptions)(options))
}

func DeactivateVolume(volumeName string) error {
	return sb.DeactivateVolume(volumeName)
}

func AddInstallationKeyOnExistingDisk(node string, newKey keys.EncryptionKey) error {
	const defaultPrefix = "ubuntu-fde"
	unlockKey, err := sbGetDiskUnlockKeyFromKernel(defaultPrefix, node, false)
	if err != nil {
		return fmt.Errorf("cannot get key for unlocked disk %s: %v", node, err)
	}

	if err := sbAddLUKS2ContainerUnlockKey(node, "installation-key", sb.DiskUnlockKey(unlockKey), sb.DiskUnlockKey(newKey)); err != nil {
		return fmt.Errorf("cannot enroll new installation key: %v", err)
	}

	return nil
}

func RenameOrDeleteKeys(node string, renames map[string]string) error {
	// FIXME: listing keys, then modifying could be a TOCTOU issue.
	// we expect here nothing else is messing with the key slots.
	slots, err := sb.ListLUKS2ContainerUnlockKeyNames(node)
	if err != nil {
		return fmt.Errorf("cannot list slots in partition save partition: %v", err)
	}
	for _, slot := range slots {
		renameTo, found := renames[slot]
		if found {
			if err := sb.RenameLUKS2ContainerKey(node, slot, renameTo); err != nil {
				if errors.Is(err, sb.ErrMissingCryptsetupFeature) {
					if err := sb.DeleteLUKS2ContainerKey(node, slot); err != nil {
						return fmt.Errorf("cannot remove old container key: %v", err)
					}
				} else {
					return fmt.Errorf("cannot rename container key: %v", err)
				}
			}
		}
	}

	return nil
}

func DeleteKeys(node string, matches map[string]bool) error {
	slots, err := sb.ListLUKS2ContainerUnlockKeyNames(node)
	if err != nil {
		return fmt.Errorf("cannot list slots in partition save partition: %v", err)
	}

	for _, slot := range slots {
		if matches[slot] {
			if err := sb.DeleteLUKS2ContainerKey(node, slot); err != nil {
				return fmt.Errorf("cannot remove old container key: %v", err)
			}
		}
	}

	return nil
}

func ResealKeysNextGeneration(devices []string, modelParams map[string][]*SealKeyModelParams) error {
	type keyDataAndSlot struct {
		KeyData  *sb.KeyData
		SlotName string
		Device   string
	}
	byPlatform := make(map[string][]*keyDataAndSlot)
	for _, device := range devices {
		slots, err := sbListLUKS2ContainerUnlockKeyNames(device)
		if err != nil {
			return err
		}
		for _, slotName := range slots {
			reader, err := sb.NewLUKS2KeyDataReader(device, slotName)
			if err != nil {
				return err
			}
			keyData, err := sb.ReadKeyData(reader)
			if err != nil {
				return err
			}
			if keyData.PlatformName() == legacyFdeHooksPlatformName || keyData.Generation() == 1 {
				return fmt.Errorf("Wrong resealing")
			}
			switch keyData.PlatformName() {
			case "fde-hooks-v3":
			case "tpm2":
				byPlatform[keyData.PlatformName()] = append(byPlatform[keyData.PlatformName()], &keyDataAndSlot{keyData, slotName, device})
			default:
			}
		}
	}

	const defaultPrefix = "ubuntu-fde"
	const remove = false
	var primaryKey sb.PrimaryKey
	var errors []error
	foundPrimaryKey := false
	for _, device := range devices {
		var err error
		primaryKey, err = sb.GetPrimaryKeyFromKernel(defaultPrefix, device, remove)
		if err == nil {
			foundPrimaryKey = true
			break
		}
		errors = append(errors, err)
	}
	if !foundPrimaryKey {
		return fmt.Errorf("no primary key found")
	}

	hooksKS := make(map[string][]*sb.KeyData)
	for _, ks := range byPlatform["fde-hooks-v3"] {
		hooksKS[ks.KeyData.Role()] = append(hooksKS[ks.KeyData.Role()], ks.KeyData)
	}

	for role, keyDatas := range hooksKS {
		var sbModels []sb.SnapModel
		for _, p := range modelParams[role] {
			sbModels = append(sbModels, p.Model)
		}

		for _, kd := range keyDatas {
			hooksKeyData, err := sb_hooks.NewKeyData(kd)
			if err != nil {
				return fmt.Errorf("cannot read key data as hook key data: %v", err)
			}
			hooksKeyData.SetAuthorizedSnapModels(rand.Reader, primaryKey, sbModels...)
		}
	}

	tpmKS := make(map[string][]*sb.KeyData)
	for _, ks := range byPlatform["tpm2"] {
		tpmKS[ks.KeyData.Role()] = append(tpmKS[ks.KeyData.Role()], ks.KeyData)
	}

	tpm, err := sbConnectToDefaultTPM()
	if err != nil {
		return fmt.Errorf("cannot connect to TPM: %v", err)
	}
	defer tpm.Close()
	if !isTPMEnabled(tpm) {
		return fmt.Errorf("TPM device is not enabled")
	}

	for role, keyDatas := range tpmKS {
		mp, ok := modelParams[role]
		if !ok {
			continue
		}

		pcrProfile, err := buildPCRProtectionProfile(mp)
		if err != nil {
			return fmt.Errorf("cannot build new PCR protection profile: %w", err)
		}

		// TODO: find out which context when revocation should happen
		if err := sbUpdateKeyDataPCRProtectionPolicy(tpm, primaryKey, pcrProfile, sb_tpm2.NoNewPCRPolicyVersion, keyDatas...); err != nil {
			return fmt.Errorf("cannot update PCR protection policy: %w", err)
		}
	}

	for _, p := range byPlatform {
		for _, ks := range p {
			writer, err := sb.NewLUKS2KeyDataWriter(ks.Device, ks.SlotName)
			if err != nil {
				return err
			}
			if err := ks.KeyData.WriteAtomic(writer); err != nil {
				return err
			}
		}
	}

	return nil
}

func unlockVolumeUsingSealedKeyGeneric(name, sourceDevice, targetDevice, mapperName string, opts *UnlockVolumeUsingSealedKeyOptions) (UnlockResult, error) {
	// TODO:UC20: use sb.SecureConnectToDefaultTPM() if we decide there's benefit in doing that or
	//            we have a hard requirement for a valid EK cert chain for every boot (ie, panic
	//            if there isn't one). But we can't do that as long as we need to download
	//            intermediate certs from the manufacturer.

	res := UnlockResult{IsEncrypted: true, PartDevice: sourceDevice}

	if fdeHasRevealKey() {
		sbSetKeyRevealer(&keyRevealerV3{})
	}
	model, err := opts.WhichModel()
	if err != nil {
		return res, fmt.Errorf("cannot retrieve which model to unlock for: %v", err)
	}
	sbSetModel(model)
	sbSetBootMode(opts.BootMode)

	method, err := unlockEncryptedPartitionNoKeyFile(mapperName, sourceDevice, opts.AllowRecoveryKey)
	res.UnlockMethod = method
	if err == nil {
		res.FsDevice = targetDevice
	}
	return res, err
}

func unlockEncryptedPartitionNoKeyFile(mapperName, sourceDevice string, allowRecovery bool) (UnlockMethod, error) {
	options := activateVolOpts(allowRecovery)
	options.Model = sb.SkipSnapModelCheck
	// ignoring model checker as it doesn't work with tpm "legacy" platform key data
	authRequestor, err := newAuthRequestor()
	if err != nil {
		return NotUnlocked, fmt.Errorf("cannot build an auth requestor: %v", err)
	}

	err = sbActivateVolumeWithKeyData(mapperName, sourceDevice, authRequestor, options)
	if err == sb.ErrRecoveryKeyUsed {
		logger.Noticef("successfully activated encrypted device %q using a fallback activation method", sourceDevice)
		return UnlockedWithRecoveryKey, nil
	}
	if err != nil {
		return NotUnlocked, fmt.Errorf("cannot activate encrypted device %q: %v", sourceDevice, err)
	}
	logger.Noticef("successfully activated encrypted device %q with TPM", sourceDevice)
	return UnlockedWithSealedKey, nil
}
