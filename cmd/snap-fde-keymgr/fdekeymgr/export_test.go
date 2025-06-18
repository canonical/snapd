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
package fdekeymgr

import (
	"github.com/snapcore/snapd/secboot/keys"
	"github.com/snapcore/snapd/testutil"
)

func MockAddRecoveryKeyToLUKS(f func(recoveryKey keys.RecoveryKey, dev string) error) (restore func()) {
	restore = testutil.Backup(&keymgrAddRecoveryKeyToLUKSDevice)
	keymgrAddRecoveryKeyToLUKSDevice = f
	return restore
}

func MockAddRecoveryKeyToLUKSUsingKey(f func(recoveryKey keys.RecoveryKey, key keys.EncryptionKey, dev string) error) (restore func()) {
	restore = testutil.Backup(&keymgrAddRecoveryKeyToLUKSDeviceUsingKey)
	keymgrAddRecoveryKeyToLUKSDeviceUsingKey = f
	return restore
}

func MockRemoveRecoveryKeyFromLUKS(f func(dev string) error) (restore func()) {
	restore = testutil.Backup(&keymgrRemoveRecoveryKeyFromLUKSDevice)
	keymgrRemoveRecoveryKeyFromLUKSDevice = f
	return restore
}

func MockRemoveRecoveryKeyFromLUKSUsingKey(f func(key keys.EncryptionKey, dev string) error) (restore func()) {
	restore = testutil.Backup(&keymgrRemoveRecoveryKeyFromLUKSDeviceUsingKey)
	keymgrRemoveRecoveryKeyFromLUKSDeviceUsingKey = f
	return restore
}

func MockStageLUKSEncryptionKeyChange(f func(newKey keys.EncryptionKey, dev string) error) (restore func()) {
	restore = testutil.Backup(&keymgrStageLUKSDeviceEncryptionKeyChange)
	keymgrStageLUKSDeviceEncryptionKeyChange = f
	return restore
}

func MockTransitionLUKSEncryptionKeyChange(f func(newKey keys.EncryptionKey, dev string) error) (restore func()) {
	restore = testutil.Backup(&keymgrTransitionLUKSDeviceEncryptionKeyChange)
	keymgrTransitionLUKSDeviceEncryptionKeyChange = f
	return restore
}
