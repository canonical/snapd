// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package builtin

import (
	"github.com/snapcore/snapd/interfaces"
)

const rawusbConnectedPlugAppArmor = `
# Description: Allow raw access to all connected USB devices.
# Reserved because this gives privileged access to the system.
# Usage: reserved
/dev/bus/usb/[0-9][0-9][0-9]/[0-9][0-9][0-9] rw,

# Allow detection of usb devices. Leaks plugged in USB device info
/sys/bus/usb/devices/ r,
/sys/devices/pci**/usb[0-9]** r,
/sys/devices/platform/soc/*.usb/usb[0-9]** r,

/run/udev/data/c16[67]:[0-9] r, # ACM USB modems
/run/udev/data/b180:*    r, # various USB block devices
/run/udev/data/c18[089]:* r, # various USB character devices: USB serial converters, etc.
/run/udev/data/+usb:* r,
`

// Transitional interface which allows access to all usb devices.
func NewRawUsbInterface() interfaces.Interface {
	return &commonInterface{
		name: "raw-usb",
		connectedPlugAppArmor: rawusbConnectedPlugAppArmor,
		reservedForOS:         true,
	}
}
