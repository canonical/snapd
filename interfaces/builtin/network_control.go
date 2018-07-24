// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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

const networkControlSummary = `allows configuring networking and network namespaces`

const networkControlBaseDeclarationSlots = `
  network-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const networkControlConnectedPlugAppArmor = `
# Description: Can configure networking and network namespaces via the standard
# 'ip netns' command (man ip-netns(8)). This interface is restricted because it
# gives wide, privileged access to networking and should only be used with
# trusted apps.

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

# For advanced wireless configuration
/sys/kernel/debug/ieee80211/ r,
/sys/kernel/debug/ieee80211/** rw,

# read netfilter module parameters
/sys/module/nf_*/                r,
/sys/module/nf_*/parameters/{,*} r,

# networking tools
/{,usr/}{,s}bin/arp ixr,
/{,usr/}{,s}bin/arpd ixr,
/{,usr/}{,s}bin/bridge ixr,
/{,usr/}{,s}bin/dhclient Pxr,             # use ixr instead if want to limit to snap dirs
/{,usr/}{,s}bin/dhclient-script ixr,
/{,usr/}{,s}bin/ifconfig ixr,
/{,usr/}{,s}bin/ifdown ixr,
/{,usr/}{,s}bin/ifquery ixr,
/{,usr/}{,s}bin/ifup ixr,
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
/{,usr/}{,s}bin/ss ixr,
/{,usr/}{,s}bin/sysctl ixr,
/{,usr/}{,s}bin/tc ixr,
/{,usr/}{,s}bin/wpa_action ixr,
/{,usr/}{,s}bin/wpa_cli ixr,
/{,usr/}{,s}bin/wpa_passphrase ixr,
/{,usr/}{,s}bin/wpa_supplicant ixr,

/dev/rfkill rw,
/sys/class/rfkill/ r,
/sys/devices/{pci[0-9a-f]*,platform,virtual}/**/rfkill[0-9]*/{,**} r,
/sys/devices/{pci[0-9a-f]*,platform,virtual}/**/rfkill[0-9]*/state w,

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

# wpa_suplicant
/{,var/}run/wpa_supplicant/** rw,
/etc/wpa_supplicant/{,**} ixr,

#ifup,ifdown, dhclient
/{,var/}run/dhclient.*.pid rw,
/var/lib/dhcp/ r,
/var/lib/dhcp/** rw,

/run/network/ifstate* rw,
/run/network/.ifstate* rw,
/run/network/ifup-* rw,
/run/network/ifdown-* rw,

# route
/etc/networks r,
/etc/ethers r,

/etc/rpc r,

# TUN/TAP - https://www.kernel.org/doc/Documentation/networking/tuntap.txt
#
# We only need to tag /dev/net/tun since the tap[0-9]* and tun[0-9]* devices
# are virtual and don't show up in /dev
/dev/net/tun rw,

# access to bridge sysfs interfaces for bridge settings
/sys/devices/virtual/net/*/bridge/* rw,

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

# For various network related netlink sockets
socket AF_NETLINK - NETLINK_ROUTE
socket AF_NETLINK - NETLINK_FIB_LOOKUP
socket AF_NETLINK - NETLINK_INET_DIAG
socket AF_NETLINK - NETLINK_XFRM
socket AF_NETLINK - NETLINK_DNRTMSG
socket AF_NETLINK - NETLINK_ISCSI
socket AF_NETLINK - NETLINK_RDMA
socket AF_NETLINK - NETLINK_GENERIC

# for receiving kobject_uevent() net messages from the kernel
socket AF_NETLINK - NETLINK_KOBJECT_UEVENT
`

/* https://www.kernel.org/doc/Documentation/networking/tuntap.txt
 *
 * We only need to tag /dev/net/tun since the tap[0-9]* and tun[0-9]* devices
 * are virtual and don't show up in /dev
 */
var networkControlConnectedPlugUDev = []string{
	`KERNEL=="rfkill"`,
	`KERNEL=="tun"`,
}

func init() {
	registerIface(&commonInterface{
		name:                  "network-control",
		summary:               networkControlSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  networkControlBaseDeclarationSlots,
		connectedPlugAppArmor: networkControlConnectedPlugAppArmor,
		connectedPlugSecComp:  networkControlConnectedPlugSecComp,
		connectedPlugUDev:     networkControlConnectedPlugUDev,
		reservedForOS:         true,
	})

}
