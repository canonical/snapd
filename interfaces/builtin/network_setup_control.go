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

# Allow use of the netplan binary from the base snap. With this interface, this
# is expected to be able to apply and generate new network configuration, as
# well as get information about the current network configuration.
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

/etc/netplan/{,**} rw,
/etc/network/{,**} rw,
/etc/systemd/network/{,**} rw,

# netplan generate
/run/ r,
/run/systemd/network/{,**} r,
/run/systemd/network/*-netplan-* w,
/run/NetworkManager/conf.d/{,**} r,
/run/NetworkManager/conf.d/*netplan*.conf* w,

/run/udev/rules.d/ rw,                 # needed for cloud-init
/run/udev/rules.d/[0-9]*-netplan-* rw,

#include <abstractions/dbus-strict>

# Allow use of Netplan API, used to configure network using dbus
# (see https://netplan.readthedocs.io/en/stable/netplan-dbus/)
dbus (send)
    bus=system
    interface=io.netplan.Netplan
    path=/io/netplan/Netplan
	member={Generate,Apply,Info,Config}
	peer=(label=unconfined),

# This is the interface for configuration objects
dbus (send)
    bus=system
    interface=io.netplan.Netplan.Config
    path=/io/netplan/Netplan/config/*
	member={Get,Set,Try,Cancel,Apply}
	peer=(label=unconfined),
`

func init() {
	registerIface(&commonInterface{
		name:                  "network-setup-control",
		summary:               networkSetupControlSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  networkSetupControlBaseDeclarationSlots,
		connectedPlugAppArmor: networkSetupControlConnectedPlugAppArmor,
	})
}
