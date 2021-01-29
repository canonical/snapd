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

// https://www.xilinx.com/support/documentation/ip_documentation/vcu/v1_2/pg252-vcu.pdf
// https://github.com/Xilinx/vcu-modules/
const allegroVcuSummary = `allows access to Xilinx Allegro Video Code Unit`

// Xilinx offers IP for their devices to decode/encode video streams, by
// using /dev/allegroDecodeIP and /dev/allegroIP devices.
// These operations should be considered privileged since the driver
// assumes trusted input, therefore require manual connection.
const allegroVcuBaseDeclarationSlots = `
  allegro-vcu:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const allegroVcuConnectedPlugAppArmor = `
# Description: Can access the Xilinx Allegro Video Core Unit, using a kernel
# module which directly controls hardware on the device.

/dev/allegroDecodeIP rw,
/dev/allegroIP rw,

/dev/dmaproxy rw,
`

var allegroVcuConnectedPlugUDev = []string{
	`SUBSYSTEM=="allegro_decode_class", KERNEL=="allegroDecodeIP"`,
	`SUBSYSTEM=="allegro_encode_class", KERNEL=="allegroIP"`,
	`SUBSYSTEM=="char", KERNEL=="dmaproxy"`,
}

func init() {
	registerIface(&commonInterface{
		name:                  "allegro-vcu",
		summary:               allegroVcuSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  allegroVcuBaseDeclarationSlots,
		connectedPlugAppArmor: allegroVcuConnectedPlugAppArmor,
		connectedPlugUDev:     allegroVcuConnectedPlugUDev,
	})
}
