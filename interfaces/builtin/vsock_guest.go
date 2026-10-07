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

const vsockGuestSummary = `allows access to vsock sockets for VM guest to host/hypervisor communication`

const vsockGuestBaseDeclarationSlots = `
  vsock-guest:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const vsockGuestConnectedPlugAppArmor = `
# Description: Allow access to vsock sockets for communication between
# a VM guest and the host or hypervisor (AF_VSOCK).
network vsock,
/dev/vsock rw,
`

const vsockGuestConnectedPlugSecComp = `
# Description: Allow access to vsock sockets for VM guest to host communication.
# socket AF_VSOCK is already permitted by the default seccomp template.
bind
listen
accept
accept4
`

var vsockGuestConnectedPlugUDev = []string{
	`KERNEL=="vsock"`,
}

func init() {
	registerIface(&commonInterface{
		name:                  "vsock-guest",
		summary:               vsockGuestSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  vsockGuestBaseDeclarationSlots,
		connectedPlugAppArmor: vsockGuestConnectedPlugAppArmor,
		connectedPlugSecComp:  vsockGuestConnectedPlugSecComp,
		connectedPlugUDev:     vsockGuestConnectedPlugUDev,
	})
}
