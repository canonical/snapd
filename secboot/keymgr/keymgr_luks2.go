// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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

package keymgr

import (
	"fmt"
	"regexp"
	"time"

	sb "github.com/snapcore/secboot"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/secboot/keyring"
	"github.com/snapcore/snapd/secboot/keys"
	"github.com/snapcore/snapd/secboot/luks2"
)

const (
	// key slot used by the encryption key
	encryptionKeySlot = 0
	// key slot used by the recovery key
	recoveryKeySlot = 1
	// temporary key slot used when changing the encryption key
	tempKeySlot = recoveryKeySlot + 1
)

var (
	sbGetDiskUnlockKeyFromKernel = sb.GetDiskUnlockKeyFromKernel
	keyringAddKeyToUserKeyring   = keyring.AddKeyToUserKeyring
)

func getEncryptionKeyFromUserKeyring(dev string) ([]byte, error) {
	const remove = false
	const defaultPrefix = "ubuntu-fde"
	// note this is the unlock key, which can be either the main key which
	// was unsealed, or the recovery key, in which case some operations may
	// not make sense
	currKey, err := sbGetDiskUnlockKeyFromKernel(defaultPrefix, dev, remove)
	if err != nil {
		return nil, fmt.Errorf("cannot obtain current unlock key for %v: %v", dev, err)
	}
	return currKey, err
}

var keyslotFull = regexp.MustCompile(`^.* cryptsetup failed with: Key slot [0-9]+ is full, please select another one\.$`)

// IsKeyslotAlreadyUsed returns true if the error indicates that the keyslot
// attempted for a given key is already used
func IsKeyslotAlreadyUsed(err error) bool {
	if err == nil {
		return false
	}
	return keyslotFull.MatchString(err.Error())
}

func isKeyslotNotActive(err error) bool {
	match, _ := regexp.MatchString(`.*: Keyslot [0-9]+ is not active`, err.Error())
	return match
}

func recoveryKDF() (*luks2.KDFOptions, error) {
	usableMem, err := osutil.TotalUsableMemory()
	if err != nil {
		return nil, fmt.Errorf("cannot get usable memory for KDF parameters when adding the recovery key: %v", err)
	}
	// The KDF memory is heuristically calculated by taking the
	// usable memory and subtracting hardcoded 384MB that is
	// needed to keep the system working. Half of that is the mem
	// we want to use for the KDF. Doing it this way avoids the expensive
	// benchmark from cryptsetup. The recovery key is already 128bit
	// strong so we don't need to be super precise here.
	kdfMem := (int(usableMem) - 384*1024*1024) / 2
	// at most 1 GB, but at least 32 kB
	if kdfMem > 1024*1024*1024 {
		kdfMem = (1024 * 1024 * 1024)
	} else if kdfMem < 32*1024 {
		kdfMem = 32 * 1024
	}
	return &luks2.KDFOptions{
		MemoryKiB:       kdfMem / 1024,
		ForceIterations: 4,
	}, nil
}

// AddRecoveryKeyToLUKSDevice adds a recovery key to a LUKS2 device. It the
// devuce unlock key from the user keyring to authorize the change. The
// recoveyry key is added to keyslot 1.
func AddRecoveryKeyToLUKSDevice(recoveryKey keys.RecoveryKey, dev string) error {
	currKey, err := getEncryptionKeyFromUserKeyring(dev)
	if err != nil {
		return err
	}

	return AddRecoveryKeyToLUKSDeviceUsingKey(recoveryKey, currKey, dev)
}

// AddRecoveryKeyToLUKSDeviceUsingKey adds a recovery key rkey to the existing
// LUKS encrypted volume on the block device given by node. The existing key to
// the encrypted volume is provided in the key argument and used to authorize
// the operation.
//
// A heuristic memory cost is used.
func AddRecoveryKeyToLUKSDeviceUsingKey(recoveryKey keys.RecoveryKey, currKey keys.EncryptionKey, dev string) error {
	opts, err := recoveryKDF()
	if err != nil {
		return err
	}

	options := luks2.AddKeyOptions{
		KDFOptions: *opts,
		Slot:       recoveryKeySlot,
	}
	if err := luks2.AddKey(dev, currKey, recoveryKey[:], &options); err != nil {
		return fmt.Errorf("cannot add key: %v", err)
	}

	if err := luks2.SetSlotPriority(dev, encryptionKeySlot, luks2.SlotPriorityHigh); err != nil {
		return fmt.Errorf("cannot change keyslot priority: %v", err)
	}

	return nil
}

