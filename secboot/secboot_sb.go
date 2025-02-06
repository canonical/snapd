// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nosecboot

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
	"errors"
	"fmt"
	"path/filepath"

	sb "github.com/snapcore/secboot"
	"golang.org/x/xerrors"

	"github.com/snapcore/snapd/kernel/fde"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/secboot/keys"
)

var (
	sbActivateVolumeWithKey         = sb.ActivateVolumeWithKey
	sbActivateVolumeWithKeyData     = sb.ActivateVolumeWithKeyData
	sbActivateVolumeWithRecoveryKey = sb.ActivateVolumeWithRecoveryKey
	sbDeactivateVolume              = sb.DeactivateVolume
	sbAddLUKS2ContainerUnlockKey    = sb.AddLUKS2ContainerUnlockKey
	sbRenameLUKS2ContainerKey       = sb.RenameLUKS2ContainerKey
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

	if fdeHasRevealKey() {
		return unlockVolumeUsingSealedKeyFDERevealKey(sealedEncryptionKeyFile, sourceDevice, targetDevice, mapperName, opts)
	} else {
		return unlockVolumeUsingSealedKeyTPM(name, sealedEncryptionKeyFile, sourceDevice, targetDevice, mapperName, opts)
	}
}

// UnlockEncryptedVolumeUsingKey unlocks an existing volume using the provided key.
func UnlockEncryptedVolumeUsingKey(disk disks.Disk, name string, key []byte) (UnlockResult, error) {
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

	authRequestor, err := newAuthRequestor()
	if err != nil {
		return fmt.Errorf("internal error: cannot build an auth requestor: %v", err)
	}

	if err := sbActivateVolumeWithRecoveryKey(name, device, authRequestor, &options); err != nil {
		return fmt.Errorf("cannot unlock encrypted device %q: %v", device, err)
	}

	return nil
}

// ActivateVolumeWithKey is a wrapper for secboot.ActivateVolumeWithKey
func ActivateVolumeWithKey(volumeName, sourceDevicePath string, key []byte, options *ActivateVolumeOptions) error {
	return sb.ActivateVolumeWithKey(volumeName, sourceDevicePath, key, (*sb.ActivateVolumeOptions)(options))
}

// DeactivateVolume is a wrapper for secboot.DeactivateVolume
func DeactivateVolume(volumeName string) error {
	return sb.DeactivateVolume(volumeName)
}

// AddBootstrapKeyOnExistingDisk will add a new bootstrap key to on an
// existing encrypted disk. The disk is expected to be unlocked and
// they key is available on the keyring. The bootstrap key is
// temporary and is expected to be used with a BootstrappedContainer,
// and removed by calling RemoveBootstrapKey.
func AddBootstrapKeyOnExistingDisk(node string, newKey keys.EncryptionKey) error {
	const defaultPrefix = "ubuntu-fde"
	unlockKey, err := sbGetDiskUnlockKeyFromKernel(defaultPrefix, node, false)
	if err != nil {
		return fmt.Errorf("cannot get key for unlocked disk %s: %v", node, err)
	}

	if err := sbAddLUKS2ContainerUnlockKey(node, "bootstrap-key", sb.DiskUnlockKey(unlockKey), sb.DiskUnlockKey(newKey)); err != nil {
		return fmt.Errorf("cannot enroll new installation key: %v", err)
	}

	return nil
}

// Rename key slots on LUKS2 container. If the key slot does not
// exist, it is ignored. If cryptsetup does not support renaming, then
// the key slots are instead removed.
func RenameOrDeleteKeys(node string, renames map[string]string) error {
	targets := make(map[string]bool)

	for _, renameTo := range renames {
		_, found := renames[renameTo]
		if found {
			return fmt.Errorf("internal error: keyslot name %s used as source and target of a rename", renameTo)
		}
		targets[renameTo] = true
	}

	// FIXME: listing keys, then modifying could be a TOCTOU issue.
	// we expect here nothing else is messing with the key slots.
	slots, err := sbListLUKS2ContainerUnlockKeyNames(node)
	if err != nil {
		return fmt.Errorf("cannot list slots in partition save partition: %v", err)
	}

	for _, slot := range slots {
		_, found := targets[slot]
		if found {
			return fmt.Errorf("slot name %s is already in use", slot)
		}
	}

	for _, slot := range slots {
		renameTo, found := renames[slot]
		if found {
			if err := sbRenameLUKS2ContainerKey(node, slot, renameTo); err != nil {
				if errors.Is(err, sb.ErrMissingCryptsetupFeature) {
					if err := sbDeleteLUKS2ContainerKey(node, slot); err != nil {
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

// DeleteKeys delete key slots on a LUKS2 container. Slots that do not
// exist are ignored.
func DeleteKeys(node string, matches map[string]bool) error {
	slots, err := sbListLUKS2ContainerUnlockKeyNames(node)
	if err != nil {
		return fmt.Errorf("cannot list slots in partition save partition: %v", err)
	}

	for _, slot := range slots {
		if matches[slot] {
			if err := sbDeleteLUKS2ContainerKey(node, slot); err != nil {
				return fmt.Errorf("cannot remove old container key: %v", err)
			}
		}
	}

	return nil
}
