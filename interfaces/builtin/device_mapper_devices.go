// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

// Allow access to already prepared device-mapper devices, such as LVM, raid,
// etc. To control device-mapper devices add a plug for the
// `device-mapper-control` interface. If you want to control LVM devices
// add a plug for the `lvm-control` interface.
const deviceMapperDevicesSummary = `allows access to device-mapper devices`

const deviceMapperDevicesBaseDeclarationPlugs = `
  device-mapper-devices:
    allow-installation: false
    deny-auto-connection: true
`

const deviceMapperDevicesBaseDeclarationSlots = `
  device-mapper-devices:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const deviceMapperDevicesConnectedPlugAppArmor = `
# Description: Allow access to device-mapper devices.

@{PROC}/devices r,
/run/udev/data/b[0-9]*:[0-9]* r,
/sys/block/ r,
/sys/devices/**/block/** r,

# Access to Device Mapper devices (including LVM logical volume devices)
/dev/dm-[0-9]{,[0-9],[0-9][0-9]} rwk,                   # Device Mapper (up to 1000 devices)

# SCSI device commands, et al
capability sys_rawio,

# Perform various privileged block-device ioctl operations
capability sys_admin,
`

var deviceMapperDevicesConnectedPlugUDev = []string{
	`SUBSYSTEM=="block"`,
}

type deviceMapperDevicesInterface struct {
	commonInterface
}

func init() {
	registerIface(&deviceMapperDevicesInterface{commonInterface{
		name:                  "device-mapper-devices",
		summary:               deviceMapperDevicesSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationPlugs:  deviceMapperDevicesBaseDeclarationPlugs,
		baseDeclarationSlots:  deviceMapperDevicesBaseDeclarationSlots,
		connectedPlugAppArmor: deviceMapperDevicesConnectedPlugAppArmor,
		connectedPlugUDev:     deviceMapperDevicesConnectedPlugUDev,
	}})
}
