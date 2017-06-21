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

const passwordManagerServiceSummary = `allows interacting with the Password Manager Service`

const passwordManagerServiceConnectedPlugAppArmor = `
# Description: Allow access to the registry and storage framework services.

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
		connectedPlugAppArmor: passwordManagerServiceConnectedPlugAppArmor,
		reservedForOS:         true,
	})
}
