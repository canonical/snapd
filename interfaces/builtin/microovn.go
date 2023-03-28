// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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

const microovnSummary = `allows access to the MicroOVN socket`

const microovnBaseDeclarationSlots = `
  microovn:
    allow-installation: false
    deny-connection: true
    deny-auto-connection: true
`

const microovnConnectedPlugAppArmor = `
# Description: allow access to the MicroOVN control socket.

/var/snap/microovn/common/state/control.socket rw,
`

const microovnConnectedPlugSecComp = `
# Description: allow access to the MicroOVN control socket.

socket AF_NETLINK - NETLINK_GENERIC
`

func init() {
	registerIface(&commonInterface{
		name:                  "microovn",
		summary:               microovnSummary,
		baseDeclarationSlots:  microovnBaseDeclarationSlots,
		connectedPlugAppArmor: microovnConnectedPlugAppArmor,
		connectedPlugSecComp:  microovnConnectedPlugSecComp,
	})
}
