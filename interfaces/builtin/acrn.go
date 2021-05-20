/*
 * Copyright (C) 2021 Intel Corporation
 *
 * This program is free software; you can redistribute it and/or modify it
 * under the terms of the GNU General Public License, as published
 * by the Free Software Foundation; either version 2 of the License,
 * or (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program; if not, see <http://www.gnu.org/licenses/>.
 *
 *
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

package builtin

import (
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/kmod"
)

const acrnSummary = `allows access to the ACRN device`

const acrnBaseDeclarationPlugs = `
  acrn:
    allow-installation: false
    deny-auto-connection: true
`

const acrnBaseDeclarationSlots = `
  acrn:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const acrnConnectedPlugAppArmor = `
# Description: Allow access to resources required by ACRN.
#
# allow setting certain CPU cores offline
/sys/devices/system/cpu/cpu[0-9]*/online w,

# allow write access to ACRN Virtio and Hypervisor service Module
/dev/acrn_vhm rw,
/dev/acrn_hsm rw,
`

var acrnConnectedPlugUDev = []string{
	`SUBSYSTEM=="vhm"`,
	`SUBSYSTEM=="hsm"`,
}

type acrnInterface struct {
	commonInterface
}

func (iface *acrnInterface) KModConnectedPlug(spec *kmod.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	/* ACRN Hypervisor Service Module (HSM) is supported since kernel 5.12 */
	_ = spec.AddModule("acrn")
	return nil
}

func init() {
	registerIface(&acrnInterface{commonInterface{
		name:                  "acrn",
		summary:               acrnSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationPlugs:  acrnBaseDeclarationPlugs,
		baseDeclarationSlots:  acrnBaseDeclarationSlots,
		connectedPlugAppArmor: acrnConnectedPlugAppArmor,
		connectedPlugUDev:     acrnConnectedPlugUDev,
	}})
}
