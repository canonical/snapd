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

const broadcomAsicControlBaseDeclarationSlots = `
  broadcom-asic-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const broadcomAsicControlConnectedPlugAppArmor = `
# Description: Allow access to broadcom asic kernel module.

/sys/module/linux_bcm_knet/{,**} r,
/sys/module/linux_kernel_bde/{,**} r,
/sys/module/linux_user_bde/{,**} r,
/dev/linux-user-bde rw,
/dev/linux-kernel-bde rw,
/dev/linux-bcm-knet rw,

# These are broader than they needs to be, but until we query udev
# for specific devices, use a broader glob
/sys/devices/pci[0-9a-f]*/**/config r,
/sys/devices/pci[0-9a-f]*/**/{,subsystem_}device r,
/sys/devices/pci[0-9a-f]*/**/{,subsystem_}vendor r,

/sys/bus/pci/devices/ r,
/run/udev/data/+pci:[0-9a-f]* r,
`

var broadcomAsicControlConnectedPlugUDev = []string{
	`SUBSYSTEM=="pci", DRIVER=="linux-kernel-bde"`,
	`SUBSYSTEM=="net", KERNEL=="bcm[0-9]*"`,
}

// The upstream linux kernel doesn't come with support for the
// necessary kernel modules we need to drive a Broadcom ASIC.
// All necessary modules need to be loaded on demand if the
// kernel the device runs with provides them.
var broadcomAsicControlConnectedPlugKMod = []string{
	"linux-user-bde",
	"linux-kernel-bde",
	"linux-bcm-knet",
}

func init() {
	registerIface(&commonInterface{
		name:                     "broadcom-asic-control",
		summary:                  broadcomAsicControlSummary,
		implicitOnCore:           true,
		implicitOnClassic:        true,
		reservedForOS:            true,
		baseDeclarationSlots:     broadcomAsicControlBaseDeclarationSlots,
		connectedPlugAppArmor:    broadcomAsicControlConnectedPlugAppArmor,
		connectedPlugKModModules: broadcomAsicControlConnectedPlugKMod,
		connectedPlugUDev:        broadcomAsicControlConnectedPlugUDev,
	})
}
