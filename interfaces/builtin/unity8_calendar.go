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

const unity8CalendarSummary = `allows operating as or interacting with the Unity 8 Calendar Service`

const unity8CalendarBaseDeclarationSlots = `
  unity8-calendar:
    allow-installation:
      slot-snap-type:
        - app
    deny-auto-connection: true
    deny-connection: true
`

const unity8CalendarPermanentSlotAppArmor = `
# Description: Allow operating as the EDS service. This gives privileged access
# to the system.

# DBus accesses
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
dbus (receive)
	bus=session
	path=/org/gnome/evolution/dataserver/CalendarFactory
	peer=(label=unconfined),
dbus (receive)
	bus=session
	path=/org/gnome/evolution/dataserver/CalendarView/**
	peer=(label=unconfined),
dbus (receive)
	bus=session
	path=/org/gnome/evolution/dataserver/Subprocess/**
	interface=org.gnome.evolution.dataserver.Calendar
	peer=(label=unconfined),
dbus (receive)
	bus=session
	path=/org/gnome/evolution/dataserver/Subprocess/Backend/Calendar/**
	peer=(label=unconfined),

# LP: #1319546. Apps shouldn't talk directly to sync-monitor, but allow it for
# now for trusted apps until sync-monitor is integrated with push
# notifications.
dbus (receive)
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
# Allow connected clients to communicate with calendar service via DBus

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

func init() {
	registerIface(&unity8PimCommonInterface{
		name:                  "unity8-calendar",
		summary:               unity8CalendarSummary,
		baseDeclarationSlots:  unity8CalendarBaseDeclarationSlots,
		permanentSlotAppArmor: unity8CalendarPermanentSlotAppArmor,
		connectedSlotAppArmor: unity8CalendarConnectedSlotAppArmor,
		connectedPlugAppArmor: unity8CalendarConnectedPlugAppArmor,
	})
}
