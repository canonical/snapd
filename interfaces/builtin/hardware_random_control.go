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

const hardwareRandomControlSummary = `allows control over the hardware random number generator`

const hardwareRandomControlBaseDeclarationSlots = `
  hardware-random-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const hardwareRandomControlConnectedPlugAppArmor = `
# Description: allow direct access to the hardware random number generator
# device. Usually, the default access to /dev/random is sufficient, but this
# allows applications such as rng-tools to use /dev/hwrng directly or change
# the hwrng via sysfs. For details, see
# https://www.kernel.org/doc/Documentation/hw_random.txt

/dev/hwrng rw,
/run/udev/data/c10:183 r,
/sys/devices/virtual/misc/ r,
/sys/devices/virtual/misc/hw_random/rng_{available,current} r,

# Allow changing the hwrng
/sys/devices/virtual/misc/hw_random/rng_current w,
`

var hardwareRandomControlConnectedPlugUDev = []string{`KERNEL=="hwrng"`}

func init() {
	registerIface(&commonInterface{
		name:                  "hardware-random-control",
		summary:               hardwareRandomControlSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  hardwareRandomControlBaseDeclarationSlots,
		connectedPlugAppArmor: hardwareRandomControlConnectedPlugAppArmor,
		connectedPlugUDev:     hardwareRandomControlConnectedPlugUDev,
		reservedForOS:         true,
	})
}
