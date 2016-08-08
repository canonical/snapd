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
# Description: Can manage systemd.
# Usage: reserved

# FIXME: Decide if we want to allow snaps to reuse the systemctl command from
# the core snap or if every snap should ship its own.

# Allow to use the systemctl command from the core snap
/bin/systemctl ix,
# Those are just symlinks to the systemctl command
/bin/reboot ix,
/bin/halt ix,
/bin/shutdown ix,

# As systemctl uses dbus we need to allow dbus communication
# with the systemd daemon here
#include <abstractions/dbus-strict>

dbus (send)
    bus=system
    peer=(name=org.freedesktop.systemd1, label=unconfined),

dbus (receive)
    bus=system
    path=/org/freedesktop/systemd1{,/**}
    interface=org.freedesktop.DBus.*
    peer=(label=unconfined),

dbus (receive)
    bus=system
    path=/org/freedesktop/systemd1{,/**}
    interface=org.freedesktop.systemd1.*
    peer=(label=unconfined),

# systemctl reads cgroup information for status reports
/sys/fs/cgroup/systemd{,/**} r,

# Need by systemctl to read cmdline each process in the status
# report is executed with
/proc/*/cmdline r,
`

const systemdControlConnectedPlugSeccomp = `
# Description: Can manage systemd.
# Usage: reserved

# systemctl needs those
getsockopt
setsockopt

# dbus
connect
getsockname
recvmsg
send
sendto
sendmsg
socket
`

// NewSystemdControlInterface returns a new "systemd-control" interface.
func NewSystemdControlInterface() interfaces.Interface {
	return &commonInterface{
		name: "systemd-control",
		connectedPlugAppArmor: systemdControlConnectedPlugAppArmor,
		connectedPlugSecComp:  systemdControlConnectedPlugSeccomp,
		reservedForOS:         true,
	}
}
