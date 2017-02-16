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

import (
	"github.com/snapcore/snapd/interfaces"
)

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
dbus (send)
    bus=session
    path=/Screensaver
    interface=org.freedesktop.ScreenSaver
    member=org.freedesktop.ScreenSaver.{Inhibit,UnInhibit,SimulateUserActivity}
    peer=(label=unconfined),

# gnome, kde and cinnamon screensaver
dbus (send)
    bus=session
    path=/{,ScreenSaver}
    interface=org.{gnome.ScreenSaver,kde.screensaver,cinnamon.ScreenSaver}
    member=SimulateUserActivity
    peer=(label=unconfined),
`

const screenInhibitControlConnectedPlugSecComp = `
# Description: Can inhibit and uninhibit screen savers in desktop sessions.
# dbus
recvfrom
recvmsg
send
sendto
sendmsg
`

// NewScreenInhibitControlInterface returns a new "screen-inhibit-control" interface.
func NewScreenInhibitControlInterface() interfaces.Interface {
	return &commonInterface{
		name: "screen-inhibit-control",
		connectedPlugAppArmor: screenInhibitControlConnectedPlugAppArmor,
		connectedPlugSecComp:  screenInhibitControlConnectedPlugSecComp,
		reservedForOS:         true,
	}
}
