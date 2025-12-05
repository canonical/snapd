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
	"context"
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	sb "github.com/snapcore/secboot"
	sb_luks2 "github.com/snapcore/secboot/luks2"
	sb_plainkey "github.com/snapcore/secboot/plainkey"
	"golang.org/x/xerrors"

	"github.com/snapcore/snapd/gadget/device"
	"github.com/snapcore/snapd/kernel/fde"
	"github.com/snapcore/snapd/kernel/fde/optee"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/secboot/keys"
)

func sbNewLUKS2KeyDataReaderImpl(device, slot string) (sb.KeyDataReader, error) {
	return sb.NewLUKS2KeyDataReader(device, slot)
}

var (
	sbActivateVolumeWithKey      = sb.ActivateVolumeWithKey
	sbFindStorageContainer       = sb.FindStorageContainer
	sbDeactivateVolume           = sb.DeactivateVolume
	sbAddLUKS2ContainerUnlockKey = sb.AddLUKS2ContainerUnlockKey
	sbRenameLUKS2ContainerKey    = sb.RenameLUKS2ContainerKey
	sbNewLUKS2KeyDataReader      = sbNewLUKS2KeyDataReaderImpl
	sbSetProtectorKeys           = sb_plainkey.SetProtectorKeys
	sbGetPrimaryKeyFromKernel    = sb.GetPrimaryKeyFromKernel
	sbTestLUKS2ContainerKey      = sb.TestLUKS2ContainerKey
	sbCheckPassphraseEntropy     = sb.CheckPassphraseEntropy
	disksDevlinks                = disks.Devlinks
	sbNewActivateContext         = sb.NewActivateContext

	sbKeyDataChangePassphrase = (*sb.KeyData).ChangePassphrase
	sbKeyDataPlatformName     = (*sb.KeyData).PlatformName

	sbWithVolumeName                       = sb_luks2.WithVolumeName
	sbWithExternalKeyData                  = sb.WithExternalKeyData
	sbWithLegacyKeyringKeyDescriptionPaths = sb.WithLegacyKeyringKeyDescriptionPaths
	sbWithRecoveryKeyTries                 = sb.WithRecoveryKeyTries
	sbWithAuthRequestor                    = sb.WithAuthRequestor
	sbWithPassphraseTries                  = sb.WithPassphraseTries
	sbWithPINTries                         = sb.WithPINTries
)

func init() {
	WithSecbootSupport = true

	device.EntropyBits = EntropyBits
}

type DiskUnlockKey sb.DiskUnlockKey
type ActivateVolumeOptions sb.ActivateVolumeOptions

const platformTpm2 = "tpm2"
const platformTpm2Legacy = "tpm2-legacy"
const platformPlainkey = "plainkey"
const platformFdeHookV2 = "fde-hook-v2"
const platformFdeHooksV3 = "fde-hooks-v3"

// LockSealedKeys manually locks access to the sealed keys. Meant to be
// called in place of passing lockKeysOnFinish as true to
// UnlockVolumeUsingSealedKeyIfEncrypted for cases where we don't know if a
// given call is the last one to unlock a volume like in degraded recover mode.
func LockSealedKeys() error {
	if fdeHasRevealKey() {
		return fde.LockSealedKeys()
	}

	client := optee.NewFDETAClient()
	if client.Present() {
		return client.Lock()
	}

	return lockTPMSealedKeys()
}

type ActivateContext interface {
	ActivateContainer(ctx context.Context, container sb.StorageContainer, opts ...sb.ActivateOption) error
	State() *sb.ActivateState
}

type activateContextImpl struct {
	*sb.ActivateContext
}

func (a *activateContextImpl) ActivateContainer(ctx context.Context, container sb.StorageContainer, opts ...sb.ActivateOption) error {
	return a.ActivateContext.ActivateContainer(ctx, container, opts...)
}

