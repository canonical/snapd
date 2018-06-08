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

const socketCanControlSummary = `allows configuration and use of SocketCAN network interfaces`

const socketCanControlBaseDeclarationSlots = `
  socketcan-control:
    allow-installation:
      slot-snap-type:
		- core
	deny-auto-connection: true
`

const socketCanControlConnectedPlugAppArmor = `
# Description: Can configure and use SocketCAN networks
network can,

# Allow configuration of the interface using the ip command for SocketCAN
/{,usr/}{,s}bin/ip ixr,

# required by ip to configure SocketCAN networks
capability net_admin,
network netlink raw,
`

const socketCanControlConnectedPlugSecComp = `
# Description: Can access SocketCAN networks.
bind

# required by ip to configure SocketCAN networks
socket AF_NETLINK - NETLINK_ROUTE
socket AF_NETLINK - NETLINK_GENERIC
`

func init() {
	registerIface(&commonInterface{
		name:                  "socketcan-control",
		summary:               socketCanControlSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  socketCanControlBaseDeclarationSlots,
		connectedPlugAppArmor: socketCanControlConnectedPlugAppArmor,
		connectedPlugSecComp:  socketCanControlConnectedPlugSecComp,
		reservedForOS:         true,
	})
}
