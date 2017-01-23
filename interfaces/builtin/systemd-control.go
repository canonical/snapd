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

import (
	"github.com/snapcore/snapd/interfaces"
)

const systemdControlConnectedPlugAppArmor = `
# Description: Can control existing services via the systemd
# dbus API (start, stop, restart).

#include <abstractions/dbus-strict>

# Allow connected snaps to start, stop, or restart a single
# systemd unit either via the global Manager or via the
# specific unit object.
dbus (send)
    bus=system
    path=/org/freedesktop/systemd1
    interface=org.freedesktop.systemd1.Manager
    member={StartUnit,StopUnit,RestartUnit}
    peer=(label=unconfined),
dbus (send)
    bus=system
    path=/org/freedesktop/systemd1/unit/**
    interface=org.freedesktop.systemd1.Unit
    member={Start,Stop,Restart,TryRestart}
    peer=(label=unconfined),

# Allow connected snaps to retrieve all or single properties
# of all available systemd units.
dbus (send)
    bus=system
    path=/org/freedesktop/systemd1/unit/**
    interface=org.freedesktop.DBus.Properties
    member=Get{,All}
    peer=(label=unconfined),
`

const systemdControlConnectedPlugSecComp = `
# Description: Can control existing services via the systemd
# dbus API (start, stop, restart).
recvfrom
recvmsg
send
sendto
sendmsg
`

// NewShutdownInterface returns a new "shutdown" interface.
func NewSystemdControlInterface() interfaces.Interface {
	return &commonInterface{
		name: "systemd-control",
		connectedPlugAppArmor: systemdControlConnectedPlugAppArmor,
		connectedPlugSecComp:  systemdControlConnectedPlugSecComp,
		reservedForOS:         true,
	}
}
