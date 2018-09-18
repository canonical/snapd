// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2018 Canonical Ltd
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

const networkSummary = `allows access to the network`

const networkBaseDeclarationSlots = `
  network:
    allow-installation:
      slot-snap-type:
        - core
`

// http://bazaar.launchpad.net/~ubuntu-security/ubuntu-core-security/trunk/view/head:/data/apparmor/policygroups/ubuntu-core/16.04/network
const networkConnectedPlugAppArmor = `
# Description: Can access the network as a client.
#include <abstractions/nameservice>
/run/systemd/resolve/stub-resolv.conf r,

# systemd-resolved (not yet included in nameservice abstraction)
#
# Allow access to the safe members of the systemd-resolved D-Bus API:
#
#   https://www.freedesktop.org/wiki/Software/systemd/resolved/
#
# This API may be used directly over the D-Bus system bus or it may be used
# indirectly via the nss-resolve plugin:
#
#   https://www.freedesktop.org/software/systemd/man/nss-resolve.html
#
#include <abstractions/dbus-strict>
dbus send
     bus=system
     path="/org/freedesktop/resolve1"
     interface="org.freedesktop.resolve1.Manager"
     member="Resolve{Address,Hostname,Record,Service}"
     peer=(name="org.freedesktop.resolve1"),

#include <abstractions/ssl_certs>

@{PROC}/sys/net/core/somaxconn r,
@{PROC}/sys/net/ipv4/tcp_fastopen r,

# Allow using netcat as client
/{,usr/}bin/nc{,.openbsd} ixr,
`

// http://bazaar.launchpad.net/~ubuntu-security/ubuntu-core-security/trunk/view/head:/data/seccomp/policygroups/ubuntu-core/16.04/network
const networkConnectedPlugSecComp = `
# Description: Can access the network as a client.
bind

# FIXME: some kernels require this with common functions in go's 'net' library.
# While this should remain in network-bind, network-control and
# network-observe, for series 16 also have it here to not break existing snaps.
# Future snapd series may remove this in the future. LP: #1689536
socket AF_NETLINK - NETLINK_ROUTE

# Userspace SCTP
# https://github.com/sctplab/usrsctp/blob/master/usrsctplib/usrsctp.h
socket AF_CONN
`

func init() {
	registerIface(&commonInterface{
		name:                  "network",
		summary:               networkSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  networkBaseDeclarationSlots,
		connectedPlugAppArmor: networkConnectedPlugAppArmor,
		connectedPlugSecComp:  networkConnectedPlugSecComp,
		reservedForOS:         true,
	})
}
