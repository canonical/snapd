// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

const cpuControlSummary = `allows setting CPU tunables`

const cpuControlBaseDeclarationSlots = `
  cpu-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const cpuControlConnectedPlugAppArmor = `
# Description: This interface allows for setting CPU tunables
/sys/devices/system/cpu/**/ r,
/sys/devices/system/cpu/cpu*/online rw,
/sys/devices/system/cpu/smt/control rw,
`

func init() {
	registerIface(&commonInterface{
		name:                  "cpu-control",
		summary:               cpuControlSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  cpuControlBaseDeclarationSlots,
		connectedPlugAppArmor: cpuControlConnectedPlugAppArmor,
		reservedForOS:         true,
	})
}
