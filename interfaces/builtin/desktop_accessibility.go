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

#include <abstractions/dbus-accessibility-strict>

# Allow the accessibility services in the user session to send us any events
dbus (receive)
    bus=accessibility
    peer=(label=unconfined),

# Allow querying for capabilities and registering
dbus (send)
    bus=accessibility
    path="/org/a11y/atspi/accessible/root"
    interface="org.a11y.atspi.Socket"
    member="Embed"
    peer=(name=org.a11y.atspi.Registry, label=unconfined),
dbus (send)
    bus=accessibility
    path="/org/a11y/atspi/registry"
    interface="org.a11y.atspi.Registry"
    member="GetRegisteredEvents"
    peer=(name=org.a11y.atspi.Registry, label=unconfined),
dbus (send)
    bus=accessibility
    path="/org/a11y/atspi/registry/deviceeventcontroller"
    interface="org.a11y.atspi.DeviceEventController"
    member="Get{DeviceEvent,Keystroke}Listeners"
    peer=(name=org.a11y.atspi.Registry, label=unconfined),
dbus (send)
    bus=accessibility
    path="/org/a11y/atspi/registry/deviceeventcontroller"
    interface="org.a11y.atspi.DeviceEventController"
    member="NotifyListenersSync"
    peer=(name=org.a11y.atspi.Registry, label=unconfined),

# org.a11y.atspi is not designed for application isolation and these rules
# can be used to send change events for other processes.
# TODO: verify that these don't need some sort of application key or otherwise
# aren't safe
dbus (send)
    bus=accessibility
    path="/org/a11y/atspi/accessible/root"
    interface="org.a11y.atspi.Event.Object"
    member="ChildrenChanged"
    peer=(name=org.freedesktop.DBus, label=unconfined),
dbus (send)
    bus=accessibility
    path="/org/a11y/atspi/accessible/root"
    interface="org.a11y.atspi.Accessible"
    member="Get*"
    peer=(label=unconfined),
dbus (send)
    bus=accessibility
    path="/org/a11y/atspi/accessible/[0-9]*"
    interface="org.a11y.atspi.Event.Object"
    member="{ChildrenChanged,PropertyChange,StateChanged,TextCaretMoved}"
    peer=(name=org.freedesktop.DBus, label=unconfined),
dbus (send)
    bus=accessibility
    path="/org/a11y/atspi/accessible/[0-9]*"
    interface="org.freedesktop.DBus.Properties"
    member="Get{,All}"
    peer=(label=unconfined),

# TODO: what does this do?
dbus (send)
    bus=accessibility
    path="/org/a11y/atspi/cache"
    interface="org.a11y.atspi.Cache"
    member="{Add,Remove}Accessible"
    peer=(name=org.freedesktop.DBus, label=unconfined),
`

const desktopAccessibilityConnectedPlugSecComp = `
# Description: Can access desktop accessibility features. This gives privileged
# access to the user's input.
listen
accept
accept4
`

func init() {
	registerIface(&commonInterface{
		name:                  "desktop-accessibility",
		summary:               desktopAccessibilitySummary,
		implicitOnClassic:     true,
		baseDeclarationSlots:  desktopAccessibilityBaseDeclarationSlots,
		connectedPlugAppArmor: desktopAccessibilityConnectedPlugAppArmor,
		connectedPlugSecComp:  desktopAccessibilityConnectedPlugSecComp,
		reservedForOS:         true,
	})
}
