// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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

const firmwareUpdaterSupportSummary = `allows operating as the Firmware Updater`

const firmwareUpdaterSupportBaseDeclarationPlugs = `
  firmware-updater-support:
    allow-installation: false
    deny-auto-connection: true
`

const firmwareUpdaterSupportBaseDeclarationSlots = `
  firmware-updater-support:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

func init() {
	registerIface(&commonInterface{
		name:                 "firmware-updater-support",
		summary:              firmwareUpdaterSupportSummary,
		implicitOnCore:       true,
		implicitOnClassic:    true,
		baseDeclarationPlugs: firmwareUpdaterSupportBaseDeclarationPlugs,
		baseDeclarationSlots: firmwareUpdaterSupportBaseDeclarationSlots,
	})
}
