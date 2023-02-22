// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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

const nvidiaSupportSummary = `allows creating static and dynamic NVIDIA chrdev`

const nvidiaSupportBaseDeclarationPlugs = `
  nvidia-support:
    allow-installation: false
    deny-auto-connection: true
`

const nvidiaSupportBaseDeclarationSlots = `
  nvidia-support:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const nvidiaSupportConnectedPlugAppArmor = `
# This is inverse of
# https://forum.snapcraft.io/t/call-for-testing-chromium-62-0-3202-62/2569/46
# As nvidia-assemble snap needs to create the static & dynamic MAJOR
# chrdevs for all other snaps to have access to. Specifically
# /dev/nvidiactl /dev/nvidia-uvm
/{,usr/}bin/mknod ixr,
allow capability mknod,

/dev/nvidia[0-9]* rw,
/dev/nvidiactl rw,
/dev/nvidia-uvm rw,
/dev/nvidia-uvm-tools rw,
/dev/nvidia-modeset rw,

# To read dynamically allocated MAJOR for nvidia-uvm
@{PROC}/devices r,
`

const nvidiaSupportConnectedPlugSecComp = `
# This is inverse of
# https://forum.snapcraft.io/t/call-for-testing-chromium-62-0-3202-62/2569/46
# As nvidia-assemble snap needs to create the static & dynamic MAJOR
# chrdevs for all other snaps to have access to. Specifically
# /dev/nvidiactl /dev/nvidia-uvm
mknod - |S_IFCHR -
mknodat - - |S_IFCHR -
`

type nvidiaSupportInterface struct {
	commonInterface
}

func init() {
	registerIface(&nvidiaSupportInterface{commonInterface: commonInterface{
		name:                  "nvidia-support",
		summary:               nvidiaSupportSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationPlugs:  nvidiaSupportBaseDeclarationPlugs,
		baseDeclarationSlots:  nvidiaSupportBaseDeclarationSlots,
		connectedPlugAppArmor: nvidiaSupportConnectedPlugAppArmor,
		connectedPlugSecComp:  nvidiaSupportConnectedPlugSecComp,
	}})
}
