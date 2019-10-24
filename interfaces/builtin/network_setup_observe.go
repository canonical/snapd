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

const networkSetupObserveSummary = `allows read access to netplan configuration`

const networkSetupObserveBaseDeclarationSlots = `
  network-setup-observe:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const networkSetupObserveConnectedPlugAppArmor = `
# Description: Can read netplan configuration files

/etc/netplan/{,**} r,
/etc/network/{,**} r,
/etc/systemd/network/{,**} r,

/run/systemd/network/{,**} r,
/run/NetworkManager/conf.d/{,**} r,
/run/udev/rules.d/ r,
/run/udev/rules.d/[0-9]*-netplan-* r,

#include <abstractions/dbus-strict>

# Allow use of Netplan Info API, used to get information on available netplan
# features and version
dbus (send)
    bus=system
    interface=io.netplan.Netplan
    path=/io/netplan/Netplan
	member=Info
	peer=(label=unconfined),

`

func init() {
	registerIface(&commonInterface{
		name:                  "network-setup-observe",
		summary:               networkSetupObserveSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  networkSetupObserveBaseDeclarationSlots,
		connectedPlugAppArmor: networkSetupObserveConnectedPlugAppArmor,
		reservedForOS:         true,
	})
}
