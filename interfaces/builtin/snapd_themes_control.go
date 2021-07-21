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

const snapdThemesControlSummary = `allows use of snapd's theme installation API`

const snapdThemesControlBaseDeclarationPlugs = `
  snapd-themes-control:
    allow-installation: false
    deny-auto-connection: true
`

const snapdThemesControlBaseDeclarationSlots = `
  snapd-themes-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

type snapThemesControlInterface struct {
	commonInterface
}

func init() {
	registerIface(&snapThemesControlInterface{commonInterface{
		name:                 "snapd-themes-control",
		summary:              snapdThemesControlSummary,
		implicitOnCore:       true,
		implicitOnClassic:    true,
		baseDeclarationPlugs: snapdThemesControlBaseDeclarationPlugs,
		baseDeclarationSlots: snapdThemesControlBaseDeclarationSlots,
	}})
}
