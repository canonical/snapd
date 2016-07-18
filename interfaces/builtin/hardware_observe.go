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

// http://bazaar.launchpad.net/~ubuntu-security/ubuntu-core-security/trunk/view/head:/data/apparmor/policygroups/ubuntu-core/16.04/log-observe
const hardwareObserveConnectedPlugAppArmor = `
# Description: Can read udevadm info
# Usage: reserved

#include <abstractions/base>

/bin/udevadm ixr,
/bin/lsblk ixr,
/usr/sbin/dmidecode ixr,
/usr/bin/lsusb ixr,
/etc/udev/udev.conf r,
@{PROC}/*/stat r,
/run/udev/data/* r,
/sys/bus/ r,
/sys/bus/**/ r,
/sys/class/ r,
/sys/class/*/ r,
/sys/devices/** r,
@{PROC}/*/mountinfo r,
@{PROC}/swaps r,
/sys/block/ r,
/sys/devices/** r,
/dev/bus/usb/ r,
/sys/bus/usb/devices/ r,
/var/lib/usbutils/usb.ids r,
/sys/firmware/dmi/tables/DMI r,
/sys/firmware/dmi/tables/smbios_entry_point r,


`

// NewHardwareObserveInterface returns a new "hardware-observe" interface.
func NewHardwareObserveInterface() interfaces.Interface {
	return &commonInterface{
		name: "hardware-observe",
		connectedPlugAppArmor: hardwareObserveConnectedPlugAppArmor,
		reservedForOS:         true,
	}
}
