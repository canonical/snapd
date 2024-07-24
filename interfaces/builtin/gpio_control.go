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

// https://www.kernel.org/doc/Documentation/gpio/
const gpioControlSummary = `allows control of all aspects of GPIO pins`

// Controlling all aspects of GPIO pins can potentially impact other snaps and
// grant wide access to specific hardware and the system, so treat as
// super-privileged
const gpioControlBaseDeclarationPlugs = `
  gpio-control:
    allow-installation: false
    deny-auto-connection: true
`
const gpioControlBaseDeclarationSlots = `
  gpio-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const gpioControlConnectedPlugAppArmor = `
# Description: Allow controlling all aspects of GPIO pins. This can potentially
# impact the system and other snaps, and allows privileged access to hardware.

# Access through legacy sysfs interface
/sys/class/gpio/{,un}export rw,
/sys/class/gpio/gpio[0-9]*/{active_low,direction,value,edge} rw,
# apparmor needs the symlink targets which on most platforms the path
# below (see also PR#12816)
/sys/devices/platform/**/gpio/gpio[0-9]*/{active_low,direction,value,edge} rw,

# Access through gpiod interface
# see https://www.kernel.org/doc/html/latest/userspace-api/gpio/chardev.html
/dev/gpiochip[0-9]* rw,
`

var gpioControlConnectedPlugUDev = []string{
	`SUBSYSTEM=="gpio", KERNEL=="gpiochip[0-9]*"`,
}

func init() {
	registerIface(&commonInterface{
		name:                  "gpio-control",
		summary:               gpioControlSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationPlugs:  gpioControlBaseDeclarationPlugs,
		baseDeclarationSlots:  gpioControlBaseDeclarationSlots,
		connectedPlugAppArmor: gpioControlConnectedPlugAppArmor,
		connectedPlugUDev:     gpioControlConnectedPlugUDev,
	})
}
