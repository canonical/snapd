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

const systemObserveSummary = `allows observing all processes and drivers`

const systemObserveBaseDeclarationSlots = `
  system-observe:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

// http://bazaar.launchpad.net/~ubuntu-security/ubuntu-core-security/trunk/view/head:/data/apparmor/policygroups/ubuntu-core/16.04/system-observe
const systemObserveConnectedPlugAppArmor = `
# Description: Can query system status information. This is restricted because
# it gives privileged read access to all processes on the system and should
# only be used with trusted apps.

# Needed by 'ps'
@{PROC}/tty/drivers r,

# This ptrace is an information leak
ptrace (read),

# ptrace can be used to break out of the seccomp sandbox, but ps requests
# 'ptrace (trace)' even though it isn't tracing other processes. Unfortunately,
# this is due to the kernel overloading trace such that the LSMs are unable to
# distinguish between tracing other processes and other accesses. We deny the
# trace here to silence the log.
# Note: for now, explicitly deny to avoid confusion and accidentally giving
# away this dangerous access frivolously. We may conditionally deny this in the
# future.
deny ptrace (trace),

# Other miscellaneous accesses for observing the system
@{PROC}/stat r,
@{PROC}/vmstat r,
@{PROC}/diskstats r,
@{PROC}/kallsyms r,
@{PROC}/partitions r,

# These are not process-specific (/proc/*/... and /proc/*/task/*/...)
@{PROC}/*/{,task/,task/*/} r,
@{PROC}/*/{,task/*/}auxv r,
@{PROC}/*/{,task/*/}cmdline r,
@{PROC}/*/{,task/*/}exe r,
@{PROC}/*/{,task/*/}fdinfo/* r,
@{PROC}/*/{,task/*/}stat r,
@{PROC}/*/{,task/*/}statm r,
@{PROC}/*/{,task/*/}status r,

# Allow discovering the os-release of the host
/var/lib/snapd/hostfs/etc/os-release rk,
/var/lib/snapd/hostfs/usr/lib/os-release rk,

#include <abstractions/dbus-strict>

dbus (send)
    bus=system
    path=/org/freedesktop/hostname1
    interface=org.freedesktop.DBus.Properties
    member=Get{,All}
    peer=(label=unconfined),

# Allow clients to introspect hostname1
dbus (send)
    bus=system
    path=/org/freedesktop/hostname1
    interface=org.freedesktop.DBus.Introspectable
    member=Introspect
    peer=(label=unconfined),

# Allow clients to enumerate DBus connection names on common buses
dbus (send)
    bus={session,system}
    path=/org/freedesktop/DBus
    interface=org.freedesktop.DBus
    member=ListNames
    peer=(label=unconfined),

# Allow clients to obtain the DBus machine ID on common buses. We do not
# mediate the path since any peer can be used.
dbus (send)
    bus={session,system}
    interface=org.freedesktop.DBus.Peer
    member=GetMachineId
    peer=(label=unconfined),
`

const systemObserveConnectedPlugSecComp = `
# Description: Can query system status information. This is restricted because
# it gives privileged read access to all processes on the system and should
# only be used with trusted apps.

# ptrace can be used to break out of the seccomp sandbox, but ps requests
# 'ptrace (trace)' from apparmor. 'ps' does not need the ptrace syscall though,
# so we deny the ptrace here to make sure we are always safe.
# Note: may uncomment once ubuntu-core-launcher understands @deny rules and
# if/when we conditionally deny this in the future.
#@deny ptrace
`

func init() {
	registerIface(&commonInterface{
		name:                  "system-observe",
		summary:               systemObserveSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  systemObserveBaseDeclarationSlots,
		connectedPlugAppArmor: systemObserveConnectedPlugAppArmor,
		connectedPlugSecComp:  systemObserveConnectedPlugSecComp,
		reservedForOS:         true,
	})
}
