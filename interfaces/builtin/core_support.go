// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2018 Canonical Ltd
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

const coreSupportSummary = `formerly special permissions for the core snap`

const coreSupportBaseDeclarationPlugs = `
  core-support:
    allow-installation:
      plug-snap-type:
        - core
`

const coreSupportBaseDeclarationSlots = `
  core-support:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

// This interface is deprecated and doesn't grant any permissions but for the
// moment we chose not to remove it in case something tests for its presence.
// This hollow interface should be removed once it is deemed safe to do so.
func init() {
	registerIface(&commonInterface{
		name:                 "core-support",
		summary:              coreSupportSummary,
		implicitOnCore:       true,
		implicitOnClassic:    true,
		baseDeclarationPlugs: coreSupportBaseDeclarationPlugs,
		baseDeclarationSlots: coreSupportBaseDeclarationSlots,
		reservedForOS:        true,
	})
}
