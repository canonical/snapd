// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nosecboot

/*
 * Copyright (C) 2020, 2024 Canonical Ltd
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

package install

import (
	"fmt"

	"github.com/snapcore/snapd/secboot"
)

var (
	secbootFormatEncryptedDevice = secboot.FormatEncryptedDevice
)

// encryptedDeviceCryptsetup represents a encrypted block device.
type encryptedDevice interface {
	Node() string
	Close() error
}

// encryptedDeviceLUKS represents a LUKS-backed encrypted block device.
type encryptedDeviceLUKS struct {
	parent string
	name   string
	node   string
}

// expected interface is implemented
var _ = encryptedDevice(&encryptedDeviceLUKS{})

// newEncryptedDeviceLUKS creates an encrypted device in the existing
// partition using the specified key with the LUKS backend.
func newEncryptedDeviceLUKS(devNode string, encType secboot.EncryptionType, key secboot.DiskUnlockKey, label, name string) (encryptedDevice, error) {
	encLabel := label + "-enc"
	if err := secbootFormatEncryptedDevice(key, encType, encLabel, devNode); err != nil {
		return nil, fmt.Errorf("cannot format encrypted device: %v", err)
	}

	if err := cryptsetupOpen(key, devNode, name); err != nil {
		return nil, fmt.Errorf("cannot open encrypted device on %s: %s", devNode, err)
	}

	dev := &encryptedDeviceLUKS{
		parent: devNode,
		name:   name,
		// A new block device is used to access the encrypted data. Note that
		// you can't open an encrypted device under different names and a name
		// can't be used in more than one device at the same time.
		node: fmt.Sprintf("/dev/mapper/%s", name),
	}
	return dev, nil
}

func (dev *encryptedDeviceLUKS) Node() string {
	return dev.node
}

func (dev *encryptedDeviceLUKS) Close() error {
	return cryptsetupClose(dev.name)
}

func cryptsetupOpenImpl(key secboot.DiskUnlockKey, node, name string) error {
	return secboot.ActivateVolumeWithKey(name, node, key, nil)
}

var cryptsetupOpen = cryptsetupOpenImpl

func cryptsetupCloseImpl(name string) error {
	return secboot.DeactivateVolume(name)
}

var cryptsetupClose = cryptsetupCloseImpl
