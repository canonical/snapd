// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

// https://www.xilinx.com/support/documentation/ip_documentation/xdma/v4_1/pg195-pcie-dma.pdf
// https://github.com/Xilinx/dma_ip_drivers/
const xilinxDmaSummary = `allows access to Xilinx DMA IP on a connected PCIe card`

const xilinxDmaBaseDeclarationPlugs = `
  xilinx-dma:
    allow-installation: false
    deny-auto-connection: true
`

const xilinxDmaBaseDeclarationSlots = `
  xilinx-dma:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

// For the most part, applications just need access to the device nodes. However, there
// are some important parameters exposed at /sys/modules/xdma/*
const xilinxDmaConnectedPlugAppArmor = `
# Access to the main device nodes created by the Xilinx XDMA driver
/dev/xdma[0-9]*_{c2h,h2c,events}_[0-9]* rw,
/dev/xdma[0-9]*_{control,user,xvc} rw,

# If multiple cards are detected, nodes are created under
/dev/xdma/card[0-9]*/** rw,

# View XDMA driver module parameters
/sys/module/xdma/parameters/* r,
`

// The xdma subsystem alone should serve as a unique identifier for all relevant devices
var xilinxDmaConnectedPlugUDev = []string{
	`SUBSYSTEM=="xdma"`,
}

func init() {
	registerIface(&commonInterface{
		name:                  "xilinx-dma",
		summary:               xilinxDmaSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationPlugs:  xilinxDmaBaseDeclarationPlugs,
		baseDeclarationSlots:  xilinxDmaBaseDeclarationSlots,
		connectedPlugAppArmor: xilinxDmaConnectedPlugAppArmor,
		connectedPlugUDev:     xilinxDmaConnectedPlugUDev,
	})
}
