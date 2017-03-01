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

// http://bazaar.launchpad.net/~ubuntu-security/ubuntu-core-security/trunk/view/head:/data/apparmor/policygroups/ubuntu-core/16.04/network-bind
const networkBindConnectedPlugAppArmor = `
# Description: Can access the network as a server.
#include <abstractions/nameservice>
#include <abstractions/ssl_certs>

# These probably shouldn't be something that apps should use, but this offers
# no information disclosure since the files are in the read-only part of the
# system.
/etc/hosts.deny r,
/etc/hosts.allow r,

@{PROC}/sys/net/core/somaxconn r,
@{PROC}/sys/net/ipv4/ip_local_port_range r,

# LP: #1496906: java apps need these for some reason and they leak the IPv6 IP
# addresses and routes. Until we find another way to handle them (see the bug
# for some options), we need to allow them to avoid developer confusion.
@{PROC}/@{pid}/net/if_inet6 r,
@{PROC}/@{pid}/net/ipv6_route r,

# java apps request this but seem to work fine without it. Netlink sockets
# are used to talk to kernel subsystems though and since apps run as root,
# allowing blanket access needs to be carefully considered. Kernel capabilities
# checks (which apparmor mediates) *should* be enough to keep abuse down,
# however Linux capabilities can be quite broad and there have been CVEs in
# this area. The issue is complicated because reservied policy groups like
# 'network-admin' and 'network-firewall' have legitimate use for this rule,
# however a network facing server shouldn't typically be running with these
# policy groups. LP: #1499897
# Note: for now, don't explicitly deny this noisy denial so --devmode isn't
# broken but eventually we may conditionally deny this.
#deny network netlink dgram,
`

// http://bazaar.launchpad.net/~ubuntu-security/ubuntu-core-security/trunk/view/head:/data/seccomp/policygroups/ubuntu-core/16.04/network-bind
const networkBindConnectedPlugSecComp = `
# Description: Can access the network as a server.
accept
accept4
bind
listen
shutdown
`

// NewNetworkBindInterface returns a new "network-bind" interface.
func NewNetworkBindInterface() interfaces.Interface {
	return &commonInterface{
		name: "network-bind",
		connectedPlugAppArmor: networkBindConnectedPlugAppArmor,
		connectedPlugSecComp:  networkBindConnectedPlugSecComp,
		reservedForOS:         true,
	}
}
