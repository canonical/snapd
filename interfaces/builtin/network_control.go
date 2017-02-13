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

const networkControlConnectedPlugAppArmor = `
# Description: Can configure networking and network namespaces via the standard
# 'ip netns' command (man ip-netns(8)). This interface is restricted because it
# gives wide, privileged access to networking and should only be used with
# trusted apps.

#include <abstractions/nameservice>
#include <abstractions/ssl_certs>

capability net_admin,
capability net_raw,
capability setuid, # ping

# Allow protocols except those that we blacklist in
# /etc/modprobe.d/blacklist-rare-network.conf
network appletalk,
network bridge,
network inet,
network inet6,
network ipx,
network packet,
network pppox,
network sna,

@{PROC}/@{pid}/net/ r,
@{PROC}/@{pid}/net/** r,

# used by sysctl, et al
@{PROC}/sys/ r,
@{PROC}/sys/net/ r,
@{PROC}/sys/net/core/ r,
@{PROC}/sys/net/core/** rw,
@{PROC}/sys/net/ipv{4,6}/ r,
@{PROC}/sys/net/ipv{4,6}/** rw,
@{PROC}/sys/net/netfilter/ r,
@{PROC}/sys/net/netfilter/** rw,
@{PROC}/sys/net/nf_conntrack_max rw,

# networking tools
/{,usr/}{,s}bin/arp ixr,
/{,usr/}{,s}bin/arpd ixr,
/{,usr/}{,s}bin/bridge ixr,
/{,usr/}{,s}bin/dhclient Pxr,             # use ixr instead if want to limit to snap dirs
/{,usr/}{,s}bin/ifconfig ixr,
/{,usr/}{,s}bin/ip ixr,
/{,usr/}{,s}bin/ipmaddr ixr,
/{,usr/}{,s}bin/iptunnel ixr,
/{,usr/}{,s}bin/nameif ixr,
/{,usr/}{,s}bin/netstat ixr,              # -p not supported
/{,usr/}{,s}bin/nstat ixr,
/{,usr/}{,s}bin/ping ixr,
/{,usr/}{,s}bin/ping6 ixr,
/{,usr/}{,s}bin/pppd ixr,
/{,usr/}{,s}bin/pppdump ixr,
/{,usr/}{,s}bin/pppoe-discovery ixr,
#/{,usr/}{,s}bin/pppstats ixr,            # needs sys_module
/{,usr/}{,s}bin/route ixr,
/{,usr/}{,s}bin/routef ixr,
/{,usr/}{,s}bin/routel ixr,
/{,usr/}{,s}bin/rtacct ixr,
/{,usr/}{,s}bin/rtmon ixr,
/{,usr/}{,s}bin/sysctl ixr,
/{,usr/}{,s}bin/tc ixr,
/{,usr/}{,s}bin/wpa_action ixr,
/{,usr/}{,s}bin/wpa_cli ixr,
/{,usr/}{,s}bin/wpa_passphrase ixr,
/{,usr/}{,s}bin/wpa_supplicant ixr,

/dev/rfkill rw,

# arp
network netlink dgram,

# ip, et al
/etc/iproute2/{,*} r,
/etc/iproute2/rt_{protos,realms,scopes,tables} w,

# ping - child profile would be nice but seccomp causes problems with that
/{,usr/}{,s}bin/ping ixr,
/{,usr/}{,s}bin/ping6 ixr,
network inet raw,
network inet6 raw,

# pppd
capability setuid,
@{PROC}/@{pid}/loginuid r,
@{PROC}/@{pid}/mounts r,

# resolvconf
/sbin/resolvconf ixr,
/run/resolvconf/{,**} r,
/run/resolvconf/** w,
/etc/resolvconf/{,**} r,
/lib/resolvconf/* ix,
# Required by resolvconf
/bin/run-parts ixr,
/etc/resolvconf/update.d/* ix,

# route
/etc/networks r,

# TUN/TAP
/dev/net/tun rw,
# These are dynamically created via ioctl() on /dev/net/tun
/dev/tun[0-9]{,[0-9]*} rw,
/dev/tap[0-9]{,[0-9]*} rw,

# Network namespaces via 'ip netns'. In order to create network namespaces
# that persist outside of the process and be entered (eg, via
# 'ip netns exec ...') the ip command uses mount namespaces such that
# applications can open the /run/netns/NAME object and use it with setns(2).
# For 'ip netns exec' it will also create a mount namespace and bind mount
# network configuration files into /etc in that namespace. See man ip-netns(8)
# for details.

capability sys_admin, # for setns()
network netlink raw,

/ r,
/run/netns/ r,     # only 'r' since snap-confine will create this for us
/run/netns/* rw,
mount options=(rw, rshared) -> /run/netns/,
mount options=(rw, bind) /run/netns/ -> /run/netns/,
mount options=(rw, bind) / -> /run/netns/*,
umount /,

# 'ip netns identify <pid>' and 'ip netns pids foo'
capability sys_ptrace,
# FIXME: ptrace can be used to break out of the seccomp sandbox unless the
# kernel has 93e35efb8de45393cf61ed07f7b407629bf698ea (in 4.8+). Until this is
# the default in snappy kernels, deny but audit as a reminder to get the
# kernels patched.
audit deny ptrace (trace) peer=snap.@{SNAP_NAME}.*, # eventually by default
audit deny ptrace (trace), # for all other peers (process-control or other)

# 'ip netns exec foo /bin/sh'
mount options=(rw, rslave) /,
mount options=(rw, rslave), # LP: #1648245
umount /sys/,

# Eg, nsenter --net=/run/netns/... <command>
/{,usr/}{,s}bin/nsenter ixr,
`

const networkControlConnectedPlugSecComp = `
# Description: Can configure networking and network namespaces via the standard
# 'ip netns' command (man ip-netns(8)). This interface is restricted because it
# gives wide, privileged access to networking and should only be used with
# trusted apps.

# for ping and ping6
capset

# Network namespaces via 'ip netns'. In order to create network namespaces
# that persist outside of the process and be entered (eg, via
# 'ip netns exec ...') the ip command uses mount namespaces such that
# applications can open the /run/netns/NAME object and use it with setns(2).
# For 'ip netns exec' it will also create a mount namespace and bind mount
# network configuration files into /etc in that namespace. See man ip-netns(8)
# for details.
bind

mount
umount
umount2

unshare
setns - CLONE_NEWNET
`

// NewNetworkControlInterface returns a new "network-control" interface.
func NewNetworkControlInterface() interfaces.Interface {
	return &commonInterface{
		name: "network-control",
		connectedPlugAppArmor: networkControlConnectedPlugAppArmor,
		connectedPlugSecComp:  networkControlConnectedPlugSecComp,
		reservedForOS:         true,
	}
}
