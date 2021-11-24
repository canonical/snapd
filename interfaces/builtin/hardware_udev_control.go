// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

const hardwareUdevControlSummary = `allows control over the hardware udev daemon`

const hardwareUdevControlBaseDeclarationSlots = `
  hardware-udev-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const hardwareUdevControlConnectedPlugAppArmor = `

# Description: allow direct access to the hardware udev daemon control
# socket and uevent files of all devices. Allows performing any
# 'trigger' and 'control' commands using udevadm.

# Allow 'udevadm trigger'
/sys/**/uevent w,

# Allow 'udevadm control'
/run/udev/control rw,
`

func init() {
	registerIface(&commonInterface{
		name:                  "hardware-udev-control",
		summary:               hardwareUdevControlSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  hardwareUdevControlBaseDeclarationSlots,
		connectedPlugAppArmor: hardwareUdevControlConnectedPlugAppArmor,
	})
}
