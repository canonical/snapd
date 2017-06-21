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

const passwordManagerServiceSummary = `allows access to password manager services`

const passwordManagerServiceDescription = `
The password-manager-service interface allows connected plugs full access to
common Desktop Environment password services (eg, gnome-keyring/secret-service
and kwallet).

The core snap provides the slot that is shared by all snaps on a classic
system. This interface gives access to sensitive information in the user's
session.
`

const passwordManagerServiceConnectedPlugAppArmor = `
# Description: Allow access to password manager services provided by popular
# Desktop Environments. This interface gives access to sensitive information in
# the user's session.

#include <abstractions/dbus-session-strict>

dbus (send)
    bus=session
    path=/org/freedesktop/secrets
    interface=org.freedesktop.DBus.Properties
    member="{Get,GetAll,PropertiesChanged}"
    peer=(label=unconfined),

dbus (receive, send)
    bus=session
    interface=org.freedesktop.Secret.*
    path=/org/freedesktop/secrets{,/**}
    peer=(label=unconfined),
`

func init() {
	registerIface(&commonInterface{
		name:                  "password-manager-service",
		summary:               passwordManagerServiceSummary,
		description:           passwordManagerServiceDescription,
		implicitOnClassic:     true,
		connectedPlugAppArmor: passwordManagerServiceConnectedPlugAppArmor,
		reservedForOS:         true,
	})
}
