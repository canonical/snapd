// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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

const tpmSummary = `allows access to the Trusted Platform Module device`

const tpmBaseDeclarationSlots = `
  tpm:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const tpmConnectedPlugAppArmor = `
# Description: for those who need to talk to the system TPM chip over /dev/tpm0
# and kernel TPM resource manager /dev/tpmrm0 (4.12+)

/dev/tpm0 rw,
/dev/tpmrm0 rw,
`

var tpmConnectedPlugUDev = []string{
	`KERNEL=="tpm[0-9]*"`,
	`KERNEL=="tpmrm[0-9]*"`,
}

func init() {
	registerIface(&commonInterface{
		name:                  "tpm",
		summary:               tpmSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  tpmBaseDeclarationSlots,
		connectedPlugAppArmor: tpmConnectedPlugAppArmor,
		connectedPlugUDev:     tpmConnectedPlugUDev,
		reservedForOS:         true,
	})
}
