// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

const canBusSummary = `allows access to the CAN bus`

const canBusBaseDeclarationSlots = `
  can-bus:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const canBusConnectedPlugAppArmor = `
# Description: Can use CAN networking
network can,
`

const canBusConnectedPlugSecComp = `
# Description: Can use CAN networking
bind

# We allow AF_CAN in the default template since it is mediated via the AppArmor rule
#socket AF_CAN
`

func init() {
	registerIface(&commonInterface{
		name:                  "can-bus",
		summary:               canBusSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  canBusBaseDeclarationSlots,
		connectedPlugAppArmor: canBusConnectedPlugAppArmor,
		connectedPlugSecComp:  canBusConnectedPlugSecComp,
	})
}
