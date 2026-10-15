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

const networkStatusSummary = `allows access to network connectivity status`

const networkStatusBaseDeclarationSlots = `
  network-status:
    allow-installation:
      slot-snap-type:
        - core
`

const networkStatusConnectedPlugAppArmor = `
# Description: allow access to network connectivity status

#include <abstractions/dbus-session-strict>

# Allow access to xdg-desktop-portal NetworkMonitor methods and signals
dbus (send, receive)
    bus=session
    interface=org.freedesktop.portal.NetworkMonitor
    path=/org/freedesktop/portal/desktop
    peer=(label=unconfined),
`

type networkStatusInterface struct {
	commonInterface
}

func init() {
	registerIface(&networkStatusInterface{
		commonInterface: commonInterface{
			name:                  "network-status",
			summary:               networkStatusSummary,
			implicitOnCore:        true,
			implicitOnClassic:     true,
			baseDeclarationSlots:  networkStatusBaseDeclarationSlots,
			connectedPlugAppArmor: networkStatusConnectedPlugAppArmor,
		},
	})
}
