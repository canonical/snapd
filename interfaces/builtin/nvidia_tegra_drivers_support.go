// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

const nvidiaTegraDriversSupportSummary = `allows iGPU access to NVIDIA Tegra platforms`

const nvidiaTegraDriversSupportBaseDeclarationSlots = `
  nvidia-tegra-drivers-support:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const nvidiaTegraDriversSupportConnectedPlugAppArmor = `
@{PROC}/sys/vm/mmap_min_addr r,

# nvidia-smi is trying to access /sys/bus/nvmem/devices/fuse/nvmem
# but doesn't have permissions to the link location which is listed here
/sys/devices/platform/bus@*/*.fuse/fuse/nvmem r,

/dev/nvmap rw,
/dev/dri/renderD[0-9]* rw,
/dev/nvgpu/igpu[0-9]/power rw,
/dev/nvgpu/igpu[0-9]/ctrl rw,
/dev/host1x-fence rw,

# tries to create shared memory slab with mknod in IPC cuda apps
/dev/shm/memmap_ipc_shm rw,
`

var nvidiaTegraDriversSupportConnectedPlugUdev = []string{
    // Nvidia dma barrier
    `SUBSYSTEM=="host1x-fence"`,

    // Tegra memory manager
    `KERNEL=="nvmap"`,

    //iGPU device nodes
    `SUBSYSTEM=="nvidia-gpu-v2-power" KERNEL=="power"`,
    `SUBSYSTEM=="nvidia-gpu-v2" KERNEL=="as"`,
    `SUBSYSTEM=="nvidia-gpu-v2" KERNEL=="channel"`,
    `SUBSYSTEM=="nvidia-gpu-v2" KERNEL=="ctrl"`,
    `SUBSYSTEM=="nvidia-gpu-v2" KERNEL=="sched"`,
    `SUBSYSTEM=="nvidia-gpu-v2" KERNEL=="nvsched"`,

    //render device node
    `SUBSYSTEM=="drm" KERNEL=="renderD[0-9]*"`,
}

const nvidiaTegraDriversSupportConnectedPlugSecComp = `
# tries to bind to socket for IPC
bind
mknod - |S_IFCHR -
mknodat - - |S_IFCHR -
`

type nvidiaTegraDriversSupportInterface struct {
	commonInterface
}

func init() {
	registerIface(&nvidiaTegraDriversSupportInterface{commonInterface: commonInterface{
		name:                  "nvidia-tegra-drivers-support",
		summary:               nvidiaTegraDriversSupportSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  nvidiaTegraDriversSupportBaseDeclarationSlots,
		connectedPlugAppArmor: nvidiaTegraDriversSupportConnectedPlugAppArmor,
		connectedPlugUDev:     nvidiaTegraDriversSupportConnectedPlugUdev,
		connectedPlugSecComp:  nvidiaTegraDriversSupportConnectedPlugSecComp,
	}})
}
