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

const ionMemoryControlSummary = `allows access to The Android ION memory allocator`

const ionMemoryControlBaseDeclarationSlots = `
  ion-memory-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const ionMemoryControlBaseDeclarationPlugs = `
  ion-memory-control:
    allow-installation: false
    deny-auto-connection: true
`

const ionMemoryControlConnectedPlugAppArmor = `
# Description: for those who need to talk to the Android ION memory allocator
# /dev/ion
# https://lwn.net/Articles/480055/

/dev/ion rw,
`

var ionMemoryControlConnectedPlugUDev = []string{
	`KERNEL=="ion"`,
}

func init() {
	registerIface(&commonInterface{
		name:                  "ion-memory-control",
		summary:               ionMemoryControlSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  ionMemoryControlBaseDeclarationSlots,
		baseDeclarationPlugs:  ionMemoryControlBaseDeclarationPlugs,
		connectedPlugAppArmor: ionMemoryControlConnectedPlugAppArmor,
		connectedPlugUDev:     ionMemoryControlConnectedPlugUDev,
	})
}
