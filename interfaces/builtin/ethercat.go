// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

const ethercatSummary = `allows access to the IgH EtherCAT master`

const ethercatBaseDeclarationSlots = `
  ethercat:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const ethercatConnectedPlugAppArmor = `
# Description: Allows access to the IgH EtherCAT master.
/dev/EtherCAT[0-9]* rw,
`

var ethercatConnectedPlugUDev = []string{
	`SUBSYSTEM=="EtherCAT", KERNEL=="EtherCAT[0-9]*"`,
}

func init() {
	registerIface(&commonInterface{
		name:                  "ethercat",
		summary:               ethercatSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  ethercatBaseDeclarationSlots,
		connectedPlugAppArmor: ethercatConnectedPlugAppArmor,
		connectedPlugUDev:     ethercatConnectedPlugUDev,
	})
}
