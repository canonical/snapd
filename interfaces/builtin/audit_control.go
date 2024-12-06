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

const auditControlConnectedPlugAppArmor = `
# CAP_AUDIT_CONTROL required to enable/disable kernel auditing, change auditing
# filter rules, and retrieve auditing status and filtering rules.
capability audit_control,

# Allow reading /proc/self/{loginuid,sessionid}
@{PROC}/@{pid}/{loginuid,sessionid} r,

# Allow writing /run/auditd.pid so the auditd pid is known by other programs.
/{,var/}run/auditd.pid rw,
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
		connectedPlugAppArmor: auditControlConnectedPlugAppArmor,
	}})
}
