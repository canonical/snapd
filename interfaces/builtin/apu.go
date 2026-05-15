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

const apuSummary = `allows access to the MTK apu(AI processing unit) devices`

const apuBaseDeclarationSlots = `
  apu:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const apuConnectedPlugAppArmor = `
# Description: Allow reading and writing to the MTK apu
# (AI processing unit, /dev/apusys)

/dev/apusys rw,
`

var apuConnectedPlugUDev = []string{`SUBSYSTEM=="misc", KERNEL=="apusys"`}

func init() {
	registerIface(&commonInterface{
		name:                  "apu",
		summary:               apuSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  apuBaseDeclarationSlots,
		connectedPlugAppArmor: apuConnectedPlugAppArmor,
		connectedPlugUDev:     apuConnectedPlugUDev,
	})
}
