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

// http://bazaar.launchpad.net/~ubuntu-security/ubuntu-core-security/trunk/view/head:/data/apparmor/policygroups/ubuntu-core/16.04/network-observe
const networkObserveConnectedPlugAppArmor = `
# Description: Can query network status information. This is restricted because
# it gives privileged read-only access to networking information and should
# only be used with trusted apps.

# network-monitor can't allow this otherwise we are basically
# network-management, but don't explicitly deny since someone might try to use
# network-management with network-monitor and that shouldn't fail weirdly
#capability net_admin,

#include <abstractions/nameservice>
#include <abstractions/ssl_certs>

@{PROC}/@{pid}/net/ r,
@{PROC}/@{pid}/net/** r,

# used by sysctl, et al (sysctl net)
@{PROC}/sys/ r,
@{PROC}/sys/net/ r,
@{PROC}/sys/net/core/ r,
@{PROC}/sys/net/core/** r,
@{PROC}/sys/net/ipv{4,6}/ r,
@{PROC}/sys/net/ipv{4,6}/** r,
@{PROC}/sys/net/netfilter/ r,
@{PROC}/sys/net/netfilter/** r,
@{PROC}/sys/net/nf_conntrack_max r,

# networking tools
/{,usr/}{,s}bin/arp ixr,
/{,usr/}{,s}bin/bridge ixr,
/{,usr/}{,s}bin/ifconfig ixr,
/{,usr/}{,s}bin/ip ixr,
/{,usr/}{,s}bin/ipmaddr ixr,
/{,usr/}{,s}bin/iptunnel ixr,
/{,usr/}{,s}bin/netstat ixr,   # -p not supported
/{,usr/}{,s}bin/nstat ixr,     # allows zeroing
#/{,usr/}{,s}bin/pppstats ixr,  # needs sys_module
/{,usr/}{,s}bin/route ixr,
/{,usr/}{,s}bin/routel ixr,
/{,usr/}{,s}bin/rtacct ixr,
/{,usr/}{,s}bin/sysctl ixr,
/{,usr/}{,s}bin/tc ixr,

# arp
network netlink dgram,

# ip, et al
/etc/iproute2/ r,
/etc/iproute2/* r,

# ping - child profile would be nice but seccomp causes problems with that
/{,usr/}{,s}bin/ping ixr,
/{,usr/}{,s}bin/ping6 ixr,
capability net_raw,
capability setuid,
network inet raw,
network inet6 raw,

# route
/etc/networks r,
/etc/ethers r,

# network devices
/sys/devices/**/net/** r,
`

// http://bazaar.launchpad.net/~ubuntu-security/ubuntu-core-security/trunk/view/head:/data/seccomp/policygroups/ubuntu-core/16.04/network-observe
const networkObserveConnectedPlugSecComp = `
# Description: Can query network status information. This is restricted because
# it gives privileged read-only access to networking information and should
# only be used with trusted apps.

# for ping and ping6
capset
`

// NewNetworkObserveInterface returns a new "network-observe" interface.
func NewNetworkObserveInterface() interfaces.Interface {
	return &commonInterface{
		name: "network-observe",
		connectedPlugAppArmor: networkObserveConnectedPlugAppArmor,
		connectedPlugSecComp:  networkObserveConnectedPlugSecComp,
		reservedForOS:         true,
	}
}
