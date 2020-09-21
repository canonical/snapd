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

const usbmuxdSummary = `allows access to usbmuxd socket`

const usbmuxdBaseDeclarationSlots = `
  usbmuxd:
    allow-installation: false
    deny-connection: true
    deny-auto-connection: true
`

const usbmuxdConnectedPlugAppArmor = `
# Description: allow access to the usbmuxd daemon socket. This gives access
# to iOS devices connected to the system via usbmuxd's socket API.

# Allow talking to the usbmuxd daemon
/{,var/}run/usbmuxd.sock rw,
`

const usbmuxdConnectedPlugSecComp = `
# Description: allow access to the usbmuxd daemon socket. This gives access
# to iOS devices connected to the system via usbmuxd's socket API.

bind
socket AF_NETLINK - NETLINK_GENERIC
`

func init() {
	registerIface(&commonInterface{
		name:                  "usbmuxd",
		summary:               usbmuxdSummary,
		baseDeclarationSlots:  usbmuxdBaseDeclarationSlots,
		connectedPlugAppArmor: usbmuxdConnectedPlugAppArmor,
		connectedPlugSecComp:  usbmuxdConnectedPlugSecComp,
	})
}
