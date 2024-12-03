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

import "github.com/snapcore/snapd/release"

const localeControlSummary = `allows control over system locale`

const localeControlBaseDeclarationSlots = `
  locale-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

// http://bazaar.launchpad.net/~ubuntu-security/ubuntu-core-security/trunk/view/head:/data/apparmor/policygroups/ubuntu-core/16.04/locale-control
const localeControlConnectedPlugAppArmor = `
# Description: Can manage locales directly separate from 'config ubuntu-core'.

#include <abstractions/dbus-strict>

# Introspection of org.freedesktop.locale1
dbus (send)
	bus=system
	path=/org/freedesktop/locale1
	interface=org.freedesktop.DBus.Introspectable
	member=Introspect,
# Properties of org.freedesktop.locale1
dbus (send)
	bus=system
	path=/org/freedesktop/locale1
	interface=org.freedesktop.DBus.Properties
	member=Get{,All},
# do not use peer=(label=unconfined) here since this is DBus activated
dbus (send)
	bus=system
	path=/org/freedesktop/locale1
	interface=org.freedesktop.locale1
	member={SetLocale,SetX11Keyboard,SetVConsoleKeyboard}
	peer=(name=org.freedesktop.locale1),
# Receive Accounts property changed events
dbus (receive)
	bus=system
	path=/org/freedesktop/locale1
	interface=org.freedesktop.DBus.Properties
	member=PropertiesChanged,

# TODO: this won't work until snappy exposes this configurability
/etc/default/locale rw,
`

func init() {
	registerIface(&commonInterface{
		name:                  "locale-control",
		summary:               localeControlSummary,
		implicitOnClassic:     true,
		implicitOnCore:        release.OnCoreDesktop,
		baseDeclarationSlots:  localeControlBaseDeclarationSlots,
		connectedPlugAppArmor: localeControlConnectedPlugAppArmor,
	})
}
