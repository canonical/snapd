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
	"github.com/snapcore/snapd/osutil"
)

type bootstrappedContainer struct {
	oldContainerKeySlot string
	devicePath          string
	key                 DiskUnlockKey
	finished            bool
}

func newLUKS2KeyDataWriterImpl(devicePath string, name string) (KeyDataWriter, error) {
	return sb.NewLUKS2KeyDataWriter(devicePath, name)
}

var newLUKS2KeyDataWriter = newLUKS2KeyDataWriterImpl

func slotNameOrDefault(slotName string) string {
	if slotName == "" {
		return "default"
	}

	return slotName
}

func (bc *bootstrappedContainer) AddKey(slotName string, newKey []byte) error {
	if bc.finished {
		return fmt.Errorf("internal error: key resetter was a already finished")
	}

	if err := sbAddLUKS2ContainerUnlockKey(bc.devicePath, slotNameOrDefault(slotName), sb.DiskUnlockKey(bc.key), sb.DiskUnlockKey(newKey)); err != nil {
		return err
	}
	return nil
}

func (bc *bootstrappedContainer) GetTokenWriter(slotName string) (KeyDataWriter, error) {
	return newLUKS2KeyDataWriter(bc.devicePath, slotNameOrDefault(slotName))
}

func (bc *bootstrappedContainer) RemoveBootstrapKey() error {
	if bc.finished {
		return nil
	}
	bc.finished = true

	if err := sbDeleteLUKS2ContainerKey(bc.devicePath, bc.oldContainerKeySlot); err != nil {
		return fmt.Errorf("cannot remove bootstrap key: %v", err)
	}

	return nil
}

func createBootstrappedContainerImpl(key DiskUnlockKey, devicePath string) BootstrappedContainer {
	return &bootstrappedContainer{
		oldContainerKeySlot: "bootstrap-key",
		devicePath:          devicePath,
		key:                 key,
		finished:            false,
	}
}

func init() {
	CreateBootstrappedContainer = createBootstrappedContainerImpl
}

func MockCreateBootstrappedContainer(f func(key DiskUnlockKey, devicePath string) BootstrappedContainer) func() {
	osutil.MustBeTestBinary("MockCreateBootstrappedContainer can be only called from tests")
	old := CreateBootstrappedContainer
	CreateBootstrappedContainer = f
	return func() {
		CreateBootstrappedContainer = old
	}
}
