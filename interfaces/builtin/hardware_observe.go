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

const hardwareObserveConnectedPlugAppArmor = `
# Description: This interface allows for getting hardware information
# from the system. This is reserved because it allows reading potentially
# sensitive information.

# used by lscpu and 'lspci -A intel-conf1/intel-conf2'
capability sys_rawio,

# used by lspci
capability sys_admin,
/etc/modprobe.d/{,*} r,

# files in /sys pertaining to hardware (eg, 'lspci -A linux-sysfs')
/sys/{block,bus,class,devices,firmware}/{,**} r,

# files in /proc/bus/pci (eg, 'lspci -A linux-proc')
@{PROC}/bus/pci/{,**} r,

# DMI tables
/sys/firmware/dmi/tables/DMI r,
/sys/firmware/dmi/tables/smbios_entry_point r,

# interrupts
@{PROC}/interrupts r,

# Needed for udevadm
/run/udev/data/** r,

# util-linux
/{,usr/}bin/lscpu ixr,

# lsusb
# Note: lsusb and its database have to be shipped in the snap if not on classic
/{,usr/}bin/lsusb ixr,
/var/lib/usbutils/usb.ids r,
/dev/ r,
/dev/bus/usb/{,**/} r,
/etc/udev/udev.conf r,

# lshw -quiet (note, lshw also tries to create /dev/fb-*, but fails gracefully)
@{PROC}/devices r,
@{PROC}/ide/{,**} r,
@{PROC}/scsi/{,**} r,
@{PROC}/device-tree/{,**} r,
/sys/kernel/debug/usb/devices r,
@{PROC}/sys/abi/{,*} r,
`

const hardwareObserveConnectedPlugSecComp = `
# Description: This interface allows for getting hardware information
# from the system. This is reserved because it allows reading potentially
# sensitive information.

# used by 'lspci -A intel-conf1/intel-conf2'
iopl

# multicast statistics
socket AF_NETLINK - NETLINK_GENERIC
`

// NewHardwareObserveInterface returns a new "hardware-observe" interface.
func NewHardwareObserveInterface() interfaces.Interface {
	return &commonInterface{
		name: "hardware-observe",
		connectedPlugAppArmor: hardwareObserveConnectedPlugAppArmor,
		connectedPlugSecComp:  hardwareObserveConnectedPlugSecComp,
		reservedForOS:         true,
	}
}
