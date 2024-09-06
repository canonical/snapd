// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

const debuggerSupportSummary = `allows certain debugger operations`

const debuggerSupportBaseDeclarationSlots = `
  debugger-support:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const debuggerSupportConnectedPlugAppArmor = `
# Description: AppArmor rules needed for the debugger-support interface.

# Basic support needed for accessing dynamic linker.
#include <abstractions/base>

capability bpf,
capability net_admin,
capability perfmon,
capability sys_admin,
capability sys_resource,

/etc/ssl/certs/** r,

/run/dbus/system_bus_socket rw,

/usr/share/ca-certificates/** r,
/sys/firmware/dmi/tables/DMI r,
/sys/devices/virtual/dmi/id/* r,
/sys/fs/cgroup/system.slice/snap.parca-agent.parca-agent-svc.service/cpu.max r,
/sys/kernel/btf/vmlinux r,

`

const debuggerSupportConnectedPlugSecComp = `

ptrace

access
arch_prctl
brk
capset
clock_gettime
clone
close
epoll_create1
epoll_ctl
execve
exit_group
fchdir
fcntl
fstat
futex
getcwd
getegid
geteuid
getgid
getpid
getppid
getrlimit
gettid
getuid
getxattr
madvise
mmap
mprotect
munmap
nanosleep
newfstatat
open
openat
pipe2
pread64
prlimit64
read
readlink
readlinkat
rt_sigaction
rt_sigprocmask
sched_getaffinity
seccomp
setrlimit
sigaltstack
stat
statx
umask
unlink
write

accept4
bind
bpf
listen
perf_event_open

`

func init() {
	registerIface(&commonInterface{
		name:                  "debugger-support",
		summary:               debuggerSupportSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  debuggerSupportBaseDeclarationSlots,
		connectedPlugAppArmor: debuggerSupportConnectedPlugAppArmor,
		connectedPlugSecComp:  debuggerSupportConnectedPlugSecComp,
	})
}
