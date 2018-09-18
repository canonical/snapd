// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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
	"github.com/snapcore/snapd/release"
)

const calendarServiceSummary = `allows communication with Evolution Data Service Calendar`

const calendarServiceBaseDeclarationSlots = `
  calendar-service:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const calendarServiceConnectedPlugAppArmor = `
# Description: Allow access to Evolution Data Service for calendars

#include <abstractions/dbus-session-strict>

# Allow use of ObjectManager APIs, used to enumerate sources
dbus (receive, send)
	bus=session
	interface=org.freedesktop.DBus.ObjectManager
	path=/org/gnome/evolution/dataserver{,/**}
	peer=(label=unconfined),

# Allow access to properties on sources
dbus (receive, send)
	bus=session
	interface=org.freedesktop.DBus.Properties
	path=/org/gnome/evolution/dataserver/SourceManager{,/**}
	peer=(label=unconfined),
dbus (receive, send)
	bus=session
	interface=org.freedesktop.DBus.Properties
	path=/org/gnome/evolution/dataserver/Calendar{,/**}
	peer=(label=unconfined),
dbus (receive, send)
	bus=session
	interface=org.freedesktop.DBus.Properties
	path=/org/gnome/evolution/dataserver/CalendarFactory
	peer=(label=unconfined),
dbus (receive, send)
	bus=session
	interface=org.freedesktop.DBus.Properties
	path=/org/gnome/evolution/dataserver/CalendarView{,/**}
	peer=(label=unconfined),
dbus (receive, send)
	bus=session
	interface=org.freedesktop.DBus.Properties
	path=/org/gnome/evolution/dataserver/Subprocess{,/**}
	peer=(label=unconfined),
# Allow access to methods
dbus (receive, send)
	bus=session
	interface=org.gnome.evolution.dataserver.Calendar
	path=/org/gnome/evolution/dataserver/{Subprocess,Calendar}{,/**}
	peer=(label=unconfined),
dbus (receive, send)
	bus=session
	interface=org.gnome.evolution.dataserver.CalendarFactory
	path=/org/gnome/evolution/dataserver/CalendarFactory
	peer=(label=unconfined),
dbus (receive, send)
	bus=session
	interface=org.gnome.evolution.dataserver.CalendarView
	path=/org/gnome/evolution/dataserver/CalendarView{,/**}
	peer=(label=unconfined),
dbus (receive, send)
	bus=session
	interface=org.gnome.evolution.dataserver.Source
	path=/org/gnome/evolution/dataserver/SourceManager{,/**}
	peer=(label=unconfined),
dbus (receive, send)
	bus=session
	interface=org.gnome.evolution.dataserver.Source.Removable
	path=/org/gnome/evolution/dataserver/SourceManager{,/**}
	peer=(label=unconfined),
dbus (receive, send)
	bus=session
	interface=org.gnome.evolution.dataserver.SourceManager
	path=/org/gnome/evolution/dataserver/SourceManager
	peer=(label=unconfined),

# Allow clients to introspect the service
dbus (send)
	bus=session
	interface=org.freedesktop.DBus.Introspectable
	path=/org/gnome/evolution/dataserver/Calendar{,/**}
	member=Introspect
	peer=(label=unconfined),
dbus (send)
	bus=session
	interface=org.freedesktop.DBus.Introspectable
	path=/org/gnome/evolution/dataserver/CalendarFactory
	member=Introspect
	peer=(label=unconfined),
dbus (send)
	bus=session
	interface=org.freedesktop.DBus.Introspectable
	path=/org/gnome/evolution/dataserver/CalendarView{,/**}
	member=Introspect
	peer=(label=unconfined),
dbus (send)
	bus=session
	interface=org.freedesktop.DBus.Introspectable
	path=/org/gnome/evolution/dataserver/SourceManager{,/**}
	member=Introspect
	peer=(label=unconfined),
`

func init() {
	registerIface(&commonInterface{
		name:                  "calendar-service",
		summary:               calendarServiceSummary,
		implicitOnClassic:     !(release.ReleaseInfo.ID == "ubuntu" && release.ReleaseInfo.VersionID == "14.04"),
		reservedForOS:         true,
		baseDeclarationSlots:  calendarServiceBaseDeclarationSlots,
		connectedPlugAppArmor: calendarServiceConnectedPlugAppArmor,
	})
}
