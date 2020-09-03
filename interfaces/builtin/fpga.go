// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

const fpgaSummary = `allows access to the FPGA subsystem`

const fpgaBaseDeclarationSlots = `
  fpga:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const fpgaConnectedPlugAppArmor = `
# Description: Can access fpga subsystem.

# Devices
/dev/fpga[0-9]* rw,

# /sys/class/fpga_* specified by the kernel docs
/sys/class/fpga_manager/fpga[0-9]*/{name,state,status} r,
/sys/class/fpga_region/region[0-9]*/compat_id r,
/sys/class/fpga_bridge/bridge[0-9]*/{name,state} r,

# Xilinx zynqmp FPGA
/sys/devices/platform/firmware:zynqmp-firmware/firmware:zynqmp-firmware:pcap/fpga_manager/fpga[0-9]*/{name,state,status} r,
/sys/devices/platform/firmware:zynqmp-firmware/firmware:zynqmp-firmware:pcap/fpga_manager/fpga[0-9]*/firmware w,
/sys/devices/platform/firmware:zynqmp-firmware/firmware:zynqmp-firmware:pcap/fpga_manager/fpga[0-9]*/{flags,key} rw,
/sys/devices/platform/fpga-full/fpga_region/region[0-9]*/compat_id r,

# Xilinx zynqmp module parameters
/sys/module/zynqmp_fpga/parameters/readback_type rw,

# configfs interface
/sys/kernel/config/device-tree/overlays/full{,1}/ rw,
/sys/kernel/config/device-tree/overlays/full{,1}/path rw,
`

func init() {
	registerIface(&commonInterface{
		name:                  "fpga",
		summary:               fpgaSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  fpgaBaseDeclarationSlots,
		connectedPlugAppArmor: fpgaConnectedPlugAppArmor,
	})
}
