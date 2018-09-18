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

const accountsServiceSummary = `allows communication with the Accounts service like GNOME Online Accounts`

const accountsServiceBaseDeclarationSlots = `
  accounts-service:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const accountsServiceConnectedPlugAppArmor = `
# Description: Allow access to Accounts service like GNOME Online Accounts

#include <abstractions/dbus-session-strict>

# Allow use of ObjectManager APIs, used to enumerate accounts
dbus (receive, send)
    bus=session
    interface=org.freedesktop.DBus.ObjectManager
    path=/org/gnome/OnlineAccounts
    peer=(label=unconfined),

# Allow getting/setting properties on accounts
dbus (receive, send)
    bus=session
    interface=org.freedesktop.DBus.Properties
    path=/org/gnome/OnlineAccounts{,/**}
    peer=(label=unconfined),

# Allow use of all OnlineAccounts methods
dbus (receive, send)
    bus=session
    interface=org.gnome.OnlineAccounts.*
    path=/org/gnome/OnlineAccounts{,/**}
    peer=(label=unconfined),

# Allow clients to introspect the service
dbus (send)
    bus=session
    interface=org.freedesktop.DBus.Introspectable
    path=/com/ubuntu/OnlineAccounts{,/**}
    member=Introspect
    peer=(label=unconfined),
`

func init() {
	registerIface(&commonInterface{
		name:                  "accounts-service",
		summary:               accountsServiceSummary,
		implicitOnClassic:     true,
		reservedForOS:         true,
		baseDeclarationSlots:  accountsServiceBaseDeclarationSlots,
		connectedPlugAppArmor: accountsServiceConnectedPlugAppArmor,
	})
}
