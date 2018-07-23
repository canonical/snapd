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

const firewallControlSummary = `allows control over network firewall`

const firewallControlBaseDeclarationSlots = `
  firewall-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

// http://bazaar.launchpad.net/~ubuntu-security/ubuntu-core-security/trunk/view/head:/data/apparmor/policygroups/ubuntu-core/16.04/firewall-control
const firewallControlConnectedPlugAppArmor = `
# Description: Can configure firewall. This is restricted because it gives
# privileged access to networking and should only be used with trusted apps.

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

capability net_admin,

/{,usr/}{,s}bin/iptables{,-save,-restore} ixr,
/{,usr/}{,s}bin/ip6tables{,-save,-restore} ixr,
/{,usr/}{,s}bin/iptables-apply ixr,
/{,usr/}{,s}bin/xtables-multi ixr, # ip[6]tables*

# ping - child profile would be nice but seccomp causes problems with that
/{,usr/}{,s}bin/ping ixr,
/{,usr/}{,s}bin/ping6 ixr,
capability net_raw,
capability setuid,
network inet raw,
network inet6 raw,

# iptables (note, we don't want to allow loading modules, but
# we can allow reading @{PROC}/sys/kernel/modprobe). Also,
# snappy needs to have iptable_filter and ip6table_filter loaded,
# they don't autoload.
unix (bind) type=stream addr="@xtables",
/{,var/}run/xtables.lock rwk,
@{PROC}/sys/kernel/modprobe r,

@{PROC}/@{pid}/net/ r,
@{PROC}/@{pid}/net/** r,

# sysctl
/{,usr/}{,s}bin/sysctl ixr,
@{PROC}/sys/ r,
@{PROC}/sys/net/ r,
@{PROC}/sys/net/core/ r,
@{PROC}/sys/net/core/** r,
@{PROC}/sys/net/ipv{4,6}/ r,
@{PROC}/sys/net/ipv{4,6}/** r,
@{PROC}/sys/net/netfilter/ r,
@{PROC}/sys/net/netfilter/** r,
@{PROC}/sys/net/nf_conntrack_max r,

# check the state of the Kmod modules
/sys/module/arp_tables/               r,
/sys/module/arp_tables/initstate      r,
/sys/module/br_netfilter/             r,
/sys/module/br_netfilter/initstate    r,
/sys/module/iptable_filter/           r,
/sys/module/iptable_filter/initstate  r,
/sys/module/ip6table_filter/          r,
/sys/module/ip6table_filter/initstate r,

# read netfilter module parameters
/sys/module/nf_*/                     r,
/sys/module/nf_*/parameters/{,*}      r,

# various firewall related sysctl files
@{PROC}/sys/net/bridge/bridge-nf-call-arptables rw,
@{PROC}/sys/net/bridge/bridge-nf-call-iptables rw,
@{PROC}/sys/net/bridge/bridge-nf-call-ip6tables rw,
@{PROC}/sys/net/bridge/bridge-nf-filter-pppoe-tagged rw,
@{PROC}/sys/net/bridge/bridge-nf-filter-vlan-tagged rw,
@{PROC}/sys/net/bridge/bridge-nf-pass-vlan-input-dev rw,
@{PROC}/sys/net/ipv4/conf/*/rp_filter w,
@{PROC}/sys/net/ipv{4,6}/conf/*/accept_source_route w,
@{PROC}/sys/net/ipv{4,6}/conf/*/accept_redirects w,
@{PROC}/sys/net/ipv4/icmp_echo_ignore_broadcasts w,
@{PROC}/sys/net/ipv4/icmp_ignore_bogus_error_responses w,
@{PROC}/sys/net/ipv4/icmp_echo_ignore_all w,
@{PROC}/sys/net/ipv4/ip_forward w,
@{PROC}/sys/net/ipv4/conf/*/log_martians w,
@{PROC}/sys/net/ipv4/tcp_syncookies w,
@{PROC}/sys/net/ipv6/conf/*/forwarding w,
@{PROC}/sys/net/netfilter/nf_conntrack_helper rw,
`

// http://bazaar.launchpad.net/~ubuntu-security/ubuntu-core-security/trunk/view/head:/data/seccomp/policygroups/ubuntu-core/16.04/firewall-control
const firewallControlConnectedPlugSecComp = `
# Description: Can configure firewall. This is restricted because it gives
# privileged access to networking and should only be used with trusted apps.

# for connecting to xtables abstract and netlink sockets
bind
socket AF_NETLINK - NETLINK_FIREWALL
socket AF_NETLINK - NETLINK_NFLOG
socket AF_NETLINK - NETLINK_NETFILTER
socket AF_NETLINK - NETLINK_IP6_FW
socket AF_NETLINK - NETLINK_ROUTE

# for ping and ping6
capset
setuid
`

var firewallControlConnectedPlugKmod = []string{
	"arp_tables",
	"br_netfilter",
	"ip6table_filter",
	"iptable_filter",
}

func init() {
	registerIface(&commonInterface{
		name:                     "firewall-control",
		summary:                  firewallControlSummary,
		implicitOnCore:           true,
		implicitOnClassic:        true,
		baseDeclarationSlots:     firewallControlBaseDeclarationSlots,
		connectedPlugAppArmor:    firewallControlConnectedPlugAppArmor,
		connectedPlugSecComp:     firewallControlConnectedPlugSecComp,
		connectedPlugKModModules: firewallControlConnectedPlugKmod,
		reservedForOS:            true,
	})
}
