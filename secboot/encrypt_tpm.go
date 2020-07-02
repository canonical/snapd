// -*- Mode: Go; indent-tabs-mode: t -*-

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

package secboot

import (
	sb "github.com/snapcore/secboot"
)

var (
	sbInitializeLUKS2Container       = sb.InitializeLUKS2Container
	sbAddRecoveryKeyToLUKS2Container = sb.AddRecoveryKeyToLUKS2Container
)

// FormatEncryptedDevice
func FormatEncryptedDevice(key EncryptionKey, label, node string) error {
	return sbInitializeLUKS2Container(node, label, key[:])
}

// AddRecoveryKey
func AddRecoveryKey(key EncryptionKey, rkey RecoveryKey, node string) error {
	return sbAddRecoveryKeyToLUKS2Container(node, key[:], rkey)
}
