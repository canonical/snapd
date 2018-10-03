// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package ifacestate

import (
	"bytes"
	"crypto/sha256"
	"fmt"

	"github.com/snapcore/snapd/interfaces/hotplug"
)

// List of attributes that determine the computation of default device key.
// Attributes are grouped by similarity, the first non-empty attribute within the group goes into the key.
// The final key is composed of 4 attributes (some of which may be empty), separated by "/".
// Warning, any future changes to these definitions requrie a new key version.
var attrGroups = [][][]string{
	// key version 0
	{
		// Name
		{"ID_V4L_PRODUCT", "NAME", "ID_NET_NAME", "PCI_SLOT_NAME"},
		// Vendor
		{"ID_VENDOR_ID", "ID_VENDOR", "ID_WWN", "ID_WWN_WITH_EXTENSION", "ID_VENDOR_FROM_DATABASE", "ID_VENDOR_ENC", "ID_OUI_FROM_DATABASE"},
		// Model
		{"ID_MODEL_ID", "ID_MODEL_ENC"},
		// Identifier
		{"ID_SERIAL", "ID_SERIAL_SHORT", "ID_NET_NAME_MAC", "ID_REVISION"},
	},
}

// deviceKeyVersion is the current version number for the default keys computed by hotplug subsystem.
// Fresh device keys always use current version format
var deviceKeyVersion = len(attrGroups) - 1

// defaultDeviceKey computes device key from the attributes of
// HotplugDeviceInfo. Empty string is returned if too few attributes are present
// to compute a good key. Attributes used to compute device key are defined in
// attrGroups list above and they depend on the keyVersion passed to the
// function.
// The resulting key returned by the function has the following format:
// <version>:<checksum> where checksum is the sha256 checksum computed over
// select attributes of the device.
func defaultDeviceKey(devinfo *hotplug.HotplugDeviceInfo, keyVersion int) string {
	var key bytes.Buffer
	found := 0
	attrs := attrGroups[keyVersion]
	for i, group := range attrs {
		for _, attr := range group {
			if val, ok := devinfo.Attribute(attr); ok && val != "" {
				key.Write([]byte(val))
				found++
				break
			}
		}
		if i < len(attrs)-1 {
			// values that compose the key are separated by null character
			key.Write([]byte{0})
		}
	}
	if found < 2 {
		return ""
	}
	return fmt.Sprintf("%d:%x", keyVersion, sha256.Sum256(key.Bytes()))
}

// HotplugDeviceAdded gets called when a device is added to the system.
func (m *InterfaceManager) HotplugDeviceAdded(devinfo *hotplug.HotplugDeviceInfo) {
}

// HotplugDeviceRemoved gets called when a device is removed from the system.
func (m *InterfaceManager) HotplugDeviceRemoved(devinfo *hotplug.HotplugDeviceInfo) {
}
