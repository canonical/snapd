// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

const multipassSummary = `allows access to the Multipass socket`

const multipassBaseDeclarationSlots = `
  multipass:
    allow-installation: false
    deny-connection: true
    deny-auto-connection: true
`

const multipassConnectedPlugSecComp = `
# Description: allow access to the Multipass daemon socket.
bind
`

func init() {
	registerIface(&commonInterface{
		name:                 "multipass",
		summary:              multipassSummary,
		baseDeclarationSlots: multipassBaseDeclarationSlots,
		connectedPlugSecComp: multipassConnectedPlugSecComp,
	})
}
