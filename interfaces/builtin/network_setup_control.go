// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

const networkSetupControlSummary = `allows access to netplan configuration`

const networkSetupControlBaseDeclarationSlots = `
  network-setup-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const networkSetupControlConnectedPlugAppArmor = `
# Description: Can read/write netplan configuration files

/etc/netplan/{,**} rw,
/etc/network/{,**} rw,
`

func init() {
	registerIface(&commonInterface{
		name:                  "network-setup-control",
		summary:               networkSetupControlSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  networkSetupControlBaseDeclarationSlots,
		connectedPlugAppArmor: networkSetupControlConnectedPlugAppArmor,
		reservedForOS:         true,
	})
}
