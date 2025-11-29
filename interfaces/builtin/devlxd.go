// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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

const devlxdSummary = `allows access to the LXD devlxd socket`

const devlxdBaseDeclarationPlugs = `
  devlxd:
    allow-installation: false
    deny-auto-connection: true
`

const devlxdBaseDeclarationSlots = `
  devlxd:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const devlxdConnectedPlugAppArmor = `
# Description: This gives access to the devlxd API for use by snaps running
# inside LXD instances (VMs and containers).

/dev/lxd/sock rw,
`

const devlxdConnectedPlugSecComp = `
# Description: This gives access to the devlxd API for use by snaps running
# inside LXD instances (VMs and containers).

socket AF_NETLINK - NETLINK_GENERIC
`

func init() {
	registerIface(&commonInterface{
		name:                  "devlxd",
		summary:               devlxdSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationPlugs:  devlxdBaseDeclarationPlugs,
		baseDeclarationSlots:  devlxdBaseDeclarationSlots,
		connectedPlugAppArmor: devlxdConnectedPlugAppArmor,
		connectedPlugSecComp:  devlxdConnectedPlugSecComp,
	})
}
