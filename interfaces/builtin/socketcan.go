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

const socketCanSummary = `allows configuration and use of SocketCAN network interfaces`

const socketCanBaseDeclarationSlots = `
  socketcan:
    allow-installation:
      slot-snap-type:
        - core
`

const socketCanConnectedPlugAppArmor = `
# Description: Can access SocketCAN networks.
network can,
network netlink raw,

capability net_admin,

# Allow configuration of the interface using ip
/{,usr/}{,s}bin/ip ixr,
`

const socketCanConnectedPlugSecComp = `
# Description: Can access SocketCAN networks.
bind

socket AF_NETLINK - NETLINK_ROUTE
socket AF_NETLINK - NETLINK_GENERIC
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
