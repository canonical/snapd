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

	"github.com/ddkwork/golibrary/mylog"
	sb "github.com/snapcore/secboot"

	"github.com/snapcore/snapd/osutil"
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

var sbGetDiskUnlockKeyFromKernel = sb.GetDiskUnlockKeyFromKernel

func getEncryptionKeyFromUserKeyring(dev string) ([]byte, error) {
	const remove = false
	const defaultPrefix = "ubuntu-fde"
	// note this is the unlock key, which can be either the main key which
	// was unsealed, or the recovery key, in which case some operations may
	// not make sense
	currKey := mylog.Check2(sbGetDiskUnlockKeyFromKernel(defaultPrefix, dev, remove))

	return currKey, err
}

// TODO rather than inspecting the error messages, parse the LUKS2 headers

var keyslotFull = regexp.MustCompile(`^.*cryptsetup failed with: Key slot [0-9]+ is full, please select another one\.$`)

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
	usableMem := mylog.Check2(osutil.TotalUsableMemory())

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
	currKey := mylog.Check2(getEncryptionKeyFromUserKeyring(dev))

	return AddRecoveryKeyToLUKSDeviceUsingKey(recoveryKey, currKey, dev)
}

// AddRecoveryKeyToLUKSDeviceUsingKey adds a recovery key rkey to the existing
// LUKS encrypted volume on the block device given by node. The existing key to
// the encrypted volume is provided in the key argument and used to authorize
// the operation.
//
// A heuristic memory cost is used.
func AddRecoveryKeyToLUKSDeviceUsingKey(recoveryKey keys.RecoveryKey, currKey keys.EncryptionKey, dev string) error {
	opts := mylog.Check2(recoveryKDF())

	options := luks2.AddKeyOptions{
		KDFOptions: *opts,
		Slot:       recoveryKeySlot,
	}
	mylog.Check(luks2.AddKey(dev, currKey, recoveryKey[:], &options))
	mylog.Check(luks2.SetSlotPriority(dev, encryptionKeySlot, luks2.SlotPriorityHigh))

	return nil
}

// RemoveRecoveryKeyFromLUKSDevice removes an existing recovery key a LUKS2
// device.
func RemoveRecoveryKeyFromLUKSDevice(dev string) error {
	currKey := mylog.Check2(getEncryptionKeyFromUserKeyring(dev))

	return RemoveRecoveryKeyFromLUKSDeviceUsingKey(currKey, dev)
}

// RemoveRecoveryKeyFromLUKSDeviceUsingKey removes an existing recovery key a
// LUKS2 using the provided key to authorize the operation.
func RemoveRecoveryKeyFromLUKSDeviceUsingKey(currKey keys.EncryptionKey, dev string) error {
	mylog.Check(
		// just remove the key we think is a recovery key (luks keyslot 1)
		luks2.KillSlot(dev, recoveryKeySlot, currKey))

	return nil
}

// StageLUKSDeviceEncryptionKeyChange stages a new encryption key with the goal
// of changing the main encryption key referenced in keyslot 0. The operation is
// authorized using the key that unlocked the device and is stored in the
// keyring (as it happens during factory reset).
func StageLUKSDeviceEncryptionKeyChange(newKey keys.EncryptionKey, dev string) error {
	if len(newKey) != keys.EncryptionKeySize {
		return fmt.Errorf("cannot use a key of size different than %v", keys.EncryptionKeySize)
	}

	// the key to authorize the device is in the keyring
	currKey := mylog.Check2(getEncryptionKeyFromUserKeyring(dev))
	mylog.Check(

		// TODO rather than inspecting the errors, parse the LUKS2 headers

		// free up the temp slot
		luks2.KillSlot(dev, tempKeySlot, currKey))

	options := luks2.AddKeyOptions{
		KDFOptions: luks2.KDFOptions{TargetDuration: 100 * time.Millisecond},
		Slot:       tempKeySlot,
	}
	mylog.Check(luks2.AddKey(dev, currKey[:], newKey, &options))

	return nil
}

// TransitionLUKSDeviceEncryptionKeyChange completes the main encryption key
// change to the new key provided in the parameters. The new key must have been
// staged before, thus it can authorize LUKS operations. Lastly, the unlock key
// in the keyring is updated to the new key.
func TransitionLUKSDeviceEncryptionKeyChange(newKey keys.EncryptionKey, dev string) error {
	if len(newKey) != keys.EncryptionKeySize {
		return fmt.Errorf("cannot use a key of size different than %v", keys.EncryptionKeySize)
	}

	// the expected state is as follows:
	// key slot 0 - the old encryption key
	// key slot 2 - the new encryption key (added during --stage)
	// the desired state is:
	// key slot 0 - the new encryption key
	// key slot 2 - empty
	// it is possible that the system was rebooted right after key slot 0 was
	// populated with the new key and key slot 2 was emptied

	// there is no state information on disk which would tell if the
	// scenario 1 above occurred and to which stage it was executed, but we
	// need to find out if key slot 2 is in use (as the caller believes that
	// a key was staged earlier); do this indirectly by trying to add a key
	// to key slot 2

	// TODO rather than inspecting the errors, parse the LUKS2 headers

	tempKeyslotAlreadyUsed := true

	options := luks2.AddKeyOptions{
		KDFOptions: luks2.KDFOptions{TargetDuration: 100 * time.Millisecond},
		Slot:       tempKeySlot,
	}
	mylog.Check(luks2.AddKey(dev, newKey, newKey, &options))
	if err == nil {
		// key slot is not in use, so we are dealing with unexpected reboot scenario
		tempKeyslotAlreadyUsed = false
	} else if err != nil && !IsKeyslotAlreadyUsed(err) {
		return fmt.Errorf("cannot add new encryption key: %v", err)
	}

	if !tempKeyslotAlreadyUsed {
		mylog.Check(
			// since the key slot was not used, it means that the transition
			// was already carried out (since it got authorized by the new
			// key), so now all is needed is to remove the added key
			luks2.KillSlot(dev, tempKeySlot, newKey))

		return nil
	}
	mylog.Check(

		// first kill the main encryption key slot, authorize the operation
		// using the new key which must have been added to the temp keyslot in
		// the stage operation
		luks2.KillSlot(dev, encryptionKeySlot, newKey))

	options = luks2.AddKeyOptions{
		KDFOptions: luks2.KDFOptions{TargetDuration: 100 * time.Millisecond},
		Slot:       encryptionKeySlot,
	}
	mylog.Check(luks2.AddKey(dev, newKey, newKey, &options))
	mylog.Check(

		// now it should be possible to kill the temporary keyslot by using the
		// new key for authorization
		luks2.KillSlot(dev, tempKeySlot, newKey))
	mylog.Check(

		// TODO needed?
		luks2.SetSlotPriority(dev, encryptionKeySlot, luks2.SlotPriorityHigh))

	return nil
}
