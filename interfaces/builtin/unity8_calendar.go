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

const unity8CalendarPermanentSlotAppArmor = `
# Description: Allow operating as the EDS service. Reserved because this
#  gives privileged access to the system.
# Usage: reserved

# DBus accesses
#include <abstractions/dbus-session-strict>
dbus (bind)
	bus=session
	name="org.gnome.evolution.dataserver.Calendar7",
dbus (bind)
	bus=session
	name=org.gnome.evolution.dataserver.Subprocess.Backend.*,

########################
# Calendar
########################
dbus (receive, send)
	bus=session
	path=/org/gnome/evolution/dataserver/CalendarFactory,
dbus (receive, send)
	bus=session
	path=/org/gnome/evolution/dataserver/CalendarFactory
	interface=org.gnome.evolution.dataserver.CalendarFactory,
dbus (receive, send)
	bus=session
	path=/org/gnome/evolution/dataserver/CalendarFactory
	interface=org.freedesktop.DBus.*,

dbus (receive, send)
	bus=session
	path=/org/gnome/evolution/dataserver/CalendarView/**,
dbus (receive, send)
	bus=session
	path=/org/gnome/evolution/dataserver/CalendarView/**
	interface=org.gnome.evolution.dataserver.CalendarView,
dbus (receive, send)
	bus=session
	path=/org/gnome/evolution/dataserver/CalendarView/**
	interface=org.freedesktop.DBus.*,

dbus (receive, send)
	bus=session
	path=/org/gnome/evolution/dataserver/Subprocess/Backend/Calendar/**
	interface=org.gnome.evolution.dataserver.Subprocess.Backend,
dbus (receive, send)
	bus=session
	path=/org/gnome/evolution/dataserver/Subprocess/Backend/Calendar/**
	interface=org.freedesktop.DBus.*,

dbus (receive, send)
	bus=session
	path=/org/gnome/evolution/dataserver/Subprocess/**
	interface=org.gnome.evolution.dataserver.Calendar,
dbus (receive, send)
	bus=session
	path=/org/gnome/evolution/dataserver/Subprocess/**
	interface=org.freedesktop.DBus.*,
`

const unity8CalendarConnectedPlugAppArmor = `
# Description: Can access the calendar. This policy group is reserved for
#  vetted applications only in this version of the policy. Once LP: #1227824
#  is fixed, this can be moved out of reserved status.
# Usage: reserved

dbus (receive, send)
     bus=session
     path=/org/gnome/evolution/dataserver/CalendarFactory
     peer=(label=unconfined),
dbus (receive, send)
     bus=session
     path=/org/gnome/evolution/dataserver/Subprocess/**
     peer=(label=unconfined),
dbus (receive, send)
     bus=session
     path=/org/gnome/evolution/dataserver/CalendarView/**
     peer=(label=unconfined),

# LP: #1319546. Apps shouldn't talk directly to sync-monitor, but allow it for
# now for trusted apps until sync-monitor is integrated with push
# notifications.
dbus (receive, send)
     bus=session
     path=/synchronizer{,/**}
     peer=(label=unconfined),
`

const unity8CalendarPermanentSlotDBus = `
	<allow own="org.gnome.evolution.dataserver.Calendar7"/>
	<allow send_destination="org.gnome.evolution.dataserver.Calendar7"/>
	<allow send_interface="org.gnome.evolution.dataserver.Calendar"/>
	<allow send_interface="org.gnome.evolution.dataserver.CalendarView"/>
	<allow send_interface="org.gnome.evolution.dataserver.CalendarFactory"/>
`

// NewCameraInterface returns a new "camera" interface.
func NewUnity8CalendarInterface() interfaces.Interface {
	return &unity8PimInterface{
		name: "unity8-calendar",
		permanentSlotAppArmor: unity8CalendarPermanentSlotAppArmor,
		connectedPlugAppArmor: unity8CalendarConnectedPlugAppArmor,
		permanentSlotDBus:     unity8CalendarPermanentSlotDBus,
	}
}
