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

const coreSupportConnectedPlugAppArmor = `
# Description: Can start/stop/restart existing services via the systemd dbus API
# and enable/disable ssh. This interface gives privileged access to all snap and
# system services.

#include <abstractions/dbus-strict>

# Allows use of dbus-send. We prefer this over systemctl because systemctl
# start/stop/restart require additional rules beyond using the DBus API. Example
# usage:
#
# Via manager:
#   dbus-send --system --print-reply --dest=org.freedesktop.systemd1 \
#       /org/freedesktop/systemd1 \
#       org.freedesktop.systemd1.Manager.StartUnit \
#       string:"test.service" string:"replace"
#
# Via unit object:
#   dbus-send --system --print-reply --dest=org.freedesktop.systemd1 \
#       /org/freedesktop/systemd1/unit/test_2eservice \
#       org.freedesktop.systemd1.Unit.Start string:"replace"
#
# Finding units:
#   dbus-send --system --print-reply --dest=org.freedesktop.systemd1 \
#       /org/freedesktop/systemd1 \
#       org.freedesktop.systemd1.Manager.ListUnits
#   dbus-send --system --print-reply --dest=org.freedesktop.systemd1 \
#       /org/freedesktop/systemd1 \
#       org.freedesktop.systemd1.Manager.GetUnit \
#       string:"test.service"
#
# Properties of units:
#   dbus-send --system --print-reply --dest=org.freedesktop.systemd1 \
#       /org/freedesktop/systemd1/unit/test_2eservice \
#       org.freedesktop.DBus.Properties.GetAll
#       string:"org.freedesktop.systemd1.Unit"
#   dbus-send --system --print-reply --dest=org.freedesktop.systemd1 \
#       /org/freedesktop/systemd1/unit/test_2eservice \
#       org.freedesktop.DBus.Properties.Get \
#       string:"org.freedesktop.systemd1.Unit" string:"ActiveState"

/{,usr/}bin/dbus-send ixr,

# Allow listing units and obtaining unit names
dbus (send)
    bus=system
    path=/org/freedesktop/systemd1
    interface=org.freedesktop.systemd1.Manager
    member=ListUnits
    peer=(label=unconfined),
dbus (send)
    bus=system
    path=/org/freedesktop/systemd1
    interface=org.freedesktop.systemd1.Manager
    member=GetUnit
    peer=(label=unconfined),

# Allow connected snaps to start, stop, or restart a single
# systemd unit either via the global Manager or via the
# specific unit object.
dbus (send)
    bus=system
    path=/org/freedesktop/systemd1
    interface=org.freedesktop.systemd1.Manager
    member={Start,Stop,Restart}Unit
    peer=(label=unconfined),
dbus (send)
    bus=system
    path=/org/freedesktop/systemd1/unit/**
    interface=org.freedesktop.systemd1.Unit
    member={Start,Stop,Restart,TryRestart}
    peer=(label=unconfined),

# Allow querying for unit properties
dbus (send)
    bus=system
    path=/org/freedesktop/systemd1/unit/**
    interface=org.freedesktop.DBus.Properties
    member=Get{,All}
    peer=(label=unconfined),

# Allow creation and removal of this specific file which disables
# the system sshd service. This will be used by the configure hook
# of the core snap.
/etc/ssh/sshd_not_to_be_run rw,
`

const coreSupportConnectedPlugSecComp = `
# Description: Can control existing services via the systemd
# dbus API (start, stop, restart).
recvfrom
recvmsg
send
sendto
sendmsg
`

// NewShutdownInterface returns a new "shutdown" interface.
func NewCoreSupportInterface() interfaces.Interface {
	return &commonInterface{
		name: "core-support",
		connectedPlugAppArmor: coreSupportConnectedPlugAppArmor,
		connectedPlugSecComp:  coreSupportConnectedPlugSecComp,
		reservedForOS:         true,
	}
}
