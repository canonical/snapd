// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

const snapRefreshObserveSummary = `allows read access to track snap refreshes and their inhibition`

const snapRefreshObserveBaseDeclarationPlugs = `
  snap-refresh-observe:
    allow-installation: false
    deny-auto-connection: true
`

const snapRefreshObserveBaseDeclarationSlots = `
  snap-refresh-observe:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

func init() {
	registerIface(&commonInterface{
		name:                 "snap-refresh-observe",
		summary:              snapRefreshObserveSummary,
		implicitOnCore:       true,
		implicitOnClassic:    true,
		baseDeclarationPlugs: snapRefreshObserveBaseDeclarationPlugs,
		baseDeclarationSlots: snapRefreshObserveBaseDeclarationSlots,
	})
}
