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

const networkObserveSummary = `allows querying network status`

const networkObserveBaseDeclarationSlots = `
  network-observe:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

// http://bazaar.launchpad.net/~ubuntu-security/ubuntu-core-security/trunk/view/head:/data/apparmor/policygroups/ubuntu-core/16.04/network-observe
const networkObserveConnectedPlugAppArmor = `
# Description: Can query network status information. This is restricted because
# it gives privileged read-only access to networking information and should
# only be used with trusted apps.

# network-observe can't allow this otherwise we are basically network-control,
# but don't explicitly deny since someone might try to use network-control with
# network-observe and that shouldn't fail weirdly
#capability net_admin,

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

# systemd-netword
#
# Allow access to listen for link property changes from systemd-netword via the D-Bus API:
#
#   https://www.freedesktop.org/software/systemd/man/latest/org.freedesktop.network1.html
#
# This can be used to run things like networkd-dispatcher, or similar, inside a snap
#
#include <abstractions/dbus-strict>
dbus receive
     bus=system
     path=/org/freedesktop/network1
     interface=org.freedesktop.DBus.Properties
     member=PropertiesChanged
     peer=(label=unconfined),

dbus receive
     bus=system
     path=/org/freedesktop/network1/link/_*
     interface=org.freedesktop.DBus.Properties
     member=PropertiesChanged
     peer=(label=unconfined),

# Allow reading systemd-networkd link properties explicitly if an app needs to query state on-demand
dbus send
     bus=system
     path=/org/freedesktop/network1/link/_*
     interface=org.freedesktop.DBus.Properties
     member=Get{,All}
     peer=(name=org.freedesktop.network1, label=unconfined),

# Allow access to read only systemd-networkd Manager objects
dbus send
     bus=system
     path=/org/freedesktop/network1
     interface=org.freedesktop.network1.Manager
     member={ListLinks,GetLinkByName,DescribeLink,Describe}
     peer=(name=org.freedesktop.network1, label=unconfined),

#include <abstractions/ssl_certs>

# see loaded kernel modules
@{PROC}/modules r,

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
/{,usr/}{,s}bin/ss ixr,
/{,usr/}{,s}bin/sysctl ixr,
/{,usr/}{,s}bin/tc ixr,

# arp
network netlink dgram,

# ip, et al
/etc/iproute2/{,**} r,

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

/etc/rpc r,

# network devices
/sys/devices/**/net/** rk,

# for receiving kobject_uevent() net messages from the kernel
network netlink raw,
`

// http://bazaar.launchpad.net/~ubuntu-security/ubuntu-core-security/trunk/view/head:/data/seccomp/policygroups/ubuntu-core/16.04/network-observe
const networkObserveConnectedPlugSecComp = `
# Description: Can query network status information. This is restricted because
# it gives privileged read-only access to networking information and should
# only be used with trusted apps.

# for ping and ping6
capset

# for using socket(AF_NETLINK, ...)
bind

# for ss
socket AF_NETLINK - NETLINK_INET_DIAG

# arp
socket AF_NETLINK - NETLINK_ROUTE

# multicast statistics
socket AF_NETLINK - NETLINK_GENERIC

# for receiving kobject_uevent() net messages from the kernel
socket AF_NETLINK - NETLINK_KOBJECT_UEVENT
`

func init() {
	registerIface(&commonInterface{
		name:                  "network-observe",
		summary:               networkObserveSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  networkObserveBaseDeclarationSlots,
		connectedPlugAppArmor: networkObserveConnectedPlugAppArmor,
		connectedPlugSecComp:  networkObserveConnectedPlugSecComp,
	})
}
