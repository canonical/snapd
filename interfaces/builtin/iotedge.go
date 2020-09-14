// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

const iotedgeSummary = `allows Azure iotedge daemon and client`

const iotedgeBaseDeclarationSlots = `
  azure-iotedge:
    allow-installation: false
    deny-connection: true
    deny-auto-connection: true
`

const iotedgeConnectedPlugAppArmor = `
# Description: allow access to the iotedge daemon socket and
# tasks to manage containers. This gives privileged access to the system
# via iotedge's socket API to iotedged, which then talks to dockerd

# Description: control and tracing of dockerd containers
@{PROC}/[0-9]*/environ r,
ptrace (read),
capability sys_ptrace,
capability dac_read_search,

# Allow iotedge daemon sockets
/{,var/}run/iotedge/mgmt.sock rw,
/{,var/}run/iotedge/workload.sock rw,

`

func init() {
	registerIface(&commonInterface{
		name:                  "iotedge",
		summary:               iotedgeSummary,
		baseDeclarationSlots:  iotedgeBaseDeclarationSlots,
		connectedPlugAppArmor: iotedgeConnectedPlugAppArmor,
	})
}
