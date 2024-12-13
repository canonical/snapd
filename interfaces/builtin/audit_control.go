// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

const auditControlSummary = `allows control over the kernel audit system`

const auditControlBaseDeclarationSlots = `
  audit-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const auditControlConnectedPlugSecComp = `
# Description: Can use netlink to communicate with kernel audit system.
bind
socket AF_NETLINK - NETLINK_AUDIT
`

const auditControlConnectedPlugAppArmor = `
# Description: Can use netlink to communicate with kernel audit system.
network netlink,

# CAP_AUDIT_CONTROL required to enable/disable kernel auditing, change auditing
# filter rules, and retrieve auditing status and filtering rules.
capability audit_control,

# Allow reading the login UID and session ID of processes.
@{PROC}/*/{loginuid,sessionid} r,

# Allow writing /run/auditd.pid and /run/auditd.state, as required by auditd.
/{,var/}run/auditd.{pid,state} rw,

# Allow adjusting the OOM score of the application.
@{PROC}/@{pid}/oom_score_adj rw,
`

type auditControlInterface struct {
	commonInterface
}

func init() {
	registerIface(&auditControlInterface{commonInterface{
		name:                  "audit-control",
		summary:               auditControlSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  auditControlBaseDeclarationSlots,
		connectedPlugSecComp:  auditControlConnectedPlugSecComp,
		connectedPlugAppArmor: auditControlConnectedPlugAppArmor,
	}})
}
