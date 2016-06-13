// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (c) 2016 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
 * GNU General Public License for more dtails.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.
 *
 */

package builtin

import (
	"github.com/snapcore/snapd/interfaces"
)

var mirPermanentSlotAppArmor = []byte(`
# Description: Allow operating as the Mir server. Reserved because this
# gives privileged access to the system.
# Usage: reserved


capability dac_override,
capability sys_tty_config,
capability sys_admin,

unix (receive, send) type=seqpacket addr=none,
/dev/dri/card0 rw,
/dev/shm/\#* rw,

/sys/devices/**/uevent rw,
/sys/devices/**/ r,
/dev/input/* rw,
/dev/tty* wr,
/run/udev/data/* r,
/run/udev/** rw,

/bin/sleep mrix,
/bin/pidof mrix,
/bin/sed mrix,
/bin/cp mrix,
/sbin/killall5 ixr,
/usr/bin/expr ixr,
/usr/bin/chmod ixr,
/bin/chmod ixr,
/proc/ r,
/proc/*/stat r,
/proc/*/cmdline r,
/sys/bus/ r,
/sys/class/ r,
/sys/class/input/ r,
/sys/class/drm/ r,
/etc/udev/udev.conf r,
capability chown,
capability fowner,

network netlink raw,
/run/mir_socket rw,
`)

var mirPermanentSlotSecComp = []byte(`
# Description: Allow operating as the mir service. Reserved because this
# gives privileged access to the system.
access
accept
faccessat

alarm
brk
bind
# ARM private syscalls
breakpoint
cacheflush
set_tls
usr26
usr32

capget

chdir
fchdir

# We can't effectively block file perms due to open() with O_CREAT, so allow
# chmod until we have syscall arg filtering (LP: #1446748)
chmod
fchmod
fchmodat

# snappy doesn't currently support per-app UID/GIDs so don't allow chown. To
# properly support chown, we need to have syscall arg filtering (LP: #1446748)
# and per-app UID/GIDs.
#chown
#chown32
#fchown
#fchown32
#fchownat

# needed for chmod'ing the mir socket so apps can use
lchown
#lchown32

clock_getres
clock_gettime
clock_nanosleep
clone
close
connect
creat
dup
dup2
dup3
epoll_create
epoll_create1
epoll_ctl
epoll_ctl_old
epoll_pwait
epoll_wait
epoll_wait_old
eventfd
eventfd2
execve
execveat
_exit
exit
exit_group
fallocate

# requires CAP_SYS_ADMIN
#fanotify_init
#fanotify_mark

fcntl
fcntl64
flock
fork
ftime
futex
get_mempolicy
get_robust_list
get_thread_area
getcpu
getcwd
getdents
getdents64
getegid
getegid32
geteuid
geteuid32
getgid
getgid32
getgroups
getgroups32
getitimer
getpgid
getpgrp
getpid
getppid
getpriority
getrandom
getresgid
getresgid32
getresuid
getresuid32

getrlimit
ugetrlimit

getrusage
getsid
getsockname
getsockopt
gettid
gettimeofday
getuid
getuid32

getxattr
fgetxattr
lgetxattr

inotify_add_watch
inotify_init
inotify_init1
inotify_rm_watch

# Needed by shell
ioctl

io_cancel
io_destroy
io_getevents
io_setup
io_submit
ioprio_get
# affects other processes, requires CAP_SYS_ADMIN. Potentially allow with
# syscall filtering of (at least) IOPRIO_WHO_USER (LP: #1446748)
#ioprio_set

ipc
kill
link
linkat
listen
listxattr
llistxattr
flistxattr

lseek
llseek
_llseek
lstat
lstat64

madvise
fadvise64
fadvise64_64
arm_fadvise64_64

mbind
mbarrier
mincore
mkdir
mkdirat
mlock
mlockall
mmap
mmap2
mprotect

# LP: #1448184 - these aren't currently mediated by AppArmor. Deny for now
#mq_getsetattr
#mq_notify
#mq_open
#mq_timedreceive
#mq_timedsend
#mq_unlink

mremap
msgctl
msgget
msgrcv
msgsnd
msync
munlock
munlockall
munmap

nanosleep

# LP: #1446748 - deny until we have syscall arg filtering. Alternatively, set
# RLIMIT_NICE hard limit for apps, launch them under an appropriate nice value
# and allow this call
#nice

# LP: #1446748 - support syscall arg filtering for mode_t with O_CREAT
open

openat
pause
pipe
pipe2
poll
ppoll

# LP: #1446748 - support syscall arg filtering
prctl
arch_prctl

read
pread
pread64
preadv
readv

readahead
readdir
readlink
readlinkat
recvmsg
remap_file_pages

removexattr
fremovexattr
lremovexattr

rename
renameat
renameat2

# The man page says this shouldn't be needed, but we've seen denials for it
# in the wild
restart_syscall

rmdir
rt_sigaction
rt_sigpending
rt_sigprocmask
rt_sigqueueinfo
rt_sigreturn
rt_sigsuspend
rt_sigtimedwait
rt_tgsigqueueinfo
sched_getaffinity
sched_getattr
sched_getparam
sched_get_priority_max
sched_get_priority_min
sched_getscheduler
sched_rr_get_interval
# LP: #1446748 - when support syscall arg filtering, enforce pid_t is 0 so the
# app may only change its own scheduler
sched_setscheduler

sched_yield

select
_newselect
pselect
pselect6

semctl
semget
semop
semtimedop
sendfile
sendfile64
sendmsg
sendto

# These break isolation but are common and can't be mediated at the seccomp
# level with arg filtering
setpgid
setpgrp

set_thread_area
setitimer

# apps don't have CAP_SYS_RESOURCE so these can't be abused to raise the hard
# limits
setrlimit
prlimit64

set_mempolicy
set_robust_list
setsid
set_tid_address
setsockopt
setxattr
fsetxattr
lsetxattr

shmat
shmctl
shmdt
shmget
signal
sigaction
signalfd
signalfd4
sigaltstack
sigpending
sigprocmask
sigreturn
sigsuspend
sigtimedwait
sigwaitinfo
socket
socketpair
splice

stat
stat64
fstat
fstat64
fstatat64
lstat
newfstatat
oldfstat
oldlstat
oldstat

statfs
statfs64
fstatfs
fstatfs64
statvfs
fstatvfs
ustat

symlink
symlinkat

sync
sync_file_range
sync_file_range2
arm_sync_file_range
fdatasync
fsync
syncfs
sysinfo
syslog
tee
tgkill
time
timer_create
timer_delete
timer_getoverrun
timer_gettime
timer_settime
timerfd_create
timerfd_gettime
timerfd_settime
times
tkill

truncate
truncate64
ftruncate
ftruncate64

umask

uname
olduname
oldolduname

unlink
unlinkat

utime
utimensat
utimes
futimesat

vfork
vmsplice
wait4
oldwait4
waitpid
waitid

write
writev
pwrite
pwrite64
pwritev
`)

