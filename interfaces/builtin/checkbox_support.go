// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

const checkboxSupportSummary = `allows checkbox to execute arbitrary system tests`

const checkboxSupportBaseDeclarationPlugs = `
  checkbox-support:
    allow-installation: false
    deny-auto-connection: true
`

const checkboxSupportBaseDeclarationSlots = `
  checkbox-support:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const checkboxSupportConnectedPlugAppArmor = `
#include <abstractions/dbus-strict>
dbus (send)
    bus=system
    path=/org/freedesktop/systemd1
    interface=org.freedesktop.systemd1.Manager
    member=StartTransientUnit
    peer=(label=unconfined),
dbus (send)
    bus=system
    path=/org/freedesktop/systemd1
    interface=org.freedesktop.systemd1.Manager
    member=KillUnit
    peer=(label=unconfined),
dbus (receive)
    bus=system
    path=/org/freedesktop/systemd1
    interface=org.freedesktop.systemd1.Manager
    member=JobRemoved
    peer=(label=unconfined),
dbus (receive)
    bus=system
    path=/org/freedesktop/systemd1/unit/*_2eservice
    interface=org.freedesktop.DBus.Properties
    member=PropertiesChanged
    peer=(label=unconfined),
`

func init() {
	registerIface(&commonInterface{
		name:                  "checkbox-support",
		summary:               checkboxSupportSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		connectedPlugAppArmor: checkboxSupportConnectedPlugAppArmor,
		baseDeclarationSlots:  checkboxSupportBaseDeclarationSlots,
		baseDeclarationPlugs:  checkboxSupportBaseDeclarationPlugs,
	})
}
