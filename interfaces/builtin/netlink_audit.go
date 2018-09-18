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

const netlinkAuditSummary = `allows access to kernel audit system through netlink`

const netlinkAuditBaseDeclarationSlots = `
  netlink-audit:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const netlinkAuditConnectedPlugSecComp = `
# Description: Can use netlink to read/write to kernel audit system.
bind
socket AF_NETLINK - NETLINK_AUDIT
`

const netlinkAuditConnectedPlugAppArmor = `
# Description: Can use netlink to read/write to kernel audit system.
network netlink,

# CAP_NET_ADMIN required for multicast netlink sockets per 'man 7 netlink'
capability net_admin,

# CAP_AUDIT_READ required to read the audit log via the netlink multicast socket
# per 'man 7 capabilities'
capability audit_read,

# CAP_AUDIT_WRITE required to write to the audit log via the netlink multicast
# socket per 'man 7 capabilities'
capability audit_write,
`

func init() {
	registerIface(&commonInterface{
		name:                  "netlink-audit",
		summary:               netlinkAuditSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  netlinkAuditBaseDeclarationSlots,
		connectedPlugSecComp:  netlinkAuditConnectedPlugSecComp,
		connectedPlugAppArmor: netlinkAuditConnectedPlugAppArmor,
		reservedForOS:         true,
	})
}
