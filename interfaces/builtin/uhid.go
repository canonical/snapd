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

const uhidSummary = `allows control over UHID devices`

const uhidBaseDeclarationSlots = `
  uhid:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const uhidConnectedPlugAppArmor = `
# Description: Allows accessing the UHID to create kernel
# hid devices from user-space.

  # Requires CONFIG_UHID
  /dev/uhid rw,
`

// Note: uhid is not represented in sysfs so it cannot be udev tagged

func init() {
	registerIface(&commonInterface{
		name:                  "uhid",
		summary:               uhidSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  uhidBaseDeclarationSlots,
		connectedPlugAppArmor: uhidConnectedPlugAppArmor,
		reservedForOS:         true,
	})
}
