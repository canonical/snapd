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

# The device info "0000:00:1c.0" can be found under the sysfs
# and it's the parent device of first matched device node "0000:01:00.0"
/sys/devices/pci[0-9]*/0000:00:1c.0/config r,
/sys/devices/pci[0-9]*/0000:00:1c.0/{,subsystem_}device r,
/sys/devices/pci[0-9]*/0000:00:1c.0/{,subsystem_}vendor r,

/sys/bus/pci/devices/0000:00:1c.0/** r,
/run/udev/data/+pci:0000:00:1c.0 r,
`

const broadcomAsicControlConnectedPlugUDev = `
SUBSYSTEM=="net", KERNEL=="bdev", TAG+="###SLOT_SECURITY_TAGS###"
`

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
