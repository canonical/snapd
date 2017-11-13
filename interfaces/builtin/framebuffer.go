// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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

const framebufferSummary = `allows access to universal framebuffer devices`

const framebufferBaseDeclarationSlots = `
  framebuffer:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const framebufferConnectedPlugAppArmor = `
# Description: Allow reading and writing to the universal framebuffer (/dev/fb*) which
# gives privileged access to the console framebuffer.

/dev/fb[0-9]* rw,
/run/udev/data/c29:[0-9]* r,
`

var framebufferConnectedPlugUDev = []string{`KERNEL=="fb[0-9]*"`}

func init() {
	registerIface(&commonInterface{
		name:                  "framebuffer",
		summary:               framebufferSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  framebufferBaseDeclarationSlots,
		connectedPlugAppArmor: framebufferConnectedPlugAppArmor,
		connectedPlugUDev:     framebufferConnectedPlugUDev,
		reservedForOS:         true,
	})
}
