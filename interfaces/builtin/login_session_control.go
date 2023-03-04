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

const loginSessionControlSummary = `allows setup of login session & seat`

const loginSessionControlBaseDeclarationSlots = `
  login-session-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const loginSessionControlConnectedPlugAppArmor = `
# Description: Can setup login session & seat. This grants privileged access to user sessions.

#include <abstractions/dbus-strict>

dbus (send,receive)
    bus=system
    path=/org/freedesktop/login1/{seat,session}/**
    interface=org.freedesktop.DBus.Properties
    member={GetAll,PropertiesChanged,Get}
    peer=(label=unconfined),

dbus (send,receive)
    bus=system
    path=/org/freedesktop/login1/seat/**
    interface=org.freedesktop.login1.Seat
    peer=(label=unconfined),

dbus (send,receive)
    bus=system
    path=/org/freedesktop/login1/session/**
    interface=org.freedesktop.login1.Session
    peer=(label=unconfined),

dbus (send,receive)
    bus=system
    path=/org/freedesktop/login1
    interface=org.freedesktop.login1.Manager
    member={ActivateSession,GetSession,GetSeat,KillSession,ListSessions,LockSession,TerminateSession,UnlockSession}
    peer=(label=unconfined),
`

func init() {
	registerIface(&commonInterface{
		name:                  "login-session-control",
		summary:               loginSessionControlSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  loginSessionControlBaseDeclarationSlots,
		connectedPlugAppArmor: loginSessionControlConnectedPlugAppArmor,
	})
}
