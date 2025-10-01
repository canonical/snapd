// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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

const nvidiaDriversSupportSummary = `NVIDIA drivers userspace system setup support`

const nvidiaDriversSupportBaseDeclarationPlugs = `
  nvidia-drivers-support:
    allow-installation: false
    deny-auto-connection: true
`

const nvidiaDriversSupportBaseDeclarationSlots = `
  nvidia-drivers-support:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const nvidiaDriversSupportConnectedPlugAppArmor = `
# This is inverse of
# https://forum.snapcraft.io/t/call-for-testing-chromium-62-0-3202-62/2569/46
# As nvidia-assemble snap needs to create the static & dynamic MAJOR
# chrdevs for all other snaps to have access to. Specifically
# /dev/nvidiactl /dev/nvidia-uvm
# Support coreutils paths (LP: #2123870)
@{SNAP_COREUTIL_DIRS}mknod ixr,
allow capability mknod,

/dev/nvidia[0-9]* rw,
/dev/nvidiactl rw,
/dev/nvidia-uvm rw,
/dev/nvidia-uvm-tools rw,
/dev/nvidia-modeset rw,

# To read dynamically allocated MAJOR for nvidia-uvm
@{PROC}/devices r,
`

const nvidiaDriversSupportConnectedPlugSecComp = `
# This is inverse of
# https://forum.snapcraft.io/t/call-for-testing-chromium-62-0-3202-62/2569/46
# As nvidia-assemble snap needs to create the static & dynamic MAJOR
# chrdevs for all other snaps to have access to. Specifically
# /dev/nvidiactl /dev/nvidia-uvm
mknod - |S_IFCHR -
mknodat - - |S_IFCHR -
`

type nvidiaDriversSupportInterface struct {
	commonInterface
}

func init() {
	registerIface(&nvidiaDriversSupportInterface{commonInterface: commonInterface{
		name:                  "nvidia-drivers-support",
		summary:               nvidiaDriversSupportSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationPlugs:  nvidiaDriversSupportBaseDeclarationPlugs,
		baseDeclarationSlots:  nvidiaDriversSupportBaseDeclarationSlots,
		connectedPlugAppArmor: nvidiaDriversSupportConnectedPlugAppArmor,
		connectedPlugSecComp:  nvidiaDriversSupportConnectedPlugSecComp,
	}})
}
