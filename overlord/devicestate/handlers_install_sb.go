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
		return nil, fmt.Errorf("cannot get key for unlocked disk: %v", err)
	}

	if err := sbAddLUKS2ContainerUnlockKey(saveNode, "installation-key", sb.DiskUnlockKey(unlockKey), sb.DiskUnlockKey(saveEncryptionKey)); err != nil {
		return nil, fmt.Errorf("cannot enroll new installation key: %v", err)
	}

	// FIXME: if the key has already be renamed, that is "default"
	// does not exist, but "factory-reset-old" does, then we
	// should ignore it.
	if err := sb.RenameLUKS2ContainerKey(saveNode, "default", "factory-reset-old"); err != nil {
		return nil, fmt.Errorf("cannot rename container key: %v", err)
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

	slots, err := sb.ListLUKS2ContainerUnlockKeyNames(filepath.Join("/dev/disk/by-partuuid", partUUID))
	if err != nil {
		return fmt.Errorf("cannot list slots in partition save partition: %v", err)
	}

	for _, slot := range slots {
		if slot == "factory-reset-old" {
			if err := sb.DeleteLUKS2ContainerKey(filepath.Join("/dev/disk/by-partuuid", partUUID), "factory-reset-old"); err != nil {
				return fmt.Errorf("cannot remove old container key: %v", err)
			}
			return nil
		}
	}

	return nil
}

var deleteOldSaveKey = deleteOldSaveKeyImpl
