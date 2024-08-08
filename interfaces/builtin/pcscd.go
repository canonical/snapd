// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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

const pcscdSummary = `allows interacting with PCSD daemon (e.g. for the PS/SC API library).`

const pcscdBaseDeclarationSlots = `
  pcscd:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const pcscdConnectedPlugAppArmor = `
# Socket for communication between PCSCD and PS/SC API library
/{var/,}run/pcscd/pcscd.comm rw,
`

func init() {
	registerIface(&commonInterface{
		name:                  "pcscd",
		summary:               pcscdSummary,
		implicitOnClassic:     true,
		baseDeclarationSlots:  pcscdBaseDeclarationSlots,
		connectedPlugAppArmor: pcscdConnectedPlugAppArmor,
	})
}
