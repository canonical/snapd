// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nosecboot

/*
 * Copyright (C) 2024 Canonical Ltd
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

package devicestate

import (
	"fmt"
	"path/filepath"

	sb "github.com/snapcore/secboot"

	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/secboot/keys"
)

var (
	sbGetDiskUnlockKeyFromKernel = sb.GetDiskUnlockKeyFromKernel
	sbAddLUKS2ContainerUnlockKey = sb.AddLUKS2ContainerUnlockKey
)

func createSaveResetterImpl(saveNode string) (secboot.KeyResetter, error) {
	// new encryption key for save
	saveEncryptionKey, err := keys.NewEncryptionKey()
	if err != nil {
		return nil, fmt.Errorf("cannot create encryption key: %v", err)
	}

	const defaultPrefix = "ubuntu-fde"
	unlockKey, err := sbGetDiskUnlockKeyFromKernel(defaultPrefix, saveNode, false)
	if err != nil {
		return nil, fmt.Errorf("cannot get key for unlocked disk %s: %v", saveNode, err)
	}

	if err := sbAddLUKS2ContainerUnlockKey(saveNode, "installation-key", sb.DiskUnlockKey(unlockKey), sb.DiskUnlockKey(saveEncryptionKey)); err != nil {
		return nil, fmt.Errorf("cannot enroll new installation key: %v", err)
	}

	// FIXME: listing keys, then modifying could be a TOCTOU issue.
	// we expect here nothing else is messing with the key slots.
	slots, err := sb.ListLUKS2ContainerUnlockKeyNames(saveNode)
	if err != nil {
		return nil, fmt.Errorf("cannot list slots in partition save partition: %v", err)
	}
	renames := map[string]string{
		"default":          "factory-reset-old",
		"default-fallback": "factory-reset-old-fallback",
		"save":             "factory-reset-old-save",
	}
	for _, slot := range slots {
		renameTo, found := renames[slot]
		if found {
			if err := sb.RenameLUKS2ContainerKey(saveNode, slot, renameTo); err != nil {
				if err == sb.ErrMissingCryptsetupFeature {
					if err := sb.DeleteLUKS2ContainerKey(saveNode, slot); err != nil {
						return nil, fmt.Errorf("cannot remove old container key: %v", err)
					}
				} else {
					return nil, fmt.Errorf("cannot rename container key: %v", err)
				}
			}
		}
	}

	return secboot.CreateKeyResetter(sb.DiskUnlockKey(saveEncryptionKey), saveNode), nil
}

var createSaveResetter = createSaveResetterImpl

func deleteOldSaveKeyImpl(saveMntPnt string) error {
	// FIXME: maybe there is better if we had a function returning the devname instead.
	partUUID, err := disks.PartitionUUIDFromMountPoint(saveMntPnt, &disks.Options{
		IsDecryptedDevice: true,
	})
	if err != nil {
		return fmt.Errorf("cannot partition save partition: %v", err)
	}

	diskPath := filepath.Join("/dev/disk/by-partuuid", partUUID)
	slots, err := sb.ListLUKS2ContainerUnlockKeyNames(diskPath)
	if err != nil {
		return fmt.Errorf("cannot list slots in partition save partition: %v", err)
	}

	toDelete := map[string]bool{
		"factory-reset-old":          true,
		"factory-reset-old-fallback": true,
		"factory-reset-old-save":     true,
	}

	for _, slot := range slots {
		if toDelete[slot] {
			if err := sb.DeleteLUKS2ContainerKey(diskPath, slot); err != nil {
				return fmt.Errorf("cannot remove old container key: %v", err)
			}
		}
	}

	return nil
}

var deleteOldSaveKey = deleteOldSaveKeyImpl
