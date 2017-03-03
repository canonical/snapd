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

import (
	"github.com/snapcore/snapd/interfaces"
)

const netlinkConnectorConnectedPlugSecComp = `
# Description: Can use netlink to communicate with kernel connector. Because
# NETLINK_CONNECTOR is not finely mediated and app-specific, use of this
# interface allows communications via all netlink connectors.
# https://github.com/torvalds/linux/blob/master/Documentation/connector/connector.txt
bind
socket AF_NETLINK - NETLINK_CONNECTOR
`

// NewNetlinkConnectorInterface returns a new "netlink-connector" interface.
func NewNetlinkConnectorInterface() interfaces.Interface {
	return &commonInterface{
		name:                 "netlink-connector",
		connectedPlugSecComp: netlinkConnectorConnectedPlugSecComp,
		reservedForOS:        true,
	}
}
