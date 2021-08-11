// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

const qrtrSummary = `allows access to the qrtr sockets`

const qrtrBaseDeclarationSlots = `
  qrtr:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const qrtrConnectedPlugAppArmor = `
# Description: Can use qipcrtr networking with sock_dgram only
network qipcrtr dgram,

# CAP_NET_ADMIN required for port number smaller QRTR_MIN_EPH_SOCKET per 'https://elixir.bootlin.com/linux/latest/source/net/qrtr/qrtr.c'
capability net_admin,
`

const qrtrConnectedPlugSecComp = `
# Description: Can use qipcrtr networking
bind

# We allow AF_QIPCRTR in the default template since it is mediated via the AppArmor rule
#socket AF_QIPCRTR
`

func init() {
	registerIface(&commonInterface{
		name:                  "qrtr",
		summary:               qrtrSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  qrtrBaseDeclarationSlots,
		connectedPlugAppArmor: qrtrConnectedPlugAppArmor,
		connectedPlugSecComp:  qrtrConnectedPlugSecComp,
	})
}
