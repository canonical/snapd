// -*- Mode: Go; indent-tabs-mode: t -*-

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

package secboot

import (
	sb "github.com/snapcore/secboot"
)

type sbKeyResetter struct {
	devicePath          string
	oldKey              sb.DiskUnlockKey
	oldContainerKeySlot string
}

func CreateKeyResetter(key sb.DiskUnlockKey, devicePath string) KeyResetter {
	return &sbKeyResetter{
		devicePath:          devicePath,
		oldKey:              key,
		oldContainerKeySlot: "installation-key",
	}
}

func (kr *sbKeyResetter) Reset(newKey sb.DiskUnlockKey) error {
	defaultKeySlotName := ""
	if err := sb.AddLUKS2ContainerUnlockKey(kr.devicePath, defaultKeySlotName, kr.oldKey, newKey); err != nil {
		return err
	}
	return sb.DeleteLUKS2ContainerKey(kr.devicePath, kr.oldContainerKeySlot)
}

type MockKeyResetter struct {
}

func (kr *MockKeyResetter) Reset(newKey sb.DiskUnlockKey) error {
	return nil
}
