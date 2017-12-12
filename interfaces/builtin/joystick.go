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

const joystickSummary = `allows access to joystick devices`

const joystickBaseDeclarationSlots = `
  joystick:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const joystickConnectedPlugAppArmor = `
# Description: Allow reading and writing to joystick devices (/dev/input/js*).

# Per https://github.com/torvalds/linux/blob/master/Documentation/admin-guide/devices.txt
# only js0-js31 is valid so limit the /dev and udev entries to those devices.
/dev/input/js{[0-9],[12][0-9],3[01]} rw,
/run/udev/data/c13:{[0-9],[12][0-9],3[01]} r,

# Allow reading for supported event reports for all input devices. See
# https://www.kernel.org/doc/Documentation/input/event-codes.txt
# FIXME: this is a very minor information leak and snapd should instead query
# udev for the specific accesses associated with the above devices.
/sys/devices/**/input[0-9]*/capabilities/* r,
`

var joystickConnectedPlugUDev = []string{`KERNEL=="js[0-9]*"`}

func init() {
	registerIface(&commonInterface{
		name:                  "joystick",
		summary:               joystickSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  joystickBaseDeclarationSlots,
		connectedPlugAppArmor: joystickConnectedPlugAppArmor,
		connectedPlugUDev:     joystickConnectedPlugUDev,
		reservedForOS:         true,
	})
}
