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

const podmanSummary = `allows access to Podman socket`

const podmanBaseDeclarationSlots = `
  podman:
    allow-installation:
      slot-snap-type:
        - core
    deny-connection: true
    deny-auto-connection: true
`

const podmanConnectedPlugAppArmor = `
# Description: allow access to the Podman service. This gives privileged
# access to the system via Podman's socket API.

# Allow talking to the Podman service (system socket)
/{,var/}run/podman/podman.sock rw,
# Allow talking to the Podman service (rootless/user socket, $XDG_RUNTIME_DIR)
owner /{,var/}run/user/[0-9]*/podman/podman.sock rw,
`

const podmanConnectedPlugSecComp = `
# Description: allow access to the Podman service. This gives privileged
# access to the system via Podman's socket API.

bind
socket AF_NETLINK - NETLINK_GENERIC
`

func init() {
	registerIface(&commonInterface{
		name:                  "podman",
		summary:               podmanSummary,
		implicitOnClassic:     true,
		baseDeclarationSlots:  podmanBaseDeclarationSlots,
		connectedPlugAppArmor: podmanConnectedPlugAppArmor,
		connectedPlugSecComp:  podmanConnectedPlugSecComp,
	})
}
