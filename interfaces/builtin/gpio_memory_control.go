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

// https://github.com/raspberrypi/linux/blob/rpi-4.4.y/drivers/char/broadcom/bcm2835-gpiomem.c
const gpioMemoryControlSummary = `allows write access to all gpio memory`

const gpioMemoryControlBaseDeclarationSlots = `
  gpio-memory-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const gpioMemoryControlConnectedPlugAppArmor = `
# Description: Allow writing to /dev/gpiomem on kernels that provide it (eg,
# via the bcm2835-gpiomem kernel module). This allows direct access to the
# physical memory for GPIO devices (i.e. a subset of /dev/mem) and therefore
# grants access to all GPIO devices on the system.
/dev/gpiomem rw,
`

var gpioMemoryControlConnectedPlugUDev = []string{`KERNEL=="gpiomem"`}

func init() {
	registerIface(&commonInterface{
		name:                  "gpio-memory-control",
		summary:               gpioMemoryControlSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  gpioMemoryControlBaseDeclarationSlots,
		connectedPlugAppArmor: gpioMemoryControlConnectedPlugAppArmor,
		connectedPlugUDev:     gpioMemoryControlConnectedPlugUDev,
		reservedForOS:         true,
	})
}
