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

// Allow control of the kernel device-mapper. The consuming snap is expected
// to ship the dmsetup tools. A consuming snap may create pseudo devices like
// `dm-zero` with this interface, if the consuming snap also wants to access
// the resulting devices it must add plugs for the `device-mapper-devices`
// interface. To map real block devices plugs for the `block-devices` interface
// must be added.
const deviceMapperControlSummary = `allows control of the kernel device-mapper`

const deviceMapperControlBaseDeclarationPlugs = `
  device-mapper-control:
    allow-installation: false
    deny-auto-connection: true
`

const deviceMapperControlBaseDeclarationSlots = `
  device-mapper-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const deviceMapperControlConnectedPlugAppArmor = `
# Description: Allow control of the kernel device-mapper.

# dmsetup queries which filesystems are supported by the kernel even when
# creating simple pseudo devices
@{PROC}/filesystems r,
@{PROC}/devices r,

# control the kernel device-mapper
/dev/mapper/control rw,

# Perform various privileged block-device ioctl operations
capability sys_admin,
`

var deviceMapperControlConnectedPlugUDev = []string{
	`KERNEL=="device-mapper"`,
}

type deviceMapperControlInterface struct {
	commonInterface
}

func init() {
	registerIface(&deviceMapperControlInterface{commonInterface{
		name:                  "device-mapper-control",
		summary:               deviceMapperControlSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationPlugs:  deviceMapperControlBaseDeclarationPlugs,
		baseDeclarationSlots:  deviceMapperControlBaseDeclarationSlots,
		connectedPlugAppArmor: deviceMapperControlConnectedPlugAppArmor,
		connectedPlugUDev:     deviceMapperControlConnectedPlugUDev,
	}})
}
