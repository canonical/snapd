// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

const snapThemesControlSummary = `allows use of snapd's theme installation API`

const snapThemesControlBaseDeclarationPlugs = `
  snap-themes-control:
    allow-installation: false
    deny-auto-connection: true
`

const snapThemesControlBaseDeclarationSlots = `
  snap-themes-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

func init() {
	registerIface(&commonInterface{
		name:                 "snap-themes-control",
		summary:              snapThemesControlSummary,
		implicitOnCore:       true,
		implicitOnClassic:    true,
		baseDeclarationPlugs: snapThemesControlBaseDeclarationPlugs,
		baseDeclarationSlots: snapThemesControlBaseDeclarationSlots,
	})
}
