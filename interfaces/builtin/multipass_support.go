// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

const multipassSupportSummary = `allows operating as the Multipass service`

const multipassSupportBaseDeclarationPlugs = `
  multipass-support:
    allow-installation: false
    deny-auto-connection: true
`

const multipassSupportBaseDeclarationSlots = `
  multipass-support:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const multipassSupportConnectedPlugAppArmor = `
# Description: allow operating as the Multipass daemon.

# Multipass is a tool to create and manage Virtual Machines and their images.
# Each VM runs as a separate "qemu" process (on Linux). VM images are automatically
# downloaded, but need conversion using "qemu-img". Networking is provided by
# configuring a TUN/TAP network on the host, with DHCP provided by a shared
# "dnsmasq" process. File sharing between the VM and the host is provided by a
# "sshfs_server" utility. All these utilities are shipped in the snap.

# Each of these utilities have a very different different purpose, and an attempt
# to confine them all in a single profile would result in an extremely broad AppArmor
# profile.

# Instead we defer to Multipass the responsibility of generating custom AppArmor
# profiles for each of these utilities, and trust it launches each utility with
# all possible security mechanisms enabled. The Multipass daemon itself will run
# under this restricted policy.

# Therefore this policy intentionally allows the Multipass daemon to configure
# AppArmor.


# Multipass has a server/client design, using a socket for IPC. The daemon runs
# as root, but makes the socket accessible to anyone in the sudo group.
# Need to permit chown and chgrp of the socket.
capability chown,

# Multipass generates AppArmor profiles for the utility processes it spawns.
/sbin/apparmor_parser ixr,
/etc/apparmor{,.d}/{,**} r,
/etc/apparmor.d/{,cache/}multipass* rw,
/sys/kernel/security/apparmor/{,**} r,
/sys/kernel/security/apparmor/.remove w,
/sys/kernel/security/apparmor/.replace w,

# Allow running utility processes under the specialized AppArmor profiles.
# These profiles will prevent utility processes escaping confinement.
capability mac_admin,

# Multipass will also use privilege separation when running utility processes
capability setuid,
capability setgid,

# Some utility process (e.g. dnsmasq) will drop root privileges after startup and
# change to another user. Multipass needs ability to stop them.
capability kill,

# Profiles Multipass generates have a naming scheme, restrict any profile changes to
# those matching that scheme. Need unsafe to prevent env scrubbing.
change_profile unsafe /var/snap/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/usr/bin/* -> multipass.*,
signal (send) peer=multipass.*,
ptrace (read, trace) peer=multipass.*,
`

const multipassSupportConnectedPlugSecComp = `
# Description: allow operating as the Multipass daemon, and enable its various utilites
# (qemu, qemu-img, dnsmasq, sshfs_server)

# Note that this profile contains the union of all the necessary syscalls each of the
# utilities requires. We rely on Multipass to generate specific AppArmor profiles
# for each child process, to further restrict their abilities.

# Multipass has server/client design, using a socket for IPC.
# Note: Qemu has several features that also open sockets, but the AppArmor profile
# Multipass will execute Qemu under locks down its filesystem access dramatically
# and prevents it creating these sockets.
accept
accept4
bind
listen

# dnsmasq fails unless it can drop supplementary groups
setgroups 0 -
setgroups32 0 -

# Multipassd needs to chown its socket to a non-root user. sshfs_server also
# will need these (see later).
chown
chown32

# Multipassd will have a child process - sshfs_server - which allows mounting a
# user-specified directory on the host into the VM. Here we permit typical filesytem
# syscalls with the knowledge that Multipass will generate a specific AppArmor
# profile for sshfs_server, restricting any filesystem access to just the
# user-specified directory.

# The default seccomp profile already includes these syscalls with the note:
# "until we have syscall arg filtering (LP: #1446748)"
# Am specifying them here as sshfs_server needs these syscalls unfiltered.
chmod
fchmod
fchmodat

# More filesystem syscalls sshfs_server will need, as it allows user to change
# file owner/group arbitrarily.
fchown
fchown32
fchownat
lchown
lchownat
`

func init() {
	registerIface(&commonInterface{
		name:                  "multipass-support",
		summary:               multipassSupportSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  multipassSupportBaseDeclarationSlots,
		baseDeclarationPlugs:  multipassSupportBaseDeclarationPlugs,
		connectedPlugAppArmor: multipassSupportConnectedPlugAppArmor,
		connectedPlugSecComp:  multipassSupportConnectedPlugSecComp,
		reservedForOS:         true,
	})
}
