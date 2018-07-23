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

const screenInhibitControlSummary = `allows inhibiting the screen saver`

const screenInhibitBaseDeclarationSlots = `
  screen-inhibit-control:
    allow-installation:
      slot-snap-type:
        - core
`

const screenInhibitControlConnectedPlugAppArmor = `
# Description: Can inhibit and uninhibit screen savers in desktop sessions.
#include <abstractions/dbus-session-strict>
#include <abstractions/dbus-strict>

# gnome-session
dbus (send)
    bus=session
    path=/org/gnome/SessionManager
    interface=org.gnome.SessionManager
    member={Inhibit,Uninhibit}
    peer=(label=unconfined),

# unity screen API
dbus (send)
    bus=system
    interface="org.freedesktop.DBus.Introspectable"
    path="/com/canonical/Unity/Screen"
    member="Introspect"
    peer=(label=unconfined),
dbus (send)
    bus=system
    interface="com.canonical.Unity.Screen"
    path="/com/canonical/Unity/Screen"
    member={keepDisplayOn,removeDisplayOnRequest}
    peer=(label=unconfined),

# freedesktop.org ScreenSaver
# compatibility rule
dbus (send)
    bus=session
    path=/Screensaver
    interface=org.freedesktop.ScreenSaver
    member={Inhibit,UnInhibit,SimulateUserActivity}
    peer=(label=unconfined),

# API rule
dbus (send)
    bus=session
    path=/{,org/freedesktop/,org/gnome/}ScreenSaver
    interface=org.freedesktop.ScreenSaver
    member={Inhibit,UnInhibit,SimulateUserActivity}
    peer=(label=unconfined),

# gnome, kde and cinnamon screensaver
dbus (send)
    bus=session
    path=/{,ScreenSaver}
    interface=org.{gnome.ScreenSaver,kde.screensaver,cinnamon.ScreenSaver}
    member=SimulateUserActivity
    peer=(label=unconfined),
`

func init() {
	registerIface(&commonInterface{
		name:                  "screen-inhibit-control",
		summary:               screenInhibitControlSummary,
		implicitOnClassic:     true,
		baseDeclarationSlots:  screenInhibitBaseDeclarationSlots,
		connectedPlugAppArmor: screenInhibitControlConnectedPlugAppArmor,
		reservedForOS:         true,
	})
}