// RemoveRecoveryKeyFromLUKSDevice removes an existing recovery key a LUKS2
// device.
func RemoveRecoveryKeyFromLUKSDevice(dev string) error {
	currKey, err := getEncryptionKeyFromUserKeyring(dev)
	if err != nil {
		return err
	}
	return RemoveRecoveryKeyFromLUKSDeviceUsingKey(currKey, dev)
}

// RemoveRecoveryKeyFromLUKSDeviceUsingKey removes an existing recovery key a
// LUKS2 using the provided key to authorize the operation.
func RemoveRecoveryKeyFromLUKSDeviceUsingKey(currKey keys.EncryptionKey, dev string) error {
	// just remove the key we think is a recovery key (luks keyslot 1)
	if err := luks2.KillSlot(dev, recoveryKeySlot, currKey); err != nil {
		if !isKeyslotNotActive(err) {
			return fmt.Errorf("cannot kill recovery key slot: %v", err)
		}
	}
	return nil
}

// ChangeLUKSDeviceEncryptionKey changes the main encryption key of the device.
// Uses an existing unlock key of that device, which is present in the kernel
// user keyring. Once complete the user keyring contains the new encryption key.
func ChangeLUKSDeviceEncryptionKey(newKey keys.EncryptionKey, dev string) error {
	if len(newKey) != keys.EncryptionKeySize {
		return fmt.Errorf("cannot use a key of size different than %v", keys.EncryptionKeySize)
	}

	currKey, err := getEncryptionKeyFromUserKeyring(dev)
	if err != nil {
		return err
	}

	// we only have the current key, we cannot add a key to an occupied
	// keyslot, and cannot start with killing its keyslot as that would make
	// the device unusable, so instead add the new key to an auxiliary
	// keyslot, then use the new key to authorize removal of keyslot 0
	// (which refers to the old key), add the new key again, but this time
	// to keyslot 0, lastly kill the aux keyslot

	if err := luks2.KillSlot(dev, tempKeySlot, currKey); err != nil {
		if !isKeyslotNotActive(err) {
			return fmt.Errorf("cannot kill the temporary keyslot: %v", err)
		}
	}

	options := luks2.AddKeyOptions{
		KDFOptions: luks2.KDFOptions{TargetDuration: 100 * time.Millisecond},
		Slot:       tempKeySlot,
	}
	if err := luks2.AddKey(dev, currKey[:], newKey, &options); err != nil {
		return fmt.Errorf("cannot add temporary key: %v", err)
	}

	// now it should be possible to kill the original keyslot by using the
	// new key for authorization
	if err := luks2.KillSlot(dev, encryptionKeySlot, newKey); err != nil {
		if !isKeyslotNotActive(err) {
			return fmt.Errorf("cannot kill existing slot: %v", err)
		}
	}
	options.Slot = encryptionKeySlot
	// add the new key to keyslot 0
	if err := luks2.AddKey(dev, newKey, newKey, &options); err != nil {
		return fmt.Errorf("cannot add key: %v", err)
	}
	// and kill the aux slot
	if err := luks2.KillSlot(dev, tempKeySlot, newKey); err != nil {
		return fmt.Errorf("cannot kill temporary key slot: %v", err)
	}
	// TODO needed?
	if err := luks2.SetSlotPriority(dev, encryptionKeySlot, luks2.SlotPriorityHigh); err != nil {
		return fmt.Errorf("cannot change keyslot priority: %v", err)
	}

	const keyringPurposeDiskUnlock = "unlock"
	const keyringPrefix = "ubuntu-fde"
	// TODO: make the key permanent in the keyring, investigate why timeout
	// is set to a very large number of weeks, but not tagged as perm in
	// /proc/keys
	if err := keyringAddKeyToUserKeyring(newKey, dev, keyringPurposeDiskUnlock, keyringPrefix); err != nil {
		return fmt.Errorf("cannot add key to user keyring: %v", err)
	}
	return nil
}
