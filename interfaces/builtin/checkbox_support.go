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
include <abstractions/dbus-strict>

# Allow starting transient units in which the tests are run. This is usually
# carried out using a dedicated helper such as
# https://gitlab.com/zygoon/plz-run which communicates with systemd over dbus.
dbus (send)
    bus=system
    path=/org/freedesktop/systemd1
    interface=org.freedesktop.systemd1.Manager
    member=StartTransientUnit
    peer=(label=unconfined),

# No mediation of which unit can be killed.
dbus (send)
    bus=system
    path=/org/freedesktop/systemd1
    interface=org.freedesktop.systemd1.Manager
    member=KillUnit
    peer=(label=unconfined),

# NOTE: D-Bus signals are mediated with the random connection name, not with
# the well-known bus name claimed by the peer.  Method calls are mediated with
# the bus name because that's how they are made by clients.

# Allow observing the JobRemoved signal which informs the caller of
# StartTransientUnit that the job has been dispatched and that the one-shot
# service has finished.
dbus (receive)
    bus=system
    path=/org/freedesktop/systemd1
    interface=org.freedesktop.systemd1.Manager
    member=JobRemoved
    peer=(label=unconfined),

# Allow observing PropertiesChanged signals from any service. This conveys,
# among others, completion of the service as well as the exit status of the
# main process.
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
