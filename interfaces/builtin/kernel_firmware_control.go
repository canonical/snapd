// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

// https://www.kernel.org/doc/html/v4.18/driver-api/firmware/fw_search_path.html
const kernelFirmwareControlSummary = `allows setting optional, custom firmware search path`

const kernelFirmwareControlBaseDeclarationPlugs = `
  kernel-firmware-control:
    allow-installation: false
    deny-auto-connection: true
`

const kernelFirmwareControlBaseDeclarationSlots = `
  kernel-firmware-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const kernelFirmwareControlConnectedPlugAppArmor = `
# Description: allows setting optional, custom firmware search path
/sys/module/firmware_class/parameters/path rw,
`

func init() {
	registerIface(&commonInterface{
		name:                  "kernel-firmware-control",
		summary:               kernelFirmwareControlSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationPlugs:  kernelFirmwareControlBaseDeclarationPlugs,
		baseDeclarationSlots:  kernelFirmwareControlBaseDeclarationSlots,
		connectedPlugAppArmor: kernelFirmwareControlConnectedPlugAppArmor,
	})
}
