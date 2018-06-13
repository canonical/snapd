// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

const dockerSummary = `allows access to Docker socket`

const dockerBaseDeclarationSlots = `
  docker:
    allow-installation: false
    deny-connection: true
    deny-auto-connection: true
`

const dockerConnectedPlugAppArmor = `
# Description: allow access to the Docker daemon socket. This gives privileged
# access to the system via Docker's socket API.

# Allow talking to the docker daemon
/{,var/}run/docker.sock rw,
`

const dockerConnectedPlugSecComp = `
# Description: allow access to the Docker daemon socket. This gives privileged
# access to the system via Docker's socket API.

bind
socket AF_NETLINK - NETLINK_GENERIC
`

func init() {
	registerIface(&commonInterface{
		name:                  "docker",
		summary:               dockerSummary,
		baseDeclarationSlots:  dockerBaseDeclarationSlots,
		connectedPlugAppArmor: dockerConnectedPlugAppArmor,
		connectedPlugSecComp:  dockerConnectedPlugSecComp,
	})
}
