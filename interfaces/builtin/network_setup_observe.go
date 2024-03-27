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

# Allow use of the netplan binary from the base snap. With this interface, this
# is expected to be able to only get information about the current network
# configuration and not generate or apply it like is allowed with
# network-setup-control.
/usr/sbin/netplan ixr,
# core18+ has /usr/sbin/netplan as a symlink to this script
/usr/share/netplan/netplan.script ixr,
# netplan related files
/usr/share/netplan/ r,
/usr/share/netplan/** r,

# Netplan uses busctl internally, so allow using that as well
/usr/bin/busctl ixr,

# from systemd 254 onward, busctl binds to a unix socket upon startup to
# facilitate debugging
unix (bind) type=stream addr="@[0-9a-fA-F]*/bus/busctl/*",

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
	})
}
