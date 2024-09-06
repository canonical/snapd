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

	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/secboot/keys"
)

func createSaveResetterImpl(saveNode string) (secboot.KeyResetter, error) {
	// new encryption key for save
	saveEncryptionKey, err := keys.NewEncryptionKey()
	if err != nil {
		return nil, fmt.Errorf("cannot create encryption key: %v", err)
	}

	if err := secboot.AddInstallationKeyOnExistingDisk(saveNode, saveEncryptionKey); err != nil {
		return nil, err
	}

	renames := map[string]string{
		"default":          "factory-reset-old",
		"default-fallback": "factory-reset-old-fallback",
		"save":             "factory-reset-old-save",
	}
	if err := secboot.RenameOrDeleteKeys(saveNode, renames); err != nil {
		return nil, err
	}

	return secboot.CreateKeyResetter(secboot.DiskUnlockKey(saveEncryptionKey), saveNode), nil
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

	toDelete := map[string]bool{
		"factory-reset-old":          true,
		"factory-reset-old-fallback": true,
		"factory-reset-old-save":     true,
	}

	return secboot.DeleteKeys(diskPath, toDelete)
}

var deleteOldSaveKey = deleteOldSaveKeyImpl
