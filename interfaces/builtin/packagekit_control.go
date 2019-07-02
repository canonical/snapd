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

const packageKitControlSummary = `allows control of the PackageKit service`

const packageKitControlBaseDeclarationSlots = `
  packagekit-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const packageKitControlConnectedPlugAppArmor = `
# Description: Allow access to PackageKit service

#include <abstractions/dbus-system-strict>

# Allow communication with the main PackageKit end point.
dbus (receive, send)
        bus=system
        interface=org.freedesktop.PackageKit
        path=/org/freedesktop/PackageKit
        peer=(label=unconfined),
dbus (receive, send)
        bus=system
        interface=org.freedesktop.PackageKit.Offline
        path=/org/freedesktop/PackageKit
        peer=(label=unconfined),
dbus (receive, send)
        bus=system
        interface=org.freedesktop.DBus.Properties
        path=/org/freedesktop/PackageKit
        peer=(label=unconfined),
dbus (send)
	bus=system
	interface=org.freedesktop.DBus.Introspectable
	path=/org/freedesktop/PackageKit
	member=Introspect
	peer=(label=unconfined),

# Allow communication with PackageKit transactions.  Unfortunately
# transactions are exported using random object paths like
# "/2371_dcbabcba", so we can't reliably use the path in the match
# rule.
dbus (receive, send)
        bus=system
        interface=org.freedesktop.PackageKit.Transaction
        peer=(label=unconfined),
dbus (receive, send)
        bus=system
        interface=org.freedesktop.DBus.Properties
        peer=(label=unconfined),
dbus (send)
	bus=system
	interface=org.freedesktop.DBus.Introspectable
	member=Introspect
	peer=(label=unconfined),
`

func init() {
	registerIface(&commonInterface{
		name:                  "packagekit-control",
		summary:               packageKitControlSummary,
		implicitOnClassic:     true,
		reservedForOS:         true,
		baseDeclarationSlots:  packageKitControlBaseDeclarationSlots,
		connectedPlugAppArmor: packageKitControlConnectedPlugAppArmor,
	})
}
