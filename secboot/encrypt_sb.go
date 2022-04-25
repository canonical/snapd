// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nosecboot
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

package secboot

import (
	"fmt"

	sb "github.com/snapcore/secboot"

	"github.com/snapcore/snapd/osutil"
)

var (
	sbInitializeLUKS2Container       = sb.InitializeLUKS2Container
	sbAddRecoveryKeyToLUKS2Container = sb.AddRecoveryKeyToLUKS2Container
)

const keyslotsAreaKiBSize = 2560 // 2.5MB
const metadataKiBSize = 2048     // 2MB

// FormatEncryptedDevice initializes an encrypted volume on the block device
// given by node, setting the specified label. The key used to unlock the volume
// is provided using the key argument.
func FormatEncryptedDevice(key EncryptionKey, label, node string) error {
	opts := &sb.InitializeLUKS2ContainerOptions{
		// use a lower, but still reasonable size that should give us
		// enough room
		MetadataKiBSize:     metadataKiBSize,
		KeyslotsAreaKiBSize: keyslotsAreaKiBSize,

		// Use fixed parameters for the KDF to avoid the
		// benchmark. This is okay because we have a high
		// entropy key and the KDF does not gain us much.
		KDFOptions: &sb.KDFOptions{
			MemoryKiB:       32,
			ForceIterations: 4,
		},
	}
	return sbInitializeLUKS2Container(node, label, key[:], opts)
}

// AddRecoveryKey adds a fallback recovery key rkey to the existing encrypted
// volume created with FormatEncryptedDevice on the block device given by node.
// The existing key to the encrypted volume is provided in the key argument.
//
// A heuristic memory cost is used.
func AddRecoveryKey(key EncryptionKey, rkey RecoveryKey, node string) error {
	usableMem, err := osutil.TotalUsableMemory()
	if err != nil {
		return fmt.Errorf("cannot get usable memory for KDF parameters when adding the recovery key: %v", err)
	}
	// The KDF memory is heuristically calculated by taking the
	// usable memory and subtracting hardcoded 384MB that is
	// needed to keep the system working. Half of that is the mem
	// we want to use for the KDF. Doing it this way avoids the expensive
	// benchmark from cryptsetup. The recovery key is already 128bit
	// strong so we don't need to be super precise here.
	kdfMem := (int(usableMem) - 384*1024*1024) / 2
	// max 1 GB
	if kdfMem > 1024*1024*1024 {
		kdfMem = (1024 * 1024 * 1024)
	}
	// min 32 KB
	if kdfMem < 32*1024 {
		kdfMem = 32 * 1024
	}
	opts := &sb.KDFOptions{
		MemoryKiB:       kdfMem / 1024,
		ForceIterations: 4,
	}

	return sbAddRecoveryKeyToLUKS2Container(node, key[:], sb.RecoveryKey(rkey), opts)
}

func (k RecoveryKey) String() string {
	return sb.RecoveryKey(k).String()
}

// EnsureRecoveryKey makes sure the encrypted block devices have a recovery key.
// XXX what is the right signature for this?
func EnsureRecoveryKey(fdeDir string) (RecoveryKey, error) {
	return RecoveryKey{}, fmt.Errorf("not implemented yet")
}

// RemoveRecoveryKeys removes any recovery key from all encrypted block devices.
// XXX what is the right signature for this?
func RemoveRecoveryKeys(fdeDir string) error {
	return fmt.Errorf("not implemented yet")
}
