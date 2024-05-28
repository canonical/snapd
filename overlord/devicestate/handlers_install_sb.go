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

	sb "github.com/snapcore/secboot"

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

	// TODO: we have to remove old keys (could be done in the resetter)
	return secboot.CreateKeyResetter(sb.DiskUnlockKey(saveEncryptionKey), saveNode), nil
}

var createSaveResetter = createSaveResetterImpl
