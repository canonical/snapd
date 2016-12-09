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

const networkNamespaceControlConnectedPlugAppArmor = `
# Description: Can configure network namespaces via the standard 'ip netns'
# command (man ip-netns(8)). In order to create network namespaces that
# persist outside of the process and be entered (eg, via 'ip netns exec ...')
# the ip command uses mount namespaces such that applications can
# open the /run/netns/NAME object and use it with setns(2). For 'ip netns exec'
# it will also create a mount namespace and bind mount network configuration
# files into /etc in that namespace. See man ip-netns(8) for details.

# 'ip netns add/delete foo'
/{,usr/}{,s}bin/ip ixr,

capability sys_admin,
network netlink raw,

/ r,
/run/netns/ r,     # only 'r' since snap-confine will create this for us
/run/netns/* rw,
mount options=(rw, rshared) -> /run/netns/,
mount options=(rw, bind) /run/netns/ -> /run/netns/,
mount options=(rw, bind) / -> /run/netns/*,
umount /,

# 'ip netns set foo bar'
capability net_admin,

# 'ip netns identify <pid>' and 'ip netns pids foo'
capability sys_ptrace,
# FIXME: ptrace can be used to break out of the seccomp sandbox unless the
# kernel has 93e35efb8de45393cf61ed07f7b407629bf698ea (in 4.8+). Until this is
# the default in snappy kernels, deny but audit as a reminder to get the
# kernels patched.
audit deny ptrace (trace) peer=snap.@{SNAP_NAME}.*,
audit deny ptrace (trace), # for all other peers

# 'ip netns exec foo /bin/sh'
mount options=(rw, rslave) /,
mount options=(rw, rslave), # LP: #1648245
umount /sys/,

# Eg, nsenter --net=/run/netns/... <command>
/{,usr/}{,s}bin/nsenter ixr,
`

const networkNamespaceControlConnectedPlugSecComp = `
# Description: Can configure network namespaces via the standard 'ip netns'
# command (man ip-netns(8)). In order to create network namespaces that
# persist outside of the process and be entered (eg, via 'ip netns exec ...')
# the ip command uses mount namespaces such that applications can
# open the /run/netns/NAME object and use it with setns(2). For 'ip netns exec'
# it will also create a mount namespace and bind mount network configuration
# files into /etc in that namespace. See man ip-netns(8) for details.

bind
sendmsg
sendto
recvfrom
recvmsg

mount
umount
umount2

unshare
setns - CLONE_NEWNET
`

// NewNetworkNamespaceControlInterface returns a new "network-namespace-control" interface.
func NewNetworkNamespaceControlInterface() interfaces.Interface {
	return &commonInterface{
		name: "network-namespace-control",
		connectedPlugAppArmor: networkNamespaceControlConnectedPlugAppArmor,
		connectedPlugSecComp:  networkNamespaceControlConnectedPlugSecComp,
		reservedForOS:         true,
	}
}
