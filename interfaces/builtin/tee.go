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

const teeSummary = `allows access to Trusted Execution Environment devices`

const teeBaseDeclarationSlots = `
  tee:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const teeBaseDeclarationPlugs = `
  tee:
    allow-installation: false
    deny-auto-connection: true
`

const teeConnectedPlugAppArmor = `
# Description: for those who need to talk to the TEE subsystem over
# /dev/tee[0-9]* and/or /dev/teepriv[0-0]*

/dev/tee[0-9]* rw,
/dev/teepriv[0-9]* rw,
`

var teeConnectedPlugUDev = []string{
	`KERNEL=="tee[0-9]*"`,
	`KERNEL=="teepriv[0-9]*"`,
}

func init() {
	registerIface(&commonInterface{
		name:                  "tee",
		summary:               teeSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  teeBaseDeclarationSlots,
		baseDeclarationPlugs:  teeBaseDeclarationPlugs,
		connectedPlugAppArmor: teeConnectedPlugAppArmor,
		connectedPlugUDev:     teeConnectedPlugUDev,
	})
}
