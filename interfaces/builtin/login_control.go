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

const loginControlConnectedPlugAppArmor = `
# Description: Can control the login system service

#include <abstractions/dbus-strict>

# Give full access to the login service. This allows a client
# to control everything (system reboot, shutdown, suspend, ...)
dbus (send,receive)
    bus=system
    path=/org/freedesktop/login1
    interface=org.freedesktop.login1.Manager
    peer=(label=unconfined),
dbus (send,receive)
    bus=system
    path=/org/freedesktop/login1{,/**}
    interface=org.freedesktop.DBus.Properties
    peer=(label=unconfined),
`

const loginControlConnectedPlugSecComp = `
# Description: Can control the login system service

# dbus
connect
getsockname
recvfrom
recvmsg
send
sendto
sendmsg
socket
`

// NewUPowerObserveInterface returns a new "upower-observe" interface.
func NewLoginControlInterface() interfaces.Interface {
	return &commonInterface{
		name: "login-control",
		connectedPlugAppArmor: loginControlConnectedPlugAppArmor,
		connectedPlugSecComp:  loginControlConnectedPlugSecComp,
		reservedForOS:         true,
	}
}
