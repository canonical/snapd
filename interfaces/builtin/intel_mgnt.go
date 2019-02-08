// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

const intelManagementSummary = `allows access to the intel management interface`

const intelManagementBaseDeclarationSlots = `
  intel-management-interface:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const intelManagementConnectedPlugAppArmor = `
# Description: Allow access to the Intel management interface.
/dev/mei[0-9]+ rw,
`

var intelManagementConnectedPlugUDev = []string{`KERNEL=="mei[0-9]+"`}

func init() {
	registerIface(&commonInterface{
		name:                  "intel-management-interface",
		summary:               intelManagementSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  intelManagementBaseDeclarationSlots,
		connectedPlugAppArmor: intelManagementConnectedPlugAppArmor,
		connectedPlugUDev:     intelManagementConnectedPlugUDev,
		reservedForOS:         true,
	})
}
