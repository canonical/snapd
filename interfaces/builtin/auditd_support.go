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

const auditdSupportSummary = `allows hosting the auditd daemon with control over the kernel audit system`

const auditdSupportBaseDeclarationPlugs = `
  auditd-support:
    allow-installation: false
    deny-auto-connection: true
`

const auditdSupportBaseDeclarationSlots = `
  auditd-support:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const auditdSupportConnectedPlugSecComp = `
# Description: Can use netlink to communicate with kernel audit system.
bind
socket AF_NETLINK - NETLINK_AUDIT

setpriority
`

const auditdSupportConnectedPlugAppArmor = `
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

type auditdSupportInterface struct {
	commonInterface
}

func init() {
	registerIface(&auditdSupportInterface{commonInterface{
		name:                  "auditd-support",
		summary:               auditdSupportSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationPlugs:  auditdSupportBaseDeclarationPlugs,
		baseDeclarationSlots:  auditdSupportBaseDeclarationSlots,
		connectedPlugSecComp:  auditdSupportConnectedPlugSecComp,
		connectedPlugAppArmor: auditdSupportConnectedPlugAppArmor,
	}})
}
