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

const systemPowerControlConnectedPlugAppArmor = `
# Description: Can reboot, power-off and halt the system.
# Usage: reserved

#include <abstractions/dbus-strict>

dbus (send)
    bus=system
    path=/org/freedesktop/systemd1
    interface=org.freedesktop.systemd1.Manager
    member={Reboot,PowerOff,Halt}
    peer=(label=unconfined),
`

const systemPowerControlConnectedPlugSecComp = `
# Description: Can reboot, power-off and halt the system.
# Following things are needed for dbus connectivity
recvfrom
recvmsg
send
sendto
sendmsg
`

// NewSystemPowerControlInterface returns a new "system-power-control" interface.
func NewSystemPowerControlInterface() interfaces.Interface {
	return &commonInterface{
		name: "system-power-control",
		connectedPlugAppArmor: systemPowerControlConnectedPlugAppArmor,
		connectedPlugSecComp:  systemPowerControlConnectedPlugSecComp,
		reservedForOS:         true,
		autoConnect:           false,
	}
}
