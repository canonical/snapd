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

package secboot

import (
	"fmt"

	sb "github.com/snapcore/secboot"
)

type sbKeyResetter struct {
	devicePath          string
	oldKey              sb.DiskUnlockKey
	oldContainerKeySlot string
	finished            bool
}

func createKeyResetterImpl(key sb.DiskUnlockKey, devicePath string) KeyResetter {
	return &sbKeyResetter{
		devicePath:          devicePath,
		oldKey:              key,
		oldContainerKeySlot: "installation-key",
	}
}

var CreateKeyResetter = createKeyResetterImpl

func (kr *sbKeyResetter) AddKey(slotName string, newKey []byte, token bool) (KeyDataWriter, error) {
	if kr.finished {
		return nil, fmt.Errorf("internal error: key resetter was a already finished")
	}
	if slotName == "" {
		slotName = "default"
	}
	if err := sb.AddLUKS2ContainerUnlockKey(kr.devicePath, slotName, kr.oldKey, sb.DiskUnlockKey(newKey)); err != nil {
		return nil, err
	}
	if !token {
		return nil, nil
	}
	writer, err := sb.NewLUKS2KeyDataWriter(kr.devicePath, slotName)
	if err != nil {
		return nil, err
	}
	return writer, nil
}

func (kr *sbKeyResetter) RemoveInstallationKey() error {
	if kr.finished {
		return nil
	}
	kr.finished = true
	return sb.DeleteLUKS2ContainerKey(kr.devicePath, kr.oldContainerKeySlot)
}

func MockCreateKeyResetter(f func(key sb.DiskUnlockKey, devicePath string) KeyResetter) func() {
	old := CreateKeyResetter
	CreateKeyResetter = f
	return func() {
		CreateKeyResetter = old
	}
}
