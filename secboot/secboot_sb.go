// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !nosecboot

/*
 * Copyright (C) 2021 Canonical Ltd
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
	"fmt"
	"path/filepath"

	sb "github.com/snapcore/secboot"
	"golang.org/x/xerrors"

	"github.com/snapcore/snapd/kernel/fde"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil/disks"
)

var (
	sbActivateVolumeWithKey         = sb.ActivateVolumeWithKey
	sbActivateVolumeWithKeyData     = sb.ActivateVolumeWithKeyData
	sbActivateVolumeWithRecoveryKey = sb.ActivateVolumeWithRecoveryKey
	sbDeactivateVolume              = sb.DeactivateVolume
)

func init() {
	WithSecbootSupport = true
}

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

	// systems that need the "fde-device-unlock" are special
	if fde.HasDeviceUnlock() {
		return unlockVolumeUsingSealedKeyViaDeviceUnlockHook(disk, name, sealedEncryptionKeyFile, opts)
	}

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
		// unlock it
		// the filesystem device for the unencrypted case is the same as the
		// partition device
		res.PartDevice = partDevice
		res.FsDevice = res.PartDevice
		return res, nil
	}

	mapperName := name + "-" + randutilRandomKernelUUID()
	sourceDevice := partDevice
	targetDevice := filepath.Join("/dev/mapper", mapperName)

	if fdeHasRevealKey() {
		return unlockVolumeUsingSealedKeyFDERevealKey(sealedEncryptionKeyFile, sourceDevice, targetDevice, mapperName, opts)
	} else {
		return unlockVolumeUsingSealedKeyTPM(name, sealedEncryptionKeyFile, sourceDevice, targetDevice, mapperName, opts)
	}
}

// UnlockEncryptedVolumeUsingKey unlocks an existing volume using the provided key.
func UnlockEncryptedVolumeUsingKey(disk disks.Disk, name string, key []byte) (UnlockResult, error) {
	// systems that need the "fde-device-unlock" are special
	if fde.HasDeviceUnlock() {
		return unlockEncryptedVolumeUsingKeyViaDeviceUnlockHook(disk, name, key)
	}

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
	// make up a new name for the mapped device
	mapperName := name + "-" + randutilRandomKernelUUID()
	if err := unlockEncryptedPartitionWithKey(mapperName, encdev, key); err != nil {
		return unlockRes, err
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

	if err := sbActivateVolumeWithRecoveryKey(name, device, nil, &options); err != nil {
		return fmt.Errorf("cannot unlock encrypted device %q: %v", device, err)
	}

	return nil
}

func setupDeviceMapperTargetForDeviceUnlock(disk disks.Disk, name string) (string, string, error) {
	// 1. mount name under the right mapper with 1mb offset

	// XXX: we're assuming that partition name is well known at this point
	// and the assumption has been verified at install time
	part, err := disk.FindMatchingPartitionWithPartLabel(name)
	if err != nil {
		return "", "", err
	}
	// TODO: use part.KernelDeviceNode
	partDevice := filepath.Join("/dev/disk/by-partuuid", part.PartitionUUID)
	partSize, err := disks.Size(partDevice)
	if err != nil {
		return "", "", err
	}
	// XXX: This is the location of both locked and unlocked
	//      paths because inline-encryption is transparent. So
	//      What is the best name?
	mapperName := fde.EncryptedDeviceMapperName(name)

	offset := fde.DeviceSetupHookPartitionOffset
	mapperSize := partSize - offset
	mapperDevice, err := disks.CreateLinearMapperDevice(partDevice, mapperName, part.PartitionUUID, offset, mapperSize)
	if err != nil {
		return "", "", err
	}

	return partDevice, mapperDevice, err
}

func unlockVolumeUsingSealedKeyViaDeviceUnlockHook(disk disks.Disk, name string, sealedEncryptionKeyFile string, opts *UnlockVolumeUsingSealedKeyOptions) (UnlockResult, error) {
	res := UnlockResult{}

	// 1. setup mapper
	partDevice, mapperDevice, err := setupDeviceMapperTargetForDeviceUnlock(disk, name)
	if err != nil {
		return res, err
	}

	// 2. unseal the key using fde-reveal-key (implicit via
	//    the registered platform handler for fde-reveal-key)
	f, err := sb.NewFileKeyDataReader(sealedEncryptionKeyFile)
	if err != nil {
		return res, err
	}
	keyData, err := sb.ReadKeyData(f)
	if err != nil {
		fmt := "cannot read key data: %w"
		return res, xerrors.Errorf(fmt, err)
	}
	unlockKey, _, err := keyData.RecoverKeys()
	if err != nil {
		fmt := "cannot recover key data: %w"
		return res, xerrors.Errorf(fmt, err)
	}

	// 3. call fde-device-unlock to unlock device
	params := &fde.DeviceUnlockParams{
		Key:           unlockKey,
		Device:        partDevice,
		PartitionName: name,
	}
	if err := fde.DeviceUnlock(params); err != nil {
		return res, err
	}
	res.UnlockMethod = UnlockedWithKey
	res.IsEncrypted = true
	res.PartDevice = partDevice
	res.FsDevice = mapperDevice
	return res, nil
}

func unlockEncryptedVolumeUsingKeyViaDeviceUnlockHook(disk disks.Disk, name string, key []byte) (UnlockResult, error) {
	res := UnlockResult{}

	// 1. setup mapper
	partDevice, mapperDevice, err := setupDeviceMapperTargetForDeviceUnlock(disk, name)
	if err != nil {
		return res, err
	}

	// 2. call fde-device-unlock to unlock device
	params := &fde.DeviceUnlockParams{
		Key:    key,
		Device: partDevice,
		// the device corresponds to a partition with this name and
		// carries similarly named filesystem inside, this relation is
		// checked at install time
		PartitionName: name,
	}
	if err := fde.DeviceUnlock(params); err != nil {
		return res, err
	}

	return UnlockResult{
		IsEncrypted:  true,
		PartDevice:   partDevice,
		FsDevice:     mapperDevice,
		UnlockMethod: UnlockedWithKey,
	}, nil
}
