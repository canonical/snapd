// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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
# Description: allow operating as the Multipass daemon. This policy intentionally
# allows the Multipass daemon to configure AppArmor, and we expect Multipass to
# create and apply correct policies for the child processes it spawns.

# Allow socket
/run/multipass_socket  rw,
capability chown,
capability fsetid,

# Multipass generates AppArmor profiles for the child processes it spawns
# Allow it to add these profiles and run the child under that profile.
/usr/sbin/aa-exec ixr,
/sbin/apparmor_parser ixr,
/etc/apparmor{,.d}/{,**} r,
/etc/apparmor.d/{,cache/}multipass* rw,
/sys/kernel/security/apparmor/{,**} r,
/sys/kernel/security/apparmor/.replace w,
/sys/kernel/security/apparmor/.remove w,

capability net_admin,
capability mac_admin,
capability kill, # multipass needs to be able to kill child processes

# Profiles Multipass generates have a naming scheme. Need unsafe to prevent env scrubbing
change_profile unsafe /var/snap/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/usr/bin/* -> multipass.*,
signal (send) peer=multipass.*,
ptrace (read, trace) peer=multipass.*,
`

const multipassSupportConnectedPlugSecComp = `
# Description: allow operating as the Multipass daemon, and enable its various child
# processes (qemu, qemu-img, dnsmasq)

# Multipassd needs these. Qemu also could use them, but multipass will execute Qemu
# with a custom profile that will lock down its filesystem access.
accept
accept4
bind
listen

# dnsmasq fails unless it can drop supplementary groups
setgroups 0 -
setgroups32 0 -

# multipassd needs to chown its socket to a non-root user
chown
chown32
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
