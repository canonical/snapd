// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !nosecboot

/*
 * Copyright (C) 2020 Canonical Ltd
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

package install

import (
	"github.com/snapcore/snapd/secboot"
)

var (
	EnsureLayoutCompatibility = ensureLayoutCompatibility
	DeviceFromRole            = deviceFromRole
	NewEncryptedDevice        = newEncryptedDevice
)

func MockSecbootFormatEncryptedDevice(f func(key secboot.EncryptionKey, label, node string) error) (restore func()) {
	old := secbootFormatEncryptedDevice
	secbootFormatEncryptedDevice = f
	return func() {
		secbootFormatEncryptedDevice = old
	}
}

func MockSecbootAddRecoveryKey(f func(key secboot.EncryptionKey, rkey secboot.RecoveryKey, node string) error) (restore func()) {
	old := secbootAddRecoveryKey
	secbootAddRecoveryKey = f
	return func() {
		secbootAddRecoveryKey = old
	}
}
