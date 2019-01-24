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

const displayControlSummary = `allows configuring display parameters`

const displayControlBaseDeclarationSlots = `
  display-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const displayControlConnectedPlugAppArmor = `
# Description: This interface allows getting information about a connected
# display and setting parameters like backlight brightness.

/sys/class/backlight/ r,
/sys/devices/pci**/{backlight/acpi_video[0-9]*,intel_backlight}/{,**} r,
/sys/devices/pci**/{backlight/acpi_video[0-9]*,intel_backlight}/bl_power w,
/sys/devices/pci**/{backlight/acpi_video[0-9]*,intel_backlight}/brightness w,
`

func init() {
	registerIface(&commonInterface{
		name:                  "display-control",
		summary:               displayControlSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  displayControlBaseDeclarationSlots,
		connectedPlugAppArmor: displayControlConnectedPlugAppArmor,
		reservedForOS:         true,
	})
}
