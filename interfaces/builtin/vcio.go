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

// https://docs.broadcom.com/doc/12358545
// https://github.com/raspberrypi/linux/tree/rpi-5.4.y/drivers/char/broadcom
const vcioSummary = `allows access to VideoCore I/O`

// The raspberry pi allows programming its GPU from userspace via the /dev/vcio
// device. These operations should be considered privileged since the driver
// assumes trusted input, therefore require manual connection.
const vcioBaseDeclarationSlots = `
  vcio:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const vcioConnectedPlugAppArmor = `
# Description: Can access to vcio.

# vcio v1 access to ARM VideoCore (BCM_VCIO) for userspace GPU programming
/dev/vcio rw,
/sys/devices/virtual/bcm2708_vcio/vcio/** r,
# the vcio driver uses dynamic allocation for its major number and
# https://www.kernel.org/doc/Documentation/admin-guide/devices.txt lists
# 234-254 char as "RESERVED FOR DYNAMIC ASSIGNMENT".
/run/udev/data/c23[4-9]:[0-9]* r,
/run/udev/data/c24[0-9]:[0-9]* r,
/run/udev/data/c25[0-4]:[0-9]* r,
`

var vcioConnectedPlugUDev = []string{
	`SUBSYSTEM=="bcm2708_vcio", KERNEL=="vcio"`,
}

func init() {
	registerIface(&commonInterface{
		name:                  "vcio",
		summary:               vcioSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  vcioBaseDeclarationSlots,
		connectedPlugAppArmor: vcioConnectedPlugAppArmor,
		connectedPlugUDev:     vcioConnectedPlugUDev,
	})
}
