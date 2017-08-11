// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

const waylandSummary = `allows access to compositors supporting wayland protocol`

const waylandBaseDeclarationSlots = `
  wayland:
    allow-installation:
      slot-snap-type:
        - core
`

const waylandConnectedPlugAppArmor = `
# Description: Can access compositors supporting the wayland protocol

# Allow access to the wayland compsitor server socket
owner /run/user/*/wayland-[0-9]* rw,
`

func init() {
	registerIface(&commonInterface{
		name:                  "wayland",
		summary:               waylandSummary,
		implicitOnClassic:     true,
		baseDeclarationSlots:  waylandBaseDeclarationSlots,
		connectedPlugAppArmor: waylandConnectedPlugAppArmor,
		reservedForOS:         true,
	})
}
