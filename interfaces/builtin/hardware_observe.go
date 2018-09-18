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

const hardwareObserveSummary = `allows reading information about system hardware`

const hardwareObserveBaseDeclarationSlots = `
  hardware-observe:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const hardwareObserveConnectedPlugAppArmor = `
# Description: This interface allows for getting hardware information
# from the system. This is reserved because it allows reading potentially
# sensitive information.

# used by lscpu and 'lspci -A intel-conf1/intel-conf2'
capability sys_rawio,

# used by lspci
capability sys_admin,
/etc/modprobe.d/{,*} r,
/lib/modprobe.d/{,*} r,

# files in /sys pertaining to hardware (eg, 'lspci -A linux-sysfs')
/sys/{block,bus,class,devices,firmware}/{,**} r,

# files in /proc/bus/pci (eg, 'lspci -A linux-proc')
@{PROC}/bus/pci/{,**} r,

# DMI tables
/sys/firmware/dmi/tables/DMI r,
/sys/firmware/dmi/tables/smbios_entry_point r,

# power information
/sys/power/{,**} r,

# interrupts
@{PROC}/interrupts r,

# libsensors
/etc/sensors3.conf r,
/etc/sensors.d/{,*} r,

# Needed for udevadm
/run/udev/data/** r,
network netlink raw,

# util-linux
/{,usr/}bin/lsblk ixr,
/{,usr/}bin/lscpu ixr,
/{,usr/}bin/lsmem ixr,

# lsmem
/sys/devices/system/memory/block_size_bytes r,
/sys/devices/system/memory/memory[0-9]*/removable r,
/sys/devices/system/memory/memory[0-9]*/state r,
/sys/devices/system/memory/memory[0-9]*/valid_zones r,

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

# kernel uevents
socket AF_NETLINK - NETLINK_KOBJECT_UEVENT
bind
`

func init() {
	registerIface(&commonInterface{
		name:                  "hardware-observe",
		summary:               hardwareObserveSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  hardwareObserveBaseDeclarationSlots,
		connectedPlugAppArmor: hardwareObserveConnectedPlugAppArmor,
		connectedPlugSecComp:  hardwareObserveConnectedPlugSecComp,
		reservedForOS:         true,
	})
}
