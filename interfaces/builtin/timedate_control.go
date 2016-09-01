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

import "github.com/snapcore/snapd/interfaces"

const timeDateControlConnectedPlugAppArmor = `
# Description: Allows configuration of time, date and timezone via systemd' timedated D-Bus interface:
# https://www.freedesktop.org/wiki/Software/systemd/timedated/
# Usage: reserved

#include <abstractions/dbus-strict>

# org.freedesktop.timedate1
dbus (send)
    bus=system
    path=/org/freedesktop/timedate1
    interface=org.freedesktop.DBus.Introspectable
    member=Introspect
    peer=(label=unconfined),

dbus (send)
    bus=system
    path=/org/freedesktop/timedate1
    interface=org.freedesktop.timedate1
    member="Set{Time,Timezone,LocalRTC,NTP}"
    peer=(label=unconfined),

dbus (receive, send)
    bus=system
    path=/org/freedesktop/timedate1
    interface=org.freedesktop.DBus.Properties
    member="{Get,GetAll,PropertiesChanged}"
`
const timeDateControlConnectedPlugSecComp = `
# dbus
connect
getsockname
recvmsg
send
sendto
sendmsg
socket
`

// NewTimeDateControlInterface returns a new "time-date-control" interface.
func NewTimeDateControlInterface() interfaces.Interface {
	return &commonInterface{
		name: "time-date-control",
		connectedPlugAppArmor: timeDateControlConnectedPlugAppArmor,
		connectedPlugSecComp:  timeDateControlConnectedPlugSecComp,
		reservedForOS:         true,
		autoConnect:           false,
	}
}
