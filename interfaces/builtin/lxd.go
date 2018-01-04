// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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

const lxdSummary = `allows access to the LXD socket`

const lxdBaseDeclarationSlots = `
  lxd:
    allow-installation: false
    deny-connection: true
    deny-auto-connection: true
`

const lxdConnectedPlugAppArmor = `
# Description: allow access to the LXD daemon socket. This gives privileged
# access to the system via LXD's socket API.

/var/snap/lxd/common/lxd/unix.socket rw,
`

const lxdConnectedPlugSecComp = `
# Description: allow access to the LXD daemon socket. This gives privileged
# access to the system via LXD's socket API.

socket AF_NETLINK - NETLINK_GENERIC
`

func init() {
	registerIface(&commonInterface{
		name:                  "lxd",
		summary:               lxdSummary,
		baseDeclarationSlots:  lxdBaseDeclarationSlots,
		connectedPlugAppArmor: lxdConnectedPlugAppArmor,
		connectedPlugSecComp:  lxdConnectedPlugSecComp,
	})
}
