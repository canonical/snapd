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
	name="org.gnome.evolution.dataserver.Subprocess.Backend.Calendar*",
dbus (bind)
	bus=session
	name="com.canonical.SyncMonitor",

# Allow traffic to/from our path and interface with any method for unconfined
# clients to talk to our calendar services.
dbus (receive, send)
	bus=session
	path=/org/gnome/evolution/dataserver/CalendarFactory
	peer=(label=unconfined),
dbus (receive, send)
	bus=session
	path=/org/gnome/evolution/dataserver/CalendarView/**
	peer=(label=unconfined),
dbus (receive, send)
	bus=session
	path=/org/gnome/evolution/dataserver/Subprocess/**
	interface=org.gnome.evolution.dataserver.Calendar
	peer=(label=unconfined),
dbus (receive, send)
	bus=session
	path=/org/gnome/evolution/dataserver/Subprocess/Backend/Calendar/**
	peer=(label=unconfined),

# LP: #1319546. Apps shouldn't talk directly to sync-monitor, but allow it for
# now for trusted apps until sync-monitor is integrated with push
# notifications.
dbus (receive, send)
	bus=session
	path=/com/canonical/SyncMonitor
	peer=(label=unconfined),
`

const unity8CalendarConnectedSlotAppArmor = `
# Allow service to interact with connected clients
# DBus accesses

########################
# Calendar
########################
dbus (receive, send)
	bus=session
	path=/org/gnome/evolution/dataserver/CalendarFactory
	peer=(label=###PLUG_SECURITY_TAGS###),
dbus (receive, send)
	bus=session
	path=/org/gnome/evolution/dataserver/CalendarView/**
	peer=(label=###PLUG_SECURITY_TAGS###),
dbus (receive, send)
	bus=session
	path=/org/gnome/evolution/dataserver/Subprocess/**
	interface=org.gnome.evolution.dataserver.Calendar
	peer=(label=###PLUG_SECURITY_TAGS###),
dbus (receive, send)
	bus=session
	path=/org/gnome/evolution/dataserver/Subprocess/Backend/Calendar/**
	peer=(label=###PLUG_SECURITY_TAGS###),

# LP: #1319546. Apps shouldn't talk directly to sync-monitor, but allow it for
# now for trusted apps until sync-monitor is integrated with push
# notifications.
dbus (receive, send)
	bus=session
	path=/com/canonical/SyncMonitor
	peer=(label=###PLUG_SECURITY_TAGS###),
`

const unity8CalendarConnectedPlugAppArmor = `
# Description: Can access the calendar. This policy group is reserved for
#  vetted applications only in this version of the policy. Once LP: #1227824
#  is fixed, this can be moved out of reserved status.
# Usage: reserved

########################
# Calendar
########################
dbus (receive, send)
	bus=session
	path=/org/gnome/evolution/dataserver/CalendarFactory
	peer=(label=###SLOT_SECURITY_TAGS###),
dbus (receive, send)
	bus=session
	path=/org/gnome/evolution/dataserver/CalendarView/**
	peer=(label=###SLOT_SECURITY_TAGS###),
dbus (receive, send)
	bus=session
	path=/org/gnome/evolution/dataserver/Subprocess/**
	interface=org.gnome.evolution.dataserver.Calendar
	peer=(label=###SLOT_SECURITY_TAGS###),
dbus (receive, send)
	bus=session
	path=/org/gnome/evolution/dataserver/Subprocess/Backend/Calendar/**
	peer=(label=###SLOT_SECURITY_TAGS###),
dbus (receive, send)
	bus=session
	path=/com/canonical/SyncMonitor
	peer=(label=###SLOT_SECURITY_TAGS###),

# LP: #1319546. Apps shouldn't talk directly to sync-monitor, but allow it for
# now for trusted apps until sync-monitor is integrated with push
# notifications.
dbus (receive, send)
	bus=session
	path=/com/canonical/SyncMonitor
	peer=(label=###SLOT_SECURITY_TAGS###),
`

const unity8CalendarPermanentSlotDBus = `
	<allow own="org.gnome.evolution.dataserver.Calendar7"/>
	<allow send_destination="org.gnome.evolution.dataserver.Calendar7"/>
	<allow send_interface="org.gnome.evolution.dataserver.Calendar"/>
	<allow send_interface="org.gnome.evolution.dataserver.CalendarView"/>
	<allow send_interface="org.gnome.evolution.dataserver.CalendarFactory"/>
`

// NewUnity8CalendarInterface returns a new "untiy8-calendar" interface.
func NewUnity8CalendarInterface() interfaces.Interface {
	return &unity8PimCommonInterface{
		name: "unity8-calendar",
		permanentSlotAppArmor: unity8CalendarPermanentSlotAppArmor,
		connectedSlotAppArmor: unity8CalendarConnectedSlotAppArmor,
		connectedPlugAppArmor: unity8CalendarConnectedPlugAppArmor,
		permanentSlotDBus:     unity8CalendarPermanentSlotDBus,
	}
}
