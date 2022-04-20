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

const acrnSupportSummary = `allows operating managing the ACRN hypervisor`

const acrnSupportBaseDeclarationSlots = `
  acrn-support:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const acrnSupportConnectedPlugAppArmor = `
# Description: Allow write access to acrn_hsm.
/dev/acrn_hsm rw,
# Allow offlining CPU cores
/sys/devices/system/cpu/cpu[0-9]*/online rw,

`

type acrnSupportInterface struct {
	commonInterface
}

var acrnSupportConnectedPlugUDev = []string{
	`KERNEL=="acrn_hsm"`,
}

func init() {
	registerIface(&acrnSupportInterface{commonInterface{
		name:                  "acrn-support",
		summary:               acrnSupportSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		connectedPlugUDev:     acrnSupportConnectedPlugUDev,
		baseDeclarationSlots:  acrnSupportBaseDeclarationSlots,
		connectedPlugAppArmor: acrnSupportConnectedPlugAppArmor,
	}})
}
