// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

const vsockSummary = `allows access to vsock sockets for VM/container host communication`

const vsockBaseDeclarationPlugs = `
  vsock:
    allow-installation: false
    deny-auto-connection: true
`

const vsockBaseDeclarationSlots = `
  vsock:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const vsockConnectedPlugAppArmor = `
# Description: Allow access to vsock sockets for communication between
# VM guests and the host or hypervisor (AF_VSOCK).
network vsock,
/dev/vsock rw,
/dev/vmci rw,
`

const vsockConnectedPlugSecComp = `
# Description: Allow access to vsock sockets for VM/container host communication.
# socket AF_VSOCK is already permitted by the default seccomp template.
bind
listen
accept
accept4
`

var vsockConnectedPlugUDev = []string{
	`KERNEL=="vsock"`,
	`KERNEL=="vmci"`,
}

func init() {
	registerIface(&commonInterface{
		name:                  "vsock",
		summary:               vsockSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationPlugs:  vsockBaseDeclarationPlugs,
		baseDeclarationSlots:  vsockBaseDeclarationSlots,
		connectedPlugAppArmor: vsockConnectedPlugAppArmor,
		connectedPlugSecComp:  vsockConnectedPlugSecComp,
		connectedPlugUDev:     vsockConnectedPlugUDev,
	})
}