var mirConnectedPlugAppArmor = []byte(`
# Description: Allow use of the Mir server. Reserved because this
# gives privileged access to the system.
# Usage: reserved

`)

var mirConnectedPlugSecComp = []byte(`
# Description: Allow operating as the mir service. Reserved because this
# gives privileged access to the system.

`)

type MirInterface struct{}

func (iface *MirInterface) Name() string {
	return "mir"
}

func (iface *MirInterface) PermanentPlugSnippet(
	plug *interfaces.Plug,
	securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		return mirPermanentSlotAppArmor, nil
	case interfaces.SecuritySecComp:
		return mirPermanentSlotSecComp, nil
	case interfaces.SecurityUDev, interfaces.SecurityDBus:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

func (iface *MirInterface) ConnectedPlugSnippet(
	plug *interfaces.Plug,
	slot *interfaces.Slot,
	securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		return nil, nil
	case interfaces.SecuritySecComp:
		return nil, nil
	case interfaces.SecurityUDev, interfaces.SecurityDBus:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

func (iface *MirInterface) PermanentSlotSnippet(
	slot *interfaces.Slot,
	securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		return nil, nil
	case interfaces.SecuritySecComp:
		return nil, nil
	case interfaces.SecurityUDev, interfaces.SecurityDBus:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

func (iface *MirInterface) ConnectedSlotSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		return nil, nil
	case interfaces.SecuritySecComp:
		return nil, nil
	case interfaces.SecurityUDev, interfaces.SecurityDBus:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

func (iface *MirInterface) SanitizePlug(plug *interfaces.Plug) error {
	return nil
}

func (iface *MirInterface) SanitizeSlot(slot *interfaces.Slot) error {
	return nil
}

func (iface *MirInterface) AutoConnect() bool {
	return false
}
