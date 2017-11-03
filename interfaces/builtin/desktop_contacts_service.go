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

const desktopContactsServiceSummary = `allows communication with Evolution Data Service Addressbook`

const desktopContactsServiceBaseDeclarationSlots = `
  desktop-contacts-service:
    allow-installation:
      slot-snap-type:
        - core
    deny-connection: true
`

const desktopContactsServiceConnectedPlugAppArmor = `
# Description: Allow access to Evolution Data Service

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
	path=/org/gnome/evolution/dataserver/AddressBook{,/**}
	peer=(label=unconfined),
dbus (receive, send)
	bus=session
	interface=org.freedesktop.DBus.Properties
	path=/org/gnome/evolution/dataserver/AddressBookFactory
	peer=(label=unconfined),
dbus (receive, send)
	bus=session
	interface=org.freedesktop.DBus.Properties
	path=/org/gnome/evolution/dataserver/AddressBookView{,/**}
	peer=(label=unconfined),
dbus (receive, send)
	bus=session
	interface=org.freedesktop.DBus.Properties
	path=/org/gnome/evolution/dataserver/Subprocess{,/**}
	peer=(label=unconfined),
# Allow access to methods
dbus (receive, send)
	bus=session
	interface=org.gnome.evolution.dataserver.AddressBook
	path=/org/gnome/evolution/dataserver/{Subprocess,AddressBook}{,/**}
	peer=(label=unconfined),
dbus (receive, send)
	bus=session
	interface=org.gnome.evolution.dataserver.AddressBookFactory
	path=/org/gnome/evolution/dataserver/AddressBookFactory
	peer=(label=unconfined),
dbus (receive, send)
	bus=session
	interface=org.gnome.evolution.dataserver.AddressBookView
	path=/org/gnome/evolution/dataserver/AddressBookView{,/**}
	peer=(label=unconfined),
dbus (receive, send)
	bus=session
	interface=org.gnome.evolution.dataserver.Source
	path=/org/gnome/evolution/dataserver/SourceManager{,/**}
	peer=(label=unconfined),

# Allow clients to introspect the service
dbus (send)
	bus=session
	interface=org.freedesktop.DBus.Introspectable
	path=/org/gnome/evolution/dataserver/AddressBook{,/**}
	member=Introspect
	peer=(label=unconfined),
dbus (send)
	bus=session
	interface=org.freedesktop.DBus.Introspectable
	path=/org/gnome/evolution/dataserver/AddressBookFactory
	member=Introspect
	peer=(label=unconfined),
dbus (send)
	bus=session
	interface=org.freedesktop.DBus.Introspectable
	path=/org/gnome/evolution/dataserver/AddressBookView{,/**}
	member=Introspect
	peer=(label=unconfined),
dbus (send)
	bus=session
	interface=org.freedesktop.DBus.Introspectable
	path=/org/gnome/evolution/dataserver/SourceManager{,/**}
	member=Introspect
	peer=(label=unconfined),

# Allow access to cached avatars
owner @{HOME}/.cache/evolution/addressbook/** r,
`

func init() {
	registerIface(&commonInterface{
		name:                  "desktop-contacts-service",
		summary:               desktopContactsServiceSummary,
		implicitOnClassic:     true,
		reservedForOS:         true,
		baseDeclarationSlots:  desktopContactsServiceBaseDeclarationSlots,
		connectedPlugAppArmor: desktopContactsServiceConnectedPlugAppArmor,
	})
}
