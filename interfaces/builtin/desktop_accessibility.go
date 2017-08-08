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

const desktopAccessibilitySummary = `allows using desktop accessibility`

const desktopAccessibilityBaseDeclarationSlots = `
  desktop-accessibility:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const desktopAccessibilityConnectedPlugAppArmor = `
# Description: Can access desktop accessibility features. This gives privileged
# access to the user's input.

#include <abstractions/dbus-session-strict>
dbus (send)
    bus=session
    path=/org/a11y/bus
    interface=org.a11y.Bus
    member=GetAddress
    peer=(label=unconfined),
dbus (send)
    bus=session
    path=/org/a11y/bus
    interface=org.freedesktop.DBus.Properties
    member=Get{,All}
    peer=(label=unconfined),

# unfortunate, but org.a11y.atspi is not designed for separation
#include <abstractions/dbus-accessibility-strict>
dbus (receive, send)
    bus=accessibility
    path=/org/a11y/atspi/**
`

func init() {
	registerIface(&commonInterface{
		name:                  "desktop-accessibility",
		summary:               desktopAccessibilitySummary,
		implicitOnClassic:     true,
		baseDeclarationSlots:  desktopAccessibilityBaseDeclarationSlots,
		connectedPlugAppArmor: desktopAccessibilityConnectedPlugAppArmor,
		reservedForOS:         true,
	})
}
