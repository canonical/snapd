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
@{PROC}/meminfo r,

# These are not process-specific (/proc/*/... and /proc/*/task/*/...)
@{PROC}/*/{,task/,task/*/} r,
@{PROC}/*/{,task/*/}auxv r,
@{PROC}/*/{,task/*/}cmdline r,
@{PROC}/*/{,task/*/}exe r,
@{PROC}/*/{,task/*/}stat r,
@{PROC}/*/{,task/*/}statm r,
@{PROC}/*/{,task/*/}status r,

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

// NewSystemObserveInterface returns a new "system-observe" interface.
func NewSystemObserveInterface() interfaces.Interface {
	return &commonInterface{
		name: "system-observe",
		connectedPlugAppArmor: systemObserveConnectedPlugAppArmor,
		connectedPlugSecComp:  systemObserveConnectedPlugSecComp,
		reservedForOS:         true,
	}
}
