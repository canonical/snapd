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

const nvidiaTegraDriversSupportSummary = `allows hardware access to NVIDIA tegra platforms`

const nvidiaTegraDriversSupportBaseDeclarationSlots = `
  nvidia-tegra-drivers-support:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const nvidiaTegraDriversSupportConnectedPlugAppArmor = `
@{PROC}/sys/vm/mmap_min_addr r,

# required to read chip information
/sys/devices/soc0/platform r,
/sys/devices/soc0/soc_id r,
/sys/devices/soc0/revision r,
/sys/devices/soc0/major r,

# nvidia-smi is trying to access /sys/bus/nvmem/devices/fuse/nvmem
# but doesn't have permissions to the link location which is listed here
/sys/devices/platform/bus@0/3810000.fuse/fuse/nvmem r,

/dev/nvmap rw,
/dev/dri/renderD128 rw,
/dev/nvgpu/igpu0/power rw,
/dev/nvgpu/igpu0/ctrl rw,
/dev/host1x-fence rw,

# tries to create shared memory slab with mknod in IPC cuda apps
/dev/shm/memmap_ipc_shm rw,
`

var nvidiaTegraDriversSupportConnectedPlugUdev = []string{
    // Nvidia dma barrier
	`SUBSYSTEM=="host1x-fence" OWNER="root" GROUP="video" MODE="0660"`,

    // Tegra memory manager
	`KERNEL=="nvmap" OWNER="root" GROUP="video" MODE="0660"`,

    //iGPU device nodes
	`SUBSYSTEM=="nvidia-gpu-v2-power" KERNEL=="power" OWNER="root" GROUP="video" MODE="0660"`,
	`SUBSYSTEM=="nvidia-gpu-v2" KERNEL=="as" OWNER="root" GROUP="video" MODE="0660"`,
	`SUBSYSTEM=="nvidia-gpu-v2" KERNEL=="channel" OWNER="root" GROUP="video" MODE="0660"`,
	`SUBSYSTEM=="nvidia-gpu-v2" KERNEL=="ctrl" OWNER="root" GROUP="video" MODE="0660"`,
	`SUBSYSTEM=="nvidia-gpu-v2" KERNEL=="sched" OWNER="root" GROUP="root" MODE="0660"`,
	`SUBSYSTEM=="nvidia-gpu-v2" KERNEL=="nvsched" OWNER="root" GROUP="video" MODE="0640"`,

    //render device node
	`SUBSYSTEM=="drm" KERNEL=="renderD128" OWNER="root" GROUP="render" MODE="0660"`,
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
