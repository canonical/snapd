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

const hardwareRandomObserveSummary = `allows reading from hardware random number generator`

const hardwareRandomObserveBaseDeclarationSlots = `
  hardware-random-observe:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const hardwareRandomObserveConnectedPlugAppArmor = `
# Description: allow direct read-only access to the hardware random number
# generator device. In addition allow observing the available and
# currently-selected hardware random number generator devices.

/dev/hwrng r,
/run/udev/data/c10:183 r,
/sys/devices/virtual/misc/ r,
/sys/devices/virtual/misc/hw_random/rng_{available,current} r,
`

var hardwareRandomObserveConnectedPlugUDev = []string{`KERNEL=="hwrng"`}

func init() {
	registerIface(&commonInterface{
		name:                  "hardware-random-observe",
		summary:               hardwareRandomObserveSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  hardwareRandomObserveBaseDeclarationSlots,
		connectedPlugAppArmor: hardwareRandomObserveConnectedPlugAppArmor,
		connectedPlugUDev:     hardwareRandomObserveConnectedPlugUDev,
		reservedForOS:         true,
	})
}
