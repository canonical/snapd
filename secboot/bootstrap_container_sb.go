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
	"strings"

	"golang.org/x/sys/unix"

	sb "github.com/snapcore/secboot"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/secboot/keys"
)

type bootstrappedContainer struct {
	tempContainerKeySlot string
	devicePath           string
	key                  DiskUnlockKey
	finished             bool
}

func newLUKS2KeyDataWriterImpl(devicePath string, name string) (KeyDataWriter, error) {
	return sb.NewLUKS2KeyDataWriter(devicePath, name)
}

var (
	newLUKS2KeyDataWriter = newLUKS2KeyDataWriterImpl
	unixAddKey            = unix.AddKey
)

func slotNameOrDefault(slotName string) string {
	if slotName == "" {
		return "default"
	}

	return slotName
}

func (bc *bootstrappedContainer) AddKey(slotName string, newKey []byte) error {
	if bc.finished {
		return fmt.Errorf("internal error: bootstrapped container was a already finished")
	}

	if err := sbAddLUKS2ContainerUnlockKey(bc.devicePath, slotNameOrDefault(slotName), sb.DiskUnlockKey(bc.key), sb.DiskUnlockKey(newKey)); err != nil {
		return err
	}
	return nil
}

func (bc *bootstrappedContainer) AddRecoveryKey(slotName string, rkey keys.RecoveryKey) error {
	if bc.finished {
		return fmt.Errorf("internal error: bootstrapped container was a already finished")
	}

	if slotName == "" {
		slotName = "default-recovery"
	}

	if err := sbAddLUKS2ContainerRecoveryKey(bc.devicePath, slotName, sb.DiskUnlockKey(bc.key), sb.RecoveryKey(rkey)); err != nil {
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

	if err := sbDeleteLUKS2ContainerKey(bc.devicePath, bc.tempContainerKeySlot); err != nil {
		return fmt.Errorf("cannot remove bootstrap key: %v", err)
	}

	return nil
}

func (bc *bootstrappedContainer) RegisterKeyAsUsed(primaryKey []byte, unlockKey []byte) {
	// secboot unlocking does not fail when it cannot save keys to the kerying. So
	// we also want to have a similar behavior and just print warnings in this function.
	devlinks, err := disksDevlinks(bc.devicePath)
	if err != nil {
		logger.Noticef("warning: cannot find symlinks for %s: %v", bc.devicePath, err)
		return
	}

	var uuidDevlink string
	for _, devlink := range devlinks {
		if strings.HasPrefix(devlink, "/dev/disk/by-uuid/") {
			uuidDevlink = devlink
			break
		}
	}
	if uuidDevlink == "" {
		logger.Noticef("warning: missing by-uuid symlink for %s", bc.devicePath)
		return
	}

	logger.Debugf("registering kerying keys for %s (%s)", bc.devicePath, uuidDevlink)

	// Format of key for secboot is <prefix>:<path>:<purpose>.
	// See internal/keyring/keyring.go in secboot.
	// "purpose" is either "aux" or "unlock".
	// See crypt.go in secboot.
	if _, err := unixAddKey("user", fmt.Sprintf("%s:%s:unlock", defaultKeyringPrefix, uuidDevlink), unlockKey, unix.KEY_SPEC_USER_KEYRING); err != nil {
		logger.Noticef("warning: cannot register unlock key for %s: %v", uuidDevlink, err)
	}
	if _, err := unixAddKey("user", fmt.Sprintf("%s:%s:aux", defaultKeyringPrefix, uuidDevlink), primaryKey, unix.KEY_SPEC_USER_KEYRING); err != nil {
		logger.Noticef("warning: cannot register primary key for %s: %v", uuidDevlink, err)
	}
}

func createBootstrappedContainerImpl(key DiskUnlockKey, devicePath string) BootstrappedContainer {
	return &bootstrappedContainer{
		tempContainerKeySlot: "bootstrap-key",
		devicePath:           devicePath,
		key:                  key,
		finished:             false,
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
