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
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	sb "github.com/snapcore/secboot"
	sb_plainkey "github.com/snapcore/secboot/plainkey"
	"golang.org/x/xerrors"

	"github.com/snapcore/snapd/kernel/fde"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/secboot/keys"
)

func sbNewLUKS2KeyDataReaderImpl(device, slot string) (sb.KeyDataReader, error) {
	return sb.NewLUKS2KeyDataReader(device, slot)
}

var (
	sbActivateVolumeWithKey         = sb.ActivateVolumeWithKey
	sbActivateVolumeWithKeyData     = sb.ActivateVolumeWithKeyData
	sbActivateVolumeWithRecoveryKey = sb.ActivateVolumeWithRecoveryKey
	sbDeactivateVolume              = sb.DeactivateVolume
	sbAddLUKS2ContainerUnlockKey    = sb.AddLUKS2ContainerUnlockKey
	sbRenameLUKS2ContainerKey       = sb.RenameLUKS2ContainerKey
	sbNewLUKS2KeyDataReader         = sbNewLUKS2KeyDataReaderImpl
	sbSetProtectorKeys              = sb_plainkey.SetProtectorKeys
	sbGetPrimaryKeyFromKernel       = sb.GetPrimaryKeyFromKernel
	disksDevlinks                   = disks.Devlinks
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
	// TODO:FDEM: this function is big. We need to split it.

	res := UnlockResult{}

	// find the encrypted device using the disk we were provided - note that
	// we do not specify IsDecryptedDevice in opts because here we are
	// looking for the encrypted device to unlock, later on in the boot
	// process we will look for the decrypted device to ensure it matches
	// what we expected
	part, err := disk.FindMatchingPartitionWithFsLabel(EncryptedPartitionName(name))
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
		part, err = disk.FindMatchingPartitionWithFsLabel(name)
		if err != nil {
			return res, fmt.Errorf("error enumerating partitions for disk to find unencrypted device %q: %v", name, err)
		}
	}

	partDevice := filepath.Join("/dev/disk/by-partuuid", part.PartitionUUID)

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
	sourceDevice := fmt.Sprintf("/dev/disk/by-uuid/%s", part.FilesystemUUID)
	targetDevice := filepath.Join("/dev/mapper", mapperName)

	res.PartDevice = partDevice

	hintExpectFDEHook := fdeHasRevealKey()

	loadedKey := &defaultKeyLoader{}
	if err := readKeyFile(sealedEncryptionKeyFile, loadedKey, hintExpectFDEHook); err != nil {
		if !os.IsNotExist(err) {
			logger.Noticef("WARNING: there was an error loading key %s: %v", sealedEncryptionKeyFile, err)
		}
	}

	var keys []*sb.KeyData
	if loadedKey.KeyData != nil {
		keys = append(keys, loadedKey.KeyData)
	}

	if opts.WhichModel != nil {
		model, err := opts.WhichModel()
		if err != nil {
			return res, fmt.Errorf("cannot retrieve which model to unlock for: %v", err)
		}
		sbSetModel(model)
		// This does not seem to work:
		//defer sbSetModel(nil)
	}
	// TODO:FDEM:FIX: set boot mode
	//sbSetBootMode("run")
	//defer sbSetBootMode("")
	sbSetKeyRevealer(&keyRevealerV3{})
	defer sbSetKeyRevealer(nil)

	const allowPassphrase = true
	options := activateVolOpts(opts.AllowRecoveryKey, allowPassphrase, partDevice)
	authRequestor, err := newAuthRequestor()
	if err != nil {
		res.UnlockMethod = NotUnlocked
		return res, fmt.Errorf("internal error: cannot build an auth requestor: %v", err)
	}

	// Non-nil FDEHookKeyV1 indicates that V1 hook key is used
	if loadedKey.FDEHookKeyV1 != nil {
		// Special case for hook keys v1. They do not have
		// primary keys. So we cannot wrap them in KeyData
		err := unlockDiskWithHookV1Key(mapperName, sourceDevice, loadedKey.FDEHookKeyV1)
		if err == nil {
			res.FsDevice = targetDevice
			res.UnlockMethod = UnlockedWithSealedKey
			return res, nil
		}
		// If we did not manage we should still try unlocking
		// with key data if there are some on the tokens.
		// Also the request for recovery key will happen in
		// ActivateVolumeWithKeyData
		logger.Noticef("WARNING: attempting opening device %s  with key file %s failed: %v", sourceDevice, sealedEncryptionKeyFile, err)
	}

	err = sbActivateVolumeWithKeyData(mapperName, sourceDevice, authRequestor, options, keys...)
	if err == sb.ErrRecoveryKeyUsed {
		logger.Noticef("successfully activated encrypted device %q using a fallback activation method", sourceDevice)
		res.UnlockMethod = UnlockedWithRecoveryKey
	} else if err != nil {
		res.UnlockMethod = NotUnlocked
		return res, fmt.Errorf("cannot activate encrypted device %q: %v", sourceDevice, err)
	} else {
		logger.Noticef("successfully activated encrypted device %q with TPM", sourceDevice)
		res.UnlockMethod = UnlockedWithSealedKey
	}

	res.FsDevice = targetDevice
	return res, nil
}

