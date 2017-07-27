// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

const broadcomAsicControlSummary = `allows using the broadcom-asic kernel module`

const broadcomAsicControlDescription = `
The broadcom-asic-control interfaces allows connected plugs to read and sometimes
write files required to use the broadcom asic kernel module.`

const broadcomAsicControlBaseDeclarationSlots = `
  broadcom-asic-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`
const broadcomAsicControlConnectedPlugAppArmor = `
# Description: Allow access to broadcom asic kernel module.

/sys/module/linux_kernel_bde/initstate r,
/sys/module/linux_user_bde/initstate r,
/sys/module/linux_kernel_bde/holders/ r,
/sys/module/linux_user_bde/holders/ r,
/sys/module/linux_user_bde/holders/** r,
/sys/module/linux_user_bde/refcnt r,
/sys/module/linux_bcm_knet/initstate r,
/sys/module/linux_bcm_knet/holders/ r,
/sys/module/linux_bcm_knet/refcnt r,
/dev/linux-user-bde rw,
/dev/linux-kernel-bde rw,
/dev/linux-bcm-knet wr,
`

func init() {
	registerIface(&commonInterface{
		name:                  "broadcom-asic-control",
		summary:               broadcomAsicControlSummary,
		description:           broadcomAsicControlDescription,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  broadcomAsicControlBaseDeclarationSlots,
		connectedPlugAppArmor: broadcomAsicControlConnectedPlugAppArmor,
		reservedForOS:         true,
	})
}
