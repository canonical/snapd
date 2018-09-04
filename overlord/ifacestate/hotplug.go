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
	"strings"

	"github.com/snapcore/snapd/interfaces/hotplug"
)

// List of attributes that determine the computation of default device key.
// Attributes are grouped by similarity, the first non-empty attribute within the group goes into the key.
// The final key is composed of 4 attributes (some of which may be empty), separated by "/".
var attrGroups = [][]string{
	{"ID_V4L_PRODUCT", "NAME", "ID_NET_NAME", "PCI_SLOT_NAME"},
	{"ID_VENDOR_ID", "ID_VENDOR", "ID_WWN", "ID_WWN_WITH_EXTENSION", "ID_VENDOR_FROM_DATABASE", "ID_VENDOR_ENC", "ID_OUI_FROM_DATABASE"},
	{"ID_MODEL_ID", "ID_MODEL_ENC"},
	{"ID_SERIAL", "ID_SERIAL_SHORT", "ID_NET_NAME_MAC", "ID_REVISION"},
}

func defaultDeviceKey(devinfo *hotplug.HotplugDeviceInfo) string {
	key := make([]string, len(attrGroups))
	for i, group := range attrGroups {
		for _, attr := range group {
			if val, ok := devinfo.Attribute(attr); ok && val != "" {
				key[i] = val
				break
			}
		}
	}
	return strings.Join(key, "/")
}

// HotplugDeviceAdded gets called when a device is added to the system.
func (m *InterfaceManager) HotplugDeviceAdded(devinfo *hotplug.HotplugDeviceInfo) {
}

// HotplugDeviceRemoved gets called when a device is removed from the system.
func (m *InterfaceManager) HotplugDeviceRemoved(devinfo *hotplug.HotplugDeviceInfo) {
}
