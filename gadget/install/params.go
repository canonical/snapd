// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019-2022, 2024 Canonical Ltd
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
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/secboot"
)

type Options struct {
	// Also mount the filesystems after creation
	Mount bool
	// Encrypt the data/save partitions
	EncryptionType secboot.EncryptionType
}

// InstalledSystemSideData carries side data of an installed system, eg. secrets
// to access its partitions.
type InstalledSystemSideData struct {
	// KeysForRoles contains key sets for the relevant structure roles.
	ResetterForRole map[string]secboot.KeyResetter
	// DeviceForRole maps a roles to their corresponding device nodes. For
	// structures with roles that require data to be encrypted, the device
	// is the raw encrypted device node (eg. /dev/mmcblk0p1).
	DeviceForRole map[string]string
}

// partEncryptionData contains meta-data for an encrypted partition.
type partEncryptionData struct {
	role            string
	device          string
	encryptedDevice string

	volName       string
	encryptionKey []byte
	// TODO: this is currently not used
	encryptedSectorSize quantity.Size
	encryptionParams    gadget.StructureEncryptionParameters
}

// EncryptionSetupData stores information needed across install
// API calls.
type EncryptionSetupData struct {
	// maps from partition label to data
	parts map[string]partEncryptionData
}

// EncryptedDevices returns a map partition role -> LUKS mapper device.
func (esd *EncryptionSetupData) EncryptedDevices() map[string]string {
	m := make(map[string]string, len(esd.parts))
	for _, p := range esd.parts {
		m[p.role] = p.encryptedDevice
	}
	return m
}

// MockEncryptedDeviceAndRole is meant to be used for unit tests from other
// packages.
type MockEncryptedDeviceAndRole struct {
	Role            string
	EncryptedDevice string
}

// MockEncryptionSetupData is meant to be used for unit tests from other
// packages.
func MockEncryptionSetupData(labelToEncDevice map[string]*MockEncryptedDeviceAndRole) *EncryptionSetupData {
	esd := &EncryptionSetupData{
		parts: map[string]partEncryptionData{}}
	for label, encryptData := range labelToEncDevice {
		esd.parts[label] = partEncryptionData{
			role:                encryptData.Role,
			encryptedDevice:     encryptData.EncryptedDevice,
			encryptionKey:       []byte{1, 2, 3},
			encryptedSectorSize: 512,
		}
	}
	return esd
}
