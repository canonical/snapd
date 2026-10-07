// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

const ubuntuDriversObserveSummary = `allows querying the Ubuntu drivers service`

const ubuntuDriversObserveBaseDeclarationSlots = `
  ubuntu-drivers-observe:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const ubuntuDriversObserveConnectedPlugAppArmor = `
# Description: Allow querying the Ubuntu drivers service for devices and their
# available driver packages.

#include <abstractions/dbus-strict>

# allow calls to the well-known service name, including during activation
dbus (send)
    bus=system
    path=/com/ubuntu/Drivers
    interface=com.ubuntu.Drivers
    member=drivers
    peer=(name=com.ubuntu.Drivers),
dbus (send)
    bus=system
    path=/com/ubuntu/Drivers
    interface=org.freedesktop.DBus.Introspectable
    member=Introspect
    peer=(name=com.ubuntu.Drivers),

# some D-Bus clients resolve the well-known service name to its unique bus
# name after activation, so allow calls to the unconfined service process
dbus (send)
    bus=system
    path=/com/ubuntu/Drivers
    interface=com.ubuntu.Drivers
    member=drivers
    peer=(label=unconfined),
dbus (send)
    bus=system
    path=/com/ubuntu/Drivers
    interface=org.freedesktop.DBus.Introspectable
    member=Introspect
    peer=(label=unconfined),
`

func init() {
	registerIface(&commonInterface{
		name:                  "ubuntu-drivers-observe",
		summary:               ubuntuDriversObserveSummary,
		implicitOnClassic:     true,
		baseDeclarationSlots:  ubuntuDriversObserveBaseDeclarationSlots,
		connectedPlugAppArmor: ubuntuDriversObserveConnectedPlugAppArmor,
	})
}
