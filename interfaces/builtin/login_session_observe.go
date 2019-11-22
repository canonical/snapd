// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

const loginSessionObserveSummary = `allows observing login and session information`

const loginSessionObserveBaseDeclarationSlots = `
  login-session-observe:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const loginSessionObserveConnectedPlugAppArmor = `
# Allow observing login and session information
/{,usr/}bin/who  ixr,
/var/log/wtmp    rk,
/{,var/}run/utmp rk,

/{,usr/}bin/lastlog ixr,
/var/log/lastlog rk,

/{,usr/}bin/faillog ixr,
/var/log/faillog rk,

# systemd session information (session files, but not .ref files)
/run/systemd/sessions/ r,
/run/systemd/sessions/*[0-9] rk,

# Supported loginctl commands:
# - list-sessions
# - show-session N
# - list-users
# - show-user N
# - list-seats
# - show-seat N

/{,usr/}bin/loginctl ixr,
#include <abstractions/dbus-strict>

# Introspection of org.freedesktop.login1
# do not use peer=(label=unconfined) here since this is DBus activated
dbus (send)
    bus=system
    path=/org/freedesktop/login1
    interface=org.freedesktop.DBus.Introspectable
    member=Introspect,

dbus (send)
    bus=system
    path=/org/freedesktop/login1{,/seat/*,/session/*,/user/*}
    interface=org.freedesktop.DBus.Properties
    member=Get{,All},

dbus (receive)
    bus=system
    path=/org/freedesktop/login1
    interface=org.freedesktop.DBus.Properties
    member=PropertiesChanged
    peer=(label=unconfined),

dbus (send)
    bus=system
    path=/org/freedesktop/login1
    interface=org.freedesktop.login1.Manager
    member=List{Seats,Sessions,Users},

dbus (send)
    bus=system
    path=/org/freedesktop/login1
    interface=org.freedesktop.login1.Manager
    member=Get{Seat,Session,User},
`

type loginSessionObserveInterface struct {
	commonInterface
	secCompSnippet string
}

func init() {
	registerIface(&loginSessionObserveInterface{commonInterface: commonInterface{
		name:                  "login-session-observe",
		summary:               loginSessionObserveSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  loginSessionObserveBaseDeclarationSlots,
		connectedPlugAppArmor: loginSessionObserveConnectedPlugAppArmor,
		reservedForOS:         true,
	}})
}
