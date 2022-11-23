// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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

const systemdReloadSummary = `allows reloading the systemd manager configuration`

const systemdReloadBaseDeclarationPlugs = `
  systemd-reload:
    allow-installation: false
    deny-auto-connection: true
`

const systemdReloadBaseDeclarationSlots = `
  systemd-reload:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const systemdReloadConnectedPlugAppArmor = `
# Description: allow the dbus call for systemctl daemon-reload

#include <abstractions/dbus-strict>

dbus (send)
    bus=system
    path=/org/freedesktop/systemd1
    interface=org.freedesktop.systemd1.Manager
    member=Reload
    peer=(label=unconfined),

# Allow clients to introspect
# do not use peer=(label=unconfined) here since this is DBus activated
dbus (send)
    bus=system
    path=/org/freedesktop/systemd1
    interface=org.freedesktop.DBus.Introspectable
    member=Introspect,
`

func init() {
	registerIface(&commonInterface{
		name:                  "systemd-reload",
		summary:               systemdReloadSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  systemdReloadBaseDeclarationSlots,
		baseDeclarationPlugs:  systemdReloadBaseDeclarationPlugs,
		connectedPlugAppArmor: systemdReloadConnectedPlugAppArmor,
	})
}