func NewActivateContext(ctx context.Context) (ActivateContext, error) {
	context, err := sbNewActivateContext(ctx, nil, sbWithAuthRequestor(NewSystemdAuthRequestor()), sbWithPassphraseTries(3), sbWithPINTries(3))
	if err != nil {
		return nil, err
	}
	return &activateContextImpl{ActivateContext: context}, nil
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
func UnlockVolumeUsingSealedKeyIfEncrypted(activation ActivateContext, disk disks.Disk, name string, sealedEncryptionKeyFile string, opts *UnlockVolumeUsingSealedKeyOptions) (UnlockResult, error) {
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

	expectFDEHook := fdeHasRevealKey()
	loadedKey := &defaultKeyLoader{}
	if err := readKeyFile(sealedEncryptionKeyFile, loadedKey, expectFDEHook); err != nil {
		if !os.IsNotExist(err) {
			logger.Noticef("WARNING: there was an error loading key %s: %v", sealedEncryptionKeyFile, err)
		}
	}

	var options []sb.ActivateOption
	if loadedKey.KeyData != nil {
		options = append(options, sbWithExternalKeyData(loadedKey.KeyData.ReadableName(), loadedKey.KeyData))
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

	sbSetBootMode(opts.BootMode)
	defer sbSetBootMode("")

	sbSetKeyRevealer(&keyRevealerV3{})
	defer sbSetKeyRevealer(nil)

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

	container, err := sbFindStorageContainer(context.Background(), sourceDevice)
	if err != nil {
		return res, err
	}

	options = append(options, sbWithVolumeName(mapperName), sbWithLegacyKeyringKeyDescriptionPaths(partDevice, sourceDevice))
	if opts.AllowRecoveryKey {
		options = append(options, sbWithRecoveryKeyTries(3))
	}
	err = activation.ActivateContainer(context.Background(), container, options...)
	if err != nil {
		res.UnlockMethod = NotUnlocked
		return res, fmt.Errorf("cannot activate encrypted device %q: %v", sourceDevice, err)
	} else {
		state := activation.State()
		activationState := state.Activations[container.CredentialName()]
		if activationState.Status == sb.ActivationSucceededWithRecoveryKey {
			logger.Noticef("successfully activated encrypted device %q using a fallback activation method", sourceDevice)
			res.UnlockMethod = UnlockedWithRecoveryKey
		} else {
			logger.Noticef("successfully activated encrypted device %q with TPM", sourceDevice)
			res.UnlockMethod = UnlockedWithSealedKey
		}
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
		if keyData.PlatformName() == platformPlainkey {
			return true, nil
		}
	}

	return false, nil
}

// UnlockEncryptedVolumeUsingProtectorKey unlocks the provided device with a
// given plain key. Depending on how then encrypted device was set up, the key
// is either used to unlock the device directly, or it is used to decrypt the
// encrypted unlock key stored in LUKS2 tokens in the device.
func UnlockEncryptedVolumeUsingProtectorKey(activation ActivateContext, disk disks.Disk, name string, key []byte) (UnlockResult, error) {
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
		// XXX secboot maintains a global object holding protector keys, there
		// is no way to pass it through context or obtain the current set of
		// protector keys, so instead simply set it to empty set once we're done
		sbSetProtectorKeys(key)
		defer sbSetProtectorKeys()

		container, err := sbFindStorageContainer(context.Background(), encdev)
		if err != nil {
			return unlockRes, err
		}
		if err := activation.ActivateContainer(context.Background(), container, sbWithVolumeName(mapperName), sbWithLegacyKeyringKeyDescriptionPaths(encdev)); err != nil {
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
	unlockKey, err := sbGetDiskUnlockKeyFromKernel(defaultKeyringPrefix, node, false)
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
func RenameKeysForFactoryReset(node string, renames map[string]string) error {
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
	// XXX: include recovery key slots sbListLUKS2ContainerRecoveryKeyNames
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

// RenameContainerKey renames a key slot on LUKS2 container. An error
// is returned if cryptsetup does not support --token-replace option.
func RenameContainerKey(devicePath, oldName, newName string) error {
	return sbRenameLUKS2ContainerKey(devicePath, oldName, newName)
}

// DeleteKeys delete key slots on a LUKS2 container. Slots that do not
// exist are ignored.
//
// XXX: s/DeleteKey/DeleteContainerKey
func DeleteKeys(node string, matches map[string]bool) error {
	// XXX: include recovery key slots sbListLUKS2ContainerRecoveryKeyNames
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

// DeleteContainerKey deletes a key slot on a LUKS2 container.
func DeleteContainerKey(devicePath, slotName string) error {
	return sbDeleteLUKS2ContainerKey(devicePath, slotName)
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

// GetPrimaryKey finds the primary from the keyring based on the path of
// encrypted devices. If it does not find any primary in the keyring,
// it then tries to read the key from a fallback key file.
func GetPrimaryKey(devices []string, fallbackKeyFiles []string) ([]byte, error) {
	for _, device := range devices {
		primaryKey, err := findPrimaryKey(device)
		if err == nil {
			return primaryKey, nil
		}
		if !errors.Is(err, sb.ErrKernelKeyNotFound) {
			return nil, err
		}
	}

	var fallbackErrors []string

	for _, fallbackKeyFile := range fallbackKeyFiles {
		primaryKey, err := os.ReadFile(fallbackKeyFile)
		if err == nil {
			return primaryKey, nil
		}
		fallbackErrors = append(fallbackErrors, fmt.Sprintf("cannot read %s: %v", fallbackKeyFile, err))
	}

	return nil, fmt.Errorf("could not find primary in keyring and cannot read fallback primary key files: %s", strings.Join(fallbackErrors, ", "))
}

// CheckRecoveryKey tests that the specified recovery key unlocks the
// device at the specified path.
func CheckRecoveryKey(devicePath string, rkey keys.RecoveryKey) error {
	if !sbTestLUKS2ContainerKey(devicePath, rkey[:]) {
		return fmt.Errorf("invalid recovery key for %s", devicePath)
	}
	return nil
}

// ListContainerRecoveryKeyNames lists the names of key slots on the specified
// device configured as recovery slots.
//
// Note: This only supports LUKS2 containers.
func ListContainerRecoveryKeyNames(devicePath string) ([]string, error) {
	return sbListLUKS2ContainerRecoveryKeyNames(devicePath)
}

// ListContainerUnlockKeyNames lists the names of key slots on the specified
// device configured as normal unlock slots (the keys associated with these
// should be protected by the platform's secure device).
//
// Note: This only supports LUKS2 containers.
func ListContainerUnlockKeyNames(devicePath string) ([]string, error) {
	return sbListLUKS2ContainerUnlockKeyNames(devicePath)
}

type keyData struct {
	kd *sb.KeyData
}

// AuthMode indicates the authentication mechanisms enabled for this key data.
func (k *keyData) AuthMode() device.AuthMode {
	switch k.kd.AuthMode() {
	case sb.AuthModeNone:
		return device.AuthModeNone
	case sb.AuthModePassphrase:
		return device.AuthModePassphrase
	// TODO:FDEM: add AuthModePIN when it lands in secboot
	default:
		return ""
	}
}

// PlatformName returns the name of the platform that handles this key data.
func (k *keyData) PlatformName() string {
	return k.kd.PlatformName()
}

// Role indicates the role of this key.
func (k *keyData) Roles() []string {
	if k.kd.Role() == "" {
		return nil
	}
	return []string{k.kd.Role()}
}

func (k *keyData) ChangePassphrase(oldPassphrase, newPassphrase string) error {
	return sbKeyDataChangePassphrase(k.kd, oldPassphrase, newPassphrase)
}

func (k *keyData) WriteTokenAtomic(devicePath, slotName string) error {
	writer, err := newLUKS2KeyDataWriter(devicePath, slotName)
	if err != nil {
		return err
	}
	return k.kd.WriteAtomic(writer)
}

// ReadContainerKeyData reads key slot key data for the specified device and slot name.
//
// Note: This only supports key datas stored in LUKS2 tokens.
func ReadContainerKeyData(devicePath, slotName string) (KeyData, error) {
	kd, err := readKeyToken(devicePath, slotName)
	if err != nil {
		return nil, err
	}

	return &keyData{kd: kd}, nil
}

// EntropyBits calculates entropy for PINs and passphrases.
//
// PINs will be supplied as a numeric passphrase.
func EntropyBits(passphrase string) (uint32, error) {
	stats, err := sbCheckPassphraseEntropy(passphrase)
	if err != nil {
		return 0, err
	}
	return stats.EntropyBits, nil
}

// AddContainerRecoveryKey adds a new recovery key to specified device.
//
// Note: The unlock key is implicitly obtained from the kernel keyring.
func AddContainerRecoveryKey(devicePath string, slotName string, rkey keys.RecoveryKey) error {
	unlockKey, err := sbGetDiskUnlockKeyFromKernel(defaultKeyringPrefix, devicePath, false)
	if err != nil {
		return fmt.Errorf("cannot get key from kernel keyring for unlocked disk %s: %v", devicePath, err)
	}
	return sbAddLUKS2ContainerRecoveryKey(devicePath, slotName, unlockKey, sb.RecoveryKey(rkey))
}

type resealKind int

const (
	tpmResealKind resealKind = iota
	hookResealKind
	noResealKind
)

// ResealKey reads a key and dispatch the reseal depending on the
// platform used by the key.
func ResealKey(key KeyDataLocation, params *ResealKeyParams) (UpdatedKeys, error) {
	loadedKey := &defaultKeyLoader{}
	keyData, err := readKeyToken(key.DevicePath, key.SlotName)
	if err != nil {
		if err := readKeyFile(key.KeyFile, loadedKey, params.HintExpectFDEHook); err != nil {
			return nil, err
		}
		keyData = loadedKey.KeyData

	}

	resealKind := tpmResealKind
	switch {
	case loadedKey.SealedKeyObject != nil:
	case loadedKey.FDEHookKeyV1 != nil:
		resealKind = noResealKind
	case keyData != nil:
		switch sbKeyDataPlatformName(keyData) {
		case platformTpm2:
		case platformTpm2Legacy:
			// This one should not happen but instead have a SealedKeyObject
		case platformPlainkey:
			resealKind = noResealKind
		case platformFdeHooksV3:
			resealKind = hookResealKind
		case platformFdeHookV2:
			resealKind = hookResealKind
		default:
			logger.Noticef("unknown platform %s, assuming it is TPM", sbKeyDataPlatformName(keyData))
		}
	default:
		return nil, fmt.Errorf("internal error: missing key data from key loader")
	}

	switch resealKind {
	case tpmResealKind:
		primaryKey, err := GetPrimaryKey(params.PrimaryKeyDevices, params.FallbackPrimaryKeyFiles)
		if err != nil {
			return nil, err
		}
		if params.VerifyPrimaryKey != nil {
			params.VerifyPrimaryKey(primaryKey)
		}
		pcrProfile, err := params.GetTpmPCRProfile()
		if err != nil {
			return nil, err
		}

		keyParams := &resealKeysWithTPMParams{
			PCRProfile: pcrProfile,
			Keys:       []KeyDataLocation{key},
			PrimaryKey: primaryKey,
		}

		return resealKeysWithTPM(keyParams, params.NewPCRPolicyVersion)

	case hookResealKind:
		err = resealKeysWithFDESetupHook([]KeyDataLocation{key}, params.PrimaryKeyDevices, params.FallbackPrimaryKeyFiles, params.VerifyPrimaryKey, params.Models, params.BootModes)
		return nil, err

	case noResealKind:
		return nil, nil
	default:
		return nil, fmt.Errorf("internal error: unknown reseal kind")
	}
}
