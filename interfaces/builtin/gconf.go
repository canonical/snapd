// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

const gconfSummary = `allows access to any item from the legacy gconf configuration system for the current user`

// Manually connected since gconf is a global database for GNOME desktop and
// application settings and offers no application isolation. Modern
// applications should use dconf/gsettings instead and this interface is
// provided for old codebases that cannot be migrated.
const gconfBaseDeclarationSlots = `
  gconf:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const gconfConnectedPlugAppArmor = `
# Description: Can access gconf databases from the user's session.

#include <abstractions/dbus-session-strict>

# gconf_client_get_default() is used by all applications and will autostart
# gconfd-2, but don't require label=unconfined since AssumedAppArmorLabel may
# not be set. Once started, gconfd-2 remains running so the other APIs can use
# label=unconfined. See gconf/gconf-dbus-utils.h
dbus (send)
    bus=session
    path=/org/gnome/GConf/Server
    member=Get{,Default}Database
    peer=(name=org.gnome.GConf),

# receive notifications and server messages
dbus (receive)
    bus=session
    path=/org/gnome/GConf/{Client,Server}
    interface=org.gnome.GConf.{Client,Server}
    peer=(label=unconfined),

# allow all operations on the database
dbus (send)
    bus=session
    path=/org/gnome/GConf/Database/*
    interface=org.gnome.GConf.Database
    peer=(label=unconfined),
`

func init() {
	registerIface(&commonInterface{
		name:                  "gconf",
		summary:               gconfSummary,
		implicitOnClassic:     true,
		connectedPlugAppArmor: gconfConnectedPlugAppArmor,
		baseDeclarationSlots:  gconfBaseDeclarationSlots,
	})
}
