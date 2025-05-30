// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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

const accelSummary = `allows access to devices in the compute accelerators subsystem`

const accelBaseDeclarationSlots = `
  accel:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: false
`

const accelConnectedPlugAppArmor = `
# Description: Provide permissions for accessing devices and files of the compute accelerator subsystem

# Access to accel character devices such as /dev/accel/accel0
#
# https://docs.kernel.org/accel/introduction.html
#
/dev/accel/accel* rw,
`

var accelConnectedPlugUDev = []string{
	`SUBSYSTEM=="accel", KERNEL=="accel*"`,
}

func init() {
	registerIface(&commonInterface{
		name:                  "accel",
		summary:               accelSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  accelBaseDeclarationSlots,
		connectedPlugAppArmor: accelConnectedPlugAppArmor,
		connectedPlugUDev:     accelConnectedPlugUDev,
	})
}
