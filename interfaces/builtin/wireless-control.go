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


const  wirelessControlConnectedPlugAppArmor = `
# Description: Can configure wireless networking. This is restricted because it gives
# wide, privileged access to wireless networking and should only be used with trusted
# apps.
# Usage: reserved

capability net_admin,
capability net_raw,

# Allow protocols except those that we blacklist in
# /etc/modprobe.d/blacklist-rare-network.conf
network appletalk,
network bridge,
network inet,
network inet6,
network ipxa,
network packet,
network pppox,
network sna,

network netlink,
network netlink raw,
network netlink dgram,

@{PROC}/@{pid}/net/ r,
@{PROC}/@{pid}/net/** r,

/dev/wl* rw,

# wireless tools
/{,usr/}{,s}bin/iw ixr,
/{,usr/}{,s}bin/iwconfig ixr,
/{,usr/}{,s}bin/iwevent ixr,
/{,usr/}{,s}bin/iwgetid ixr,
/{,usr/}{,s}bin/iiwlist ixr,
/{,usr/}{,s}bin/iwpriv ixr,
/{,usr/}{,s}bin/iwspy ixr,
`

const wirelessControlConnectedPlugSecComp = `
# Description: Can configure wireless networking. This is restricted because it gives
# wide, privileged access to wireless networking and should only be used with trusted
# apps.
# Usage: reserved

accept
accept4
bind
connect
getpeername
getsockname
getsockopt
listen
recv
recvfrom
recvmmsg
recvmsg
send
sendmmsg
sendmsg
sendto
setsockopt
shutdown
socketpair
socket


`

// NewWirekessControlInterface returns a new "wireless-control" interface.
func NewWirelessControlInterface() interfaces.Interface {
	return &commonInterface{
		name: "wireless-control",
		connectedPlugAppArmor: wirelessControlConnectedPlugAppArmor,
		connectedPlugSecComp:  wirelessControlConnectedPlugSecComp,
		reservedForOS:         true,
	}
}
