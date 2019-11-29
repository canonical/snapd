// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

const dpdkControlSummary = `allows using dpdk`

const dpdkControlBaseDeclarationSlots = `
  dpdk-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const dpdkControlConnectedPlugAppArmor = `
# Description: Allow control to dpdk.
/run/dpdk{,/**} rwk,
owner @{PROC}/@{pid}/pagemap r,
/sys/bus/pci/drivers/virtio-pci/unbind rw,
/sys/bus/pci/drivers/e1000/unbind rw,
/sys/bus/pci/drivers/igb_uio/unbind rw,
/sys/bus/pci/drivers/rte_kni/unbind rw,
/sys/bus/pci/drivers/uio_pci_generic/unbind rw,
/sys/bus/pci/drivers/virtio-pci/bind rw,
/sys/bus/pci/drivers/e1000/bind rw,
/sys/bus/pci/drivers/igb_uio/bind rw,
/sys/bus/pci/drivers/rte_kni/bind rw,
/sys/bus/pci/drivers/uio_pci_generic/bind rw,
/sys/devices/pci*{,/**} rw,
/proc/ioports r,
/dev/uio[0-9]* rw,
/dev/vfio/vfio rw,
/dev/infiniband/rdma_cm rw,
/dev/infiniband/uverbs[0-9]* rw,
/sys/bus/pci/drivers{,/**} rw,
`

const dpdkControlConnectedPlugSecComp = `
# Description: Controls DPDK SecComp.
move_pages
`

// Network drivers with DPDK support used by different vendors.
var dpdkConnectedPlugKmod = []string{
	"mlx4_core",
	"mlx5_core",
}

var dpdkControlConnectedPlugUDev = []string{`SUBSYSTEM=="uio"`}

func init() {
	registerIface(&commonInterface{
		name:                     "dpdk-control",
		summary:                  dpdkControlSummary,
		implicitOnCore:           true,
		implicitOnClassic:        true,
		baseDeclarationSlots:     dpdkControlBaseDeclarationSlots,
		connectedPlugAppArmor:    dpdkControlConnectedPlugAppArmor,
		connectedPlugSecComp:     dpdkControlConnectedPlugSecComp,
		connectedPlugKModModules: dpdkConnectedPlugKmod,
		connectedPlugUDev:        dpdkControlConnectedPlugUDev,
	})
}
