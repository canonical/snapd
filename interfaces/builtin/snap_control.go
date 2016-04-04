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
	"github.com/ubuntu-core/snappy/interfaces"
)

// http://bazaar.launchpad.net/~ubuntu-security/ubuntu-core-security/trunk/view/head:/data/apparmor/policygroups/ubuntu-core/16.04/snap-control
const snapControlConnectedPlugAppArmor = `
# Description: Can manage snaps via snapd.
# Usage: reserved

/run/snapd.socket rw,
`

// http://bazaar.launchpad.net/~ubuntu-security/ubuntu-core-security/trunk/view/head:/data/seccomp/policygroups/ubuntu-core/16.04/snap-control
const snapControlConnectedPlugSecComp = `
# Description: Can use snapd.
# Usage: reserved

# Can communicate with snapd abstract socket
connect
getsockname
recv
recvmsg
send
sendto
sendmsg
socket
socketpair
`

// NewSnapControlInterface returns a new "snap-control" interface.
func NewSnapControlInterface() interfaces.Interface {
	return &commonInterface{
		name: "snap-control",
		connectedPlugAppArmor: snapControlConnectedPlugAppArmor,
		connectedPlugSecComp:  snapControlConnectedPlugSecComp,
		reservedForOS:         true,
	}
}
