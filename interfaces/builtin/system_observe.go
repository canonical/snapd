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

const systemObserveConnectedPlugAppArmor = `
# Description: Can query system status information. This is restricted because
# it gives privileged read access to all processes on the system and should
# only be used with trusted apps.

# Needed by 'ps'
@{PROC}/tty/drivers r,

# This ptrace is an information leak. Intentionlly omit 'ptrace (trace)' here
# since since ps doesn't actually need to trace other processes. Note this
# allows a number of accesses (assuming the associated /proc file is allowed),
# such as various memory address locations and esp/eip via /proc/*/stat,
# /proc/*/mem, /proc/*/personality, /proc/*/stack, /proc/*/syscall,
# /proc/*/timerslack_ns and /proc/*/wchan (see man proc).
#
# Some files like /proc/kallsyms (but anything using %pK format specifier) need
# 'capability syslog' when /proc/sys/kernel/kptr_restrict=1, but we
# intentionally do not allow since it could be used to defeat KASLR.
ptrace (read),

# Other miscellaneous accesses for observing the system
@{PROC}/locks r,
@{PROC}/modules r,
@{PROC}/stat r,
@{PROC}/vmstat r,
@{PROC}/zoneinfo r,
@{PROC}/diskstats r,
@{PROC}/kallsyms r,
@{PROC}/partitions r,
@{PROC}/pressure/cpu r,
@{PROC}/pressure/io r,
@{PROC}/pressure/memory r,
@{PROC}/sys/kernel/panic r,
@{PROC}/sys/kernel/panic_on_oops r,
@{PROC}/sys/vm/max_map_count r,
@{PROC}/sys/vm/panic_on_oom r,

# These are not process-specific (/proc/*/... and /proc/*/task/*/...)
@{PROC}/*/{,task/,task/*/} r,
@{PROC}/*/{,task/*/}auxv r,
@{PROC}/*/{,task/*/}cgroup r,
@{PROC}/*/{,task/*/}cmdline r,
@{PROC}/*/{,task/*/}comm r,
@{PROC}/*/{,task/*/}exe r,
@{PROC}/*/{,task/*/}fdinfo/* r,
@{PROC}/*/{,task/*/}oom_score r,
# allow reading of smaps_rollup, which is a summary of the memory use of a process,
# but not smaps which contains a detailed mappings breakdown like
# /proc/self/maps, which we do not allow access to for other processes
@{PROC}/*/{,task/*/}smaps_rollup r,
@{PROC}/*/{,task/*/}stat r,
@{PROC}/*/{,task/*/}statm r,
@{PROC}/*/{,task/*/}status r,
@{PROC}/*/{,task/*/}wchan r,

# Allow discovering the os-release of the host
/var/lib/snapd/hostfs/etc/os-release rk,
/var/lib/snapd/hostfs/usr/lib/os-release rk,

# Allow discovering system-wide CFS Bandwidth Control information
# https://www.kernel.org/doc/html/latest/scheduler/sched-bwc.html
/sys/fs/cgroup/cpu,cpuacct/cpu.cfs_period_us r,
/sys/fs/cgroup/cpu,cpuacct/cpu.cfs_quota_us r,
/sys/fs/cgroup/cpu,cpuacct/cpu.shares r,
/sys/fs/cgroup/cpu,cpuacct/cpu.stat r,
/sys/fs/cgroup/memory/memory.stat r,

#include <abstractions/dbus-strict>

# do not use peer=(label=unconfined) here since this is DBus activated
dbus (send)
    bus=system
    path=/org/freedesktop/hostname1
    interface=org.freedesktop.DBus.Properties
    member=Get{,All},

# Allow clients to introspect hostname1
# do not use peer=(label=unconfined) here since this is DBus activated
dbus (send)
    bus=system
    path=/org/freedesktop/hostname1
    interface=org.freedesktop.DBus.Introspectable
    member=Introspect,

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

# Allow reading if protected hardlinks are enabled, but don't allow enabling or
# disabling them
@{PROC}/sys/fs/protected_hardlinks r,
@{PROC}/sys/fs/protected_symlinks r,
@{PROC}/sys/fs/protected_fifos r,
@{PROC}/sys/fs/protected_regular r,
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
		suppressPtraceTrace:   true,
	})
}
