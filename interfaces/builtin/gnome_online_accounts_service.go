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

const gnomeOnlineAccountsServiceSummary = `allows communication with the GNOME Online Accounts service`

const gnomeOnlineAccountsServiceBaseDeclarationSlots = `
  gnome-online-accounts-service:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const gnomeOnlineAccountsServiceConnectedPlugAppArmor = `
# Description: Allow access to GNOME Online Accounts service

#include <abstractions/dbus-session-strict>

# Allow use of ObjectManager APIs, used to enumerate accounts
dbus (receive, send)
    bus=session
    interface=org.freedesktop.DBus.ObjectManager
    path=/org/gnome/OnlineAccounts
    peer=(label=unconfined),

# Allow access to properties on accounts
dbus (receive, send)
    bus=session
    interface=org.freedesktop.DBus.Properties
    path=/org/gnome/OnlineAccounts{,/**}
    peer=(label=unconfined),

# Allow access to OnlineAccounts methods
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
		name:                  "gnome-online-accounts-service",
		summary:               gnomeOnlineAccountsServiceSummary,
		implicitOnClassic:     true,
		reservedForOS:         true,
		baseDeclarationSlots:  gnomeOnlineAccountsServiceBaseDeclarationSlots,
		connectedPlugAppArmor: gnomeOnlineAccountsServiceConnectedPlugAppArmor,
	})
}
