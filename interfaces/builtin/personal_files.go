// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

const personalFilesSummary = `allows access to personal files or directories`

const personalFilesBaseDeclarationSlots = `
  personal-files:
    allow-installation:
      slot-snap-type:
        - core
    deny-connection: true
    deny-auto-connection: true
`

const personalFilesConnectedPlugAppArmor = `
# Description: Can access specific personal files or directories.
# This is restricted because it gives file access to arbitrary locations.
`

type personalFilesInterface struct {
	systemFilesInterface
}

func init() {
	registerIface(&personalFilesInterface{systemFilesInterface{commonInterface{
		name:                 "personal-files",
		summary:              personalFilesSummary,
		implicitOnCore:       true,
		implicitOnClassic:    true,
		baseDeclarationSlots: personalFilesBaseDeclarationSlots,
		reservedForOS:        true,
	}}})
}
