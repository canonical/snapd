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

const scsiGenericSummary = `allows access to SCSI generic driver devices`

const scsiGenericBaseDeclarationSlots = `
  scsi-generic:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const scsiGenericBaseDeclarationPlugs = `
  block-devices:
    allow-installation: false
    deny-auto-connection: true
`

const scsiGenericConnectedPlugAppArmor = `
# allow read,write access to generic scsi devices
# ref: https://www.kernel.org/doc/Documentation/scsi/scsi-generic.txt
/dev/sg[0-9]* rw,
`

var scsiGenericConnectedPlugUDev = []string{
	// ref: https://www.kernel.org/doc/Documentation/scsi/scsi-generic.txt
	`KERNEL=="sg[0-9]*"`,
}

func init() {
	registerIface(&commonInterface{
		name:                  "scsi-generic",
		summary:               scsiGenericSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  scsiGenericBaseDeclarationSlots,
		connectedPlugAppArmor: scsiGenericConnectedPlugAppArmor,
		connectedPlugUDev:     scsiGenericConnectedPlugUDev,
	})
}
