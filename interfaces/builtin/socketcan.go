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

const socketCanSummary = `allows use of SocketCAN network interfaces`

const socketCanBaseDeclarationSlots = `
  socketcan:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const socketCanConnectedPlugAppArmor = `
# Description: Can use SocketCAN networking
network can,
`

const socketCanConnectedPlugSecComp = `
# Description: Can use SocketCAN networking
bind
`

func init() {
	registerIface(&commonInterface{
		name:                  "socketcan",
		summary:               socketCanSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  socketCanBaseDeclarationSlots,
		connectedPlugAppArmor: socketCanConnectedPlugAppArmor,
		connectedPlugSecComp:  socketCanConnectedPlugSecComp,
		reservedForOS:         true,
	})
}
