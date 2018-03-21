// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2018 Canonical Ltd
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

package seccomp

// defaultTemplate contains default seccomp template.
// It can be overridden for testing using MockTemplate().
var defaultTemplate = []byte(`
# Description: Allows access to app-specific directories and basic runtime
#
# The default seccomp policy is default deny with a whitelist of allowed
# syscalls. The default policy is intended to be safe for any application to
# use and should be evaluated in conjunction with other security backends (eg
# AppArmor). For example, a few particularly problematic syscalls that are left
# out of the default policy are (non-exhaustive):
# - kexec_load
# - create_module, init_module, finit_module, delete_module (kernel modules)
# - name_to_handle_at (history of vulnerabilities)
# - open_by_handle_at (history of vulnerabilities)
# - ptrace (can be used to break out of sandbox with <4.8 kernels)
# - add_key, keyctl, request_key (kernel keyring)

#
# Allowed accesses
#

access
faccessat

alarm
brk

# ARM private syscalls
breakpoint
cacheflush
set_tls
usr26
usr32

capget
# AppArmor mediates capabilities, so allow capset (useful for apps that for
# example want to drop capabilities)
capset

chdir
fchdir

# We can't effectively block file perms due to open() with O_CREAT, so allow
# chmod until we have syscall arg filtering (LP: #1446748)
chmod
fchmod
fchmodat

# snappy doesn't currently support per-app UID/GIDs. All daemons run as 'root'
# so allow chown to 'root'. DAC will prevent non-root from chowning to root.
chown - u:root g:root
chown32 - u:root g:root
fchown - u:root g:root
fchown32 - u:root g:root
fchownat - - u:root g:root
lchown - u:root g:root
lchown32 - u:root g:root

clock_getres
clock_gettime
clock_nanosleep
clone
close

# needed by ls -l
connect

chroot

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

# TIOCSTI allows for faking input (man tty_ioctl)
# TODO: this should be scaled back even more
ioctl - !TIOCSTI

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
membarrier
memfd_create
mincore
mkdir
mkdirat
mlock
mlock2
mlockall
mmap
mmap2

# Allow mknod for regular files, pipes and sockets (and not block or char
# devices)
mknod - |S_IFREG -
mknodat - - |S_IFREG -
mknod - |S_IFIFO -
mknodat - - |S_IFIFO -
mknod - |S_IFSOCK -
mknodat - - |S_IFSOCK -

modify_ldt
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

# Allow using nice() with default or lower priority
# FIXME: https://github.com/seccomp/libseccomp/issues/69 which means we
# currently have to use <=19. When that bug is fixed, use >=0
nice <=19
# Allow using setpriority to set the priority of the calling process to default
# or lower priority (eg, 'nice -n 9 <command>')
# default or lower priority.
# FIXME: https://github.com/seccomp/libseccomp/issues/69 which means we
# currently have to use <=19. When that bug is fixed, use >=0
setpriority PRIO_PROCESS 0 <=19

# LP: #1446748 - support syscall arg filtering for mode_t with O_CREAT
open

openat
pause
personality
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

# allow reading from sockets
recv
recvfrom
recvmsg
recvmmsg

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
# enforce pid_t is 0 so the app may only change its own scheduler and affinity.
# Use process-control interface for controlling other pids.
sched_setaffinity 0 - -
sched_setparam 0 -

# 'sched_setscheduler' without argument filtering was allowed in 2.21 and
# earlier and 2.22 added 'sched_setscheduler 0 - -', introducing LP: #1661265.
# For now, continue to allow sched_setscheduler unconditionally.
sched_setscheduler

sched_yield

# Allow configuring seccomp filter. This is ok because the kernel enforces that
# the new filter is a subset of the current filter (ie, no widening
# permissions)
seccomp

select
_newselect
pselect
pselect6

semctl
semget
semop
semtimedop

# allow sending to sockets
send
sendto
sendmsg
sendmmsg

sendfile
sendfile64

# While we don't yet have seccomp arg filtering (LP: #1446748), we must allow
# these because the launcher drops privileges after seccomp_load(). Eventually
# we will only allow dropping to particular UIDs. For now, we mediate this with
# AppArmor
setgid
setgid32
setregid
setregid32
setresgid
setresgid32
setresuid
setresuid32
setreuid
setreuid32
setuid
setuid32
#setgroups
#setgroups32

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

setxattr
fsetxattr
lsetxattr

shmat
shmctl
shmdt
shmget
shutdown
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

# AppArmor mediates AF_UNIX/AF_LOCAL via 'unix' rules and all other AF_*
# domains via 'network' rules. We won't allow bare 'network' AppArmor rules, so
# we can allow 'socket' for all domains except AF_NETLINK and let AppArmor
# handle the rest.
socket AF_UNIX
socket AF_LOCAL
socket AF_INET
socket AF_INET6
socket AF_IPX
socket AF_X25
socket AF_AX25
socket AF_ATMPVC
socket AF_APPLETALK
socket AF_PACKET
socket AF_ALG
socket AF_CAN
socket AF_BRIDGE
socket AF_NETROM
socket AF_ROSE
socket AF_NETBEUI
socket AF_SECURITY
socket AF_KEY
socket AF_ASH
socket AF_ECONET
socket AF_SNA
socket AF_IRDA
socket AF_PPPOX
socket AF_WANPIPE
socket AF_BLUETOOTH
socket AF_RDS
socket AF_LLC
socket AF_TIPC
socket AF_IUCV
socket AF_RXRPC
socket AF_ISDN
socket AF_PHONET
socket AF_IEEE802154
socket AF_CAIF
socket AF_NFC
socket AF_VSOCK
socket AF_MPLS
socket AF_IB

# For usrsctp, AppArmor doesn't support 'network conn,' since AF_CONN is
# userspace and encapsulated in other domains that are mediated. As such, do
# not allow AF_CONN by default here.
# socket AF_CONN

# For AF_NETLINK, we'll use a combination of AppArmor coarse mediation and
# seccomp arg filtering of netlink families.
# socket AF_NETLINK - -

# needed by snapctl
getsockopt
setsockopt
getsockname
getpeername

# Per man page, on Linux this is limited to only AF_UNIX so it is ok to have
# in the default template
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
timerfd
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

# FIXME: remove this after LP: #1446748 is implemented
# This is an older interface and single entry point that can be used instead
# of socket(), bind(), connect(), etc individually.
socketcall
`)

// Go's net package attempts to bind early to check whether IPv6 is available or not.
// For systems with apparmor enabled, this will be mediated and cause an error to be
// returned. Without apparmor, the call goes through to seccomp and the process is
// killed instead of just getting the error.
//
// For that reason once apparmor is disabled the seccomp profile is given access
// to bind, so that these processes are not improperly killed. There is on going
// work to make seccomp return an error in those cases as well and log the error.
// Once that's in place we can drop this hack.
const bindSyscallWorkaround = `
# Add bind() for systems with only Seccomp enabled to workaround
# LP #1644573
bind
`
