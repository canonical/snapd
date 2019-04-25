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

const sessionSummary = `allows setup of login session & seat`

const sessionBaseDeclarationSlots = `
  session:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
    deny-connection:
      on-classic: false
`

const sessionConnectedPlugAppArmor = `
# Description: Can setup login session & seat.

#include <abstractions/dbus-strict>

dbus (send)
    bus=system
    path=/org/freedesktop/login1/{seat,session}/*
    interface=org.freedesktop.DBus.Properties
    member=GetAll,

dbus (send)
    bus=system
    path=/org/freedesktop/login1/seat/*
    interface=org.freedesktop.login1.Seat
    member=ActiveSession
    peer=(label=unconfined),

dbus (send)
    bus=system
    path=/org/freedesktop/login1/session/*
    interface=org.freedesktop.login1.Session
    member={TakeControl,TakeDevice,PauseDevice,PauseDeviceComplete,ResumeDevice,ReleaseDevice,Active,State,Lock,Unlock}
    peer=(label=unconfined),
`

func init() {
	registerIface(&commonInterface{
		name:                  "session",
		summary:               sessionSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  sessionBaseDeclarationSlots,
		connectedPlugAppArmor: sessionConnectedPlugAppArmor,
		reservedForOS:         true,
	})
}