func deviceHasPlainKey(device string) (bool, error) {
	slots, err := sbListLUKS2ContainerUnlockKeyNames(device)
	if err != nil {
		return false, fmt.Errorf("cannot list slots in partition save partition: %w", err)
	}

	for _, slot := range slots {
		reader, err := sbNewLUKS2KeyDataReader(device, slot)
		if err != nil {
			// There can be multiple errors, including
			// missing key data. So we just have to ignore
			// them.
			continue
		}
		keyData, err := sbReadKeyData(reader)
		if err != nil {
			// Error should be unexpected here. So we
			// should warn if we see any error.
			logger.Noticef("WARNING: keyslot %s has an invalid key data: %v", slot, err)
			continue
		}
		if keyData.PlatformName() == "plainkey" {
			return true, nil
		}
	}

	return false, nil
}

// UnlockEncryptedVolumeUsingProtectorKey unlocks the provided device with a
// given plain key. Depending on how then encrypted device was set up, the key
// is either used to unlock the device directly, or it is used to decrypt the
// encrypted unlock key stored in LUKS2 tokens in the device.
func UnlockEncryptedVolumeUsingProtectorKey(disk disks.Disk, name string, key []byte) (UnlockResult, error) {
	unlockRes := UnlockResult{
		UnlockMethod: NotUnlocked,
	}

	// find the encrypted device using the disk we were provided - note that
	// we do not specify IsDecryptedDevice in opts because here we are
	// looking for the encrypted device to unlock, later on in the boot
	// process we will look for the decrypted device to ensure it matches
	// what we expected
	part, err := disk.FindMatchingPartitionWithFsLabel(EncryptedPartitionName(name))
	if err != nil {
		return unlockRes, err
	}
	unlockRes.IsEncrypted = true
	// we have a device
	encdev := filepath.Join("/dev/disk/by-uuid", part.FilesystemUUID)
	unlockRes.PartDevice = encdev

	uuid, err := randutilRandomKernelUUID()
	if err != nil {
		// We failed before we could generate the filsystem device path for
		// the encrypted partition device, so we return FsDevice empty.
		return unlockRes, err
	}

	// make up a new name for the mapped device
	mapperName := name + "-" + uuid

	foundPlainKey, err := deviceHasPlainKey(encdev)
	if err != nil {
		return unlockRes, err
	}

	// in the legacy setup, the key, is the exact plain key that unlocks the
	// device, in the modern setup (indicated by presence of tokens carrying
	// named key data), the plain key is used to decrypt the actual unlock key

	if foundPlainKey {
		const allowRecovery = false
		// we should not allow passphrases as this action
		// should not expect interaction with the user
		const allowPassphrase = false
		options := activateVolOpts(allowRecovery, allowPassphrase)

		// XXX secboot maintains a global object holding protector keys, there
		// is no way to pass it through context or obtain the current set of
		// protector keys, so instead simply set it to empty set once we're done
		sbSetProtectorKeys(key)
		defer sbSetProtectorKeys()

		var authRequestor sb.AuthRequestor = nil
		if err := sbActivateVolumeWithKeyData(mapperName, encdev, authRequestor, options); err != nil {
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
// WARNING: this function is not always atomic. If cryptsetup is too
// old, it will try to copy and delete keys instead. Please avoid
// using this function in new code.
func RenameKeys(node string, renames map[string]string) error {
	targets := make(map[string]bool)

	for _, renameTo := range renames {
		_, found := renames[renameTo]
		if found {
			return fmt.Errorf("internal error: keyslot name %s used as source and target of a rename", renameTo)
		}
		targets[renameTo] = true
	}

	// TODO:FDEM:FIX: listing keys, then modifying could be a TOCTOU issue.
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
					if err := sbCopyAndRemoveLUKS2ContainerKey(node, slot, renameTo); err != nil {
						return fmt.Errorf("cannot rename old container key: %v", err)
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

func findPrimaryKey(devicePath string) ([]byte, error) {
	const remove = false
	p, err := sbGetPrimaryKeyFromKernel(keyringPrefix, devicePath, remove)
	if err == nil {
		return p, nil
	}
	if !errors.Is(err, sb.ErrKernelKeyNotFound) {
		return nil, err
	}

	// Old kernels will use "by-partuuid" symlinks. So let's
	// look at all the symlinks of the device.
	devlinks, errDevlinks := disksDevlinks(devicePath)
	if errDevlinks != nil {
		return nil, err
	}
	var errDevlink error
	for _, devlink := range devlinks {
		if !strings.HasPrefix(devlink, "/dev/disk/by-partuuid/") {
			continue
		}
		p, errDevlink = sbGetPrimaryKeyFromKernel(keyringPrefix, devlink, remove)
		if errDevlink == nil {
			return p, nil
		}
	}
	return nil, err
}

// GetPrimaryKeyDigest retrieve the primary key for a disk from the
// keyring and returns its digest. If the path given does not match
// the keyring, then it will look for symlink in /dev/disk/by-partuuid
// for that device.
func GetPrimaryKeyDigest(devicePath string, alg crypto.Hash) (salt []byte, digest []byte, err error) {
	p, err := findPrimaryKey(devicePath)
	if err != nil {
		if errors.Is(err, sb.ErrKernelKeyNotFound) {
			return nil, nil, ErrKernelKeyNotFound
		}
		return nil, nil, err
	}

	var saltArray [32]byte
	if _, err := rand.Read(saltArray[:]); err != nil {
		return nil, nil, err
	}

	h := hmac.New(alg.New, saltArray[:])
	h.Write(p)
	return saltArray[:], h.Sum(nil), nil
}

// VerifyPrimaryKeyDigest retrieve the primary key for a disk from the
// keyring and verifies its digest. If the path given does not match
// the keyring, then it will look for symlink in /dev/disk/by-partuuid
// for that device.
func VerifyPrimaryKeyDigest(devicePath string, alg crypto.Hash, salt []byte, digest []byte) (bool, error) {
	p, err := findPrimaryKey(devicePath)
	if err != nil {
		if errors.Is(err, sb.ErrKernelKeyNotFound) {
			return false, ErrKernelKeyNotFound
		}
		return false, err
	}

	h := hmac.New(alg.New, salt[:])
	h.Write(p)
	return hmac.Equal(h.Sum(nil), digest), nil
}

type HashAlg = sb.HashAlg

func (key *SealKeyRequest) getWriter() (sb.KeyDataWriter, error) {
	if key.KeyFile != "" {
		return sb.NewFileKeyDataWriter(key.KeyFile), nil
	} else {
		return key.BootstrappedContainer.GetTokenWriter(key.SlotName)
	}
}

// TemporaryNameOldKeys takes a disk using legacy keyslots 0, 1, 2 and
// adds names to those keyslots. This is needed to convert the save
// disk during a factory reset. This is a no-operation if all keyslots
// are already named.
func TemporaryNameOldKeys(devicePath string) error {
	if err := sb.NameLegacyLUKS2ContainerKey(devicePath, 0, "old-default-key"); err != nil && !errors.Is(err, sb.KeyslotAlreadyHasANameErr) {
		return err
	}
	if err := sb.NameLegacyLUKS2ContainerKey(devicePath, 1, "old-recovery-key"); err != nil && !errors.Is(err, sb.KeyslotAlreadyHasANameErr) {
		return err
	}
	if err := sb.NameLegacyLUKS2ContainerKey(devicePath, 2, "old-temporary-key"); err != nil && !errors.Is(err, sb.KeyslotAlreadyHasANameErr) {
		return err
	}
	return nil
}

// DeleteOldKeys removes key slots from an old installation that
// had names created by TemporaryNameOldKeys.
func DeleteOldKeys(devicePath string) error {
	toDelete := map[string]bool{
		"old-default-key":   true,
		"old-recovery-key":  true,
		"old-temporary-key": true,
	}
	return DeleteKeys(devicePath, toDelete)
}

func sbCopyAndRemoveLUKS2ContainerKeyImpl(devicePath, keyslotName, renameTo string) error {
	return sb.CopyAndRemoveLUKS2ContainerKey(sb.AllowNonAtomicOperation(), devicePath, keyslotName, renameTo)
}

var sbCopyAndRemoveLUKS2ContainerKey = sbCopyAndRemoveLUKS2ContainerKeyImpl
