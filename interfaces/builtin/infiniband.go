// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

const infinibandSummary = `allows direct access to InfiniBand and RDMA hardware`

const infinibandBaseDeclarationSlots = `
  infiniband:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const infinibandConnectedPlugAppArmor = `
# Description: Allows control and data plane access to InfiniBand/RDMA hardware devices,
# RDMA Communication Manager, and DPDK/DOCA hardware steering memory buffers.

# Character device nodes for verbs, management datagrams (mad), and communication manager
/dev/infiniband/* rw,

# Sysfs IOMMU group topology required for DMA mapping
/sys/kernel/iommu_groups/{,**} r,

# Mellanox/NVIDIA DOCA and DPDK hardware steering shared runtime files
/var/tmp/doca_mlx5_hws* rw,
/var/tmp/dpdk_net_mlx5_* rw,
`

const infinibandConnectedPlugSecComp = `
# RDMA netlink sockets
socket AF_NETLINK - NETLINK_RDMA
`

// Udev tagging triggers snapd cgroup BPF device filtering to allow character device access
var infinibandConnectedPlugUDev = []string{
	`SUBSYSTEM=="infiniband_verbs"`,
	`SUBSYSTEM=="infiniband_mad"`,
	`SUBSYSTEM=="rdma_cm"`,
	`SUBSYSTEM=="infiniband"`,
}

type infinibandInterface struct {
	commonInterface
}

func init() {
	registerIface(&infinibandInterface{
		commonInterface: commonInterface{
			name:                  "infiniband",
			summary:               infinibandSummary,
			implicitOnCore:        true,
			implicitOnClassic:     true,
			baseDeclarationSlots:  infinibandBaseDeclarationSlots,
			connectedPlugAppArmor: infinibandConnectedPlugAppArmor,
			connectedPlugSecComp:  infinibandConnectedPlugSecComp,
			connectedPlugUDev:     infinibandConnectedPlugUDev,
		},
	})
}
