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

const passwordManagerServiceSummary = `allow access to common password manager services`

const passwordManagerBaseDeclarationSlots = `
  password-manager-service:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const passwordManagerServiceConnectedPlugAppArmor = `
# Description: Allow access to password manager services provided by popular
# Desktop Environments. This interface gives access to sensitive information
# available in the user's session.

#include <abstractions/dbus-session-strict>

# Provide full access to the secret-service API:
# - https://standards.freedesktop.org/secret-service/)
#
# The secret-service allows managing (add/delete/lock/etc) collections and
# (add/delete/etc) items within collections. The API also has the concept of
# aliases for collections which is typically used to access the default
# collection. While it would be possible for an application developer to use a
# snap-specific collection and mediate by object path, application developers
# are meant to instead to treat collections (typically the default collection)
# as a database of key/value attributes each with an associated secret that
# applications may query. Because AppArmor does not mediate member data,
# typical and recommended usage of the API does not allow for application
# isolation. For details, see:
# - https://standards.freedesktop.org/secret-service/ch03.html
#
dbus (receive, send)
    bus=session
    path=/org/freedesktop/secrets{,/**}
    interface=org.freedesktop.DBus.*
    peer=(label=unconfined),

dbus (receive, send)
    bus=session
    path=/org/freedesktop/secrets{,/**}
    interface=org.freedesktop.Secret.{Collection,Item,Prompt,Service,Session}
    peer=(label=unconfined),

# KWallet's client API is still in use in KDE/Plasma. It's DBus API relies upon
# member data for access to its 'folders' and 'entries' and is therefore does
# not allow for application isolation via AppArmor. For details, see:
# - https://cgit.kde.org/kdelibs.git/tree/kdeui/util/kwallet.h?h=v4.14.33
#
dbus (receive, send)
    bus=session
    path=/modules/kwalletd{,5}
    interface=org.freedesktop.DBus.*
    peer=(label=unconfined),

dbus (receive, send)
    bus=session
    path=/modules/kwalletd{,5}
    interface=org.kde.KWallet
    peer=(label=unconfined),
`

func init() {
	registerIface(&commonInterface{
		name:                  "password-manager-service",
		summary:               passwordManagerServiceSummary,
		implicitOnClassic:     true,
		baseDeclarationSlots:  passwordManagerBaseDeclarationSlots,
		connectedPlugAppArmor: passwordManagerServiceConnectedPlugAppArmor,
		reservedForOS:         true,
	})
}
