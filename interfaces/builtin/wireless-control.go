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
capability setgid,

network netlink dgram,
network bridge,
network packet,

@{PROC}/@{pid}/net/ r,
@{PROC}/@{pid}/net/** r,

# used by sysctl, et al
@{PROC}/sys/ r,
@{PROC}/sys/net/ r,
@{PROC}/sys/net/dev/ r,
@{PROC}/sys/net/core/ r,
@{PROC}/sys/net/core/** rw,
@{PROC}/sys/net/ipv{4,6}/ r,
@{PROC}/sys/net/ipv{4,6}/** rw,
@{PROC}/sys/net/netfilter/ r,
@{PROC}/sys/net/netfilter/** rw,
@{PROC}/sys/net/nf_conntrack_max rw,

# wireless tools
/{,usr/}{,s}bin/iw ixr,
/{,usr/}{,s}bin/iwconfig ixr,
/{,usr/}{,s}bin/iwevent ixr,
/{,usr/}{,s}bin/iwgetid ixr,
/{,usr/}{,s}bin/iiwlist ixr,
/{,usr/}{,s}bin/iwpriv ixr,
/{,usr/}{,s}bin/iwspy ixr,

/{,usr/}{,s}bin/dnsmasq ixr,
/{,usr/}{,s}bin/hostapd ixr,

/dev/rfkill r,
/sys/class/net/ r,
/sys/class/net/** r,
/bin/sync ixr,

#include <abstractions/nameservice>

# DBus accesses
#include <abstractions/dbus-strict>

# Allow access to wpa-supplicant for managing WiFi networks
dbus (receive, send)
    bus=system
    path=/fi/w1/wpa_supplicant1{,/**}
    interface=fi.w1.wpa_supplicant1*
    peer=(label=unconfined),
dbus (receive, send)
    bus=system
    path=/fi/w1/wpa_supplicant1{,/**}
    interface=org.freedesktop.DBus.*
    peer=(label=unconfined),
`
// http://bazaar.launchpad.net/~ubuntu-security/ubuntu-core-security/trunk/view/head:/data/seccomp/policygroups/ubuntu-core/16.04/wireless-control
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

chown
chown32
fchown
fchown32
fchownat
lchown
lchown32
setgroups32

`

// NewWirelessControlInterface returns a new "wireless-control" interface.
func NewWirelessControlInterface() interfaces.Interface {
	return &commonInterface{
		name: "wireless-control",
		connectedPlugAppArmor: wirelessControlConnectedPlugAppArmor,
		connectedPlugSecComp:  wirelessControlConnectedPlugSecComp,
		reservedForOS:         true,
	}
}
