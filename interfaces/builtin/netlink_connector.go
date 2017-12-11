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

const netlinkConnectorSummary = `allows communication through the kernel netlink connector`

const netlinkConnectorBaseDeclarationSlots = `
  netlink-connector:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const netlinkConnectorConnectedPlugSecComp = `
# Description: Can use netlink to communicate with kernel connector. Because
# NETLINK_CONNECTOR is not finely mediated and app-specific, use of this
# interface allows communications via all netlink connectors.
# https://github.com/torvalds/linux/blob/master/Documentation/connector/connector.txt
bind
socket AF_NETLINK - NETLINK_CONNECTOR
`

const netlinkConnectorConnectedPlugAppArmor = `
# Description: Can use netlink to communicate with kernel connector. Because
# NETLINK_CONNECTOR is not finely mediated and app-specific, use of this
# interface allows communications via all netlink connectors.
# https://github.com/torvalds/linux/blob/master/Documentation/connector/connector.txt
network netlink,
# CAP_NET_ADMIN required per 'man 7 netlink'
capability net_admin,
`

func init() {
	registerIface(&commonInterface{
		name:                  "netlink-connector",
		summary:               netlinkConnectorSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  netlinkConnectorBaseDeclarationSlots,
		connectedPlugSecComp:  netlinkConnectorConnectedPlugSecComp,
		connectedPlugAppArmor: netlinkConnectorConnectedPlugAppArmor,
		reservedForOS:         true,
	})
}
