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
# The default seccomp policy is default deny with an allowlist of allowed
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
faccessat2

alarm
brk

# ARM private syscalls
breakpoint
cacheflush
get_tls
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

# Daemons typically run as 'root' so allow chown to 'root'. DAC will prevent
# non-root from chowning to root.
# (chown root:root)
chown - u:root g:root
chown32 - u:root g:root
fchown - u:root g:root
fchown32 - u:root g:root
fchownat - - u:root g:root
lchown - u:root g:root
lchown32 - u:root g:root
# (chown root)
chown - u:root -1
chown32 - u:root -1
fchown - u:root -1
fchown32 - u:root -1
fchownat - - u:root -1
lchown - u:root -1
lchown32 - u:root -1
# (chgrp root)
chown - -1 g:root
chown32 - -1 g:root
fchown - -1 g:root
fchown32 - -1 g:root
fchownat - - -1 g:root
lchown - -1 g:root
lchown32 - -1 g:root

clock_getres
clock_getres_time64
clock_gettime
clock_gettime64
clock_nanosleep
clock_nanosleep_time64
clone
clone3
close
close_range

# needed by ls -l
connect

# the file descriptors used here will already be mediated by apparmor,
# the 6th argument is flags, which currently is always 0
copy_file_range - - - - - 0

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
futex_time64
futex_waitv
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

# ioctl() mediation currently primarily relies on Linux capabilities as well as
# the initial syscall for the fd to pass to ioctl(). See 'man capabilities'
# and 'man ioctl_list'. TIOCSTI requires CAP_SYS_ADMIN but allows for faking
# input (man tty_ioctl), so we disallow it to prevent snaps plugging interfaces
# with 'capability sys_admin' from interfering with other snaps or the
# unconfined user's terminal.
# similarly, TIOCLINUX allows to fake input as well (man ioctl_console) so
# disallow that too
# TODO: this should be scaled back even more
~ioctl - TIOCSTI
~ioctl - TIOCLINUX
# see CVE-2019-7303
~ioctl - 4294967295|TIOCSTI
~ioctl - 4294967295|TIOCLINUX
ioctl

io_cancel
io_destroy
io_getevents
io_pgetevents
io_pgetevents_time64
io_setup
io_submit
ioprio_get
# affects other processes, requires CAP_SYS_ADMIN. Potentially allow with
# syscall filtering of (at least) IOPRIO_WHO_USER (LP: #1446748)
#ioprio_set

ipc
kill
# kcmp is guarded in the kernel via ptrace with PTRACE_MODE_READ_REALCREDS
# such that the calling process must already be able to ptrace the target
# processes and so this is safe.
kcmp - - KCMP_FILE
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

# Argument filtering with gt/ge/lt/le does not work properly with
# libseccomp < 2.4 or golang-seccomp < 0.9.1. See:
# - https://bugs.launchpad.net/snapd/+bug/1825052/comments/9
# - https://github.com/seccomp/libseccomp/issues/69
# Eventually we want to use >=0, but we need libseccomp and golang-seccomp to
# be updated everywhere first. In the meantime, use <=19 and rely on the fact
# that AppArmor mediates CAP_SYS_NICE (and for systems without AppArmor, we
# ignore this lack of mediation since snaps are not meaningfully confined).
#
# Allow using nice() with default or lower priority
nice <=19
# Allow using setpriority to set the priority of the calling process to default
# or lower priority (eg, 'nice -n 9 <command>')
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
ppoll_time64

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
recvmmsg_time64

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

# glibc 2.35 unconditionally calls rseq for all threads
rseq

rt_sigaction
rt_sigpending
rt_sigprocmask
rt_sigqueueinfo
rt_sigreturn
rt_sigsuspend
rt_sigtimedwait
rt_sigtimedwait_time64
rt_tgsigqueueinfo
sched_getaffinity
sched_getattr
sched_getparam
sched_get_priority_max
sched_get_priority_min
sched_getscheduler
sched_rr_get_interval
sched_rr_get_interval_time64
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
pselect6_time64

# Allow use of SysV semaphores. Note that allocated resources are not freed by
# OOM which can lead to global kernel resource leakage.
semctl
semget
semop
semtimedop
semtimedop_time64

# allow sending to sockets
send
sendto
sendmsg
sendmmsg

sendfile
sendfile64

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
socket AF_XDP
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
socket AF_QIPCRTR

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
statx

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

# At one point, we relied on AppArmor denying CAP_SYSLOG to prevent privileged
# syslog(2) access. However, as of Jammy, the kernel.dmesg_restrict sysctl has
# been set to 1 by default, which requires CAP_SYSLOG for access to /dev/kmsg.
# Thus, we have to grant CAP_SYSLOG to the log-observe interface, so we must
# explicitly deny the non-"observe" type privileged accesses here. The following
# only allows SYSLOG_ACTION_READ{_ALL}, SYSLOG_ACTION_SIZE_{UNREAD,BUFFER}
~syslog SYSLOG_ACTION_CLOSE
~syslog SYSLOG_ACTION_OPEN
~syslog SYSLOG_ACTION_READ_CLEAR
~syslog SYSLOG_ACTION_CLEAR
~syslog SYSLOG_ACTION_CONSOLE_OFF
~syslog SYSLOG_ACTION_CONSOLE_ON
~syslog SYSLOG_ACTION_CONSOLE_LEVEL
~syslog >SYSLOG_ACTION_SIZE_BUFFER
syslog

tee
tgkill
time
timer_create
timer_delete
timer_getoverrun
timer_gettime
timer_gettime64
timer_settime
timer_settime64
timerfd
timerfd_create
timerfd_gettime
timerfd_gettime64
timerfd_settime
timerfd_settime64
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
utimensat_time64
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

// socketcall is an older interface and single entry point that can be used
// instead of socket(), bind(), connect(), etc individually. It isn't needed
// by most architectures with new enough kernels and glibc, so we leave it out
// of the default policy and add only when needed.
const socketcallSyscallDeprecated = `
# Add socketcall() for system and/or base that requires it. LP: #1446748
socketcall
`

// Historically snapd has allowed the use of the various setuid, setgid and
// setgroups syscalls, relying on AppArmor for mediation of the CAP_SETUID and
// CAP_SETGID. In core20, these can be dropped.
var barePrivDropSyscalls = `
# Allow these and rely on AppArmor to mediate CAP_SETUID and CAP_SETGID. When
# dropping to particular UID/GIDs, we'll use a different set of
# argument-filtered syscalls.
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
`

// Syscalls for setuid/setgid family of syscalls when dealing with only root
// uid and gid
var rootSetUidGidSyscalls = `
# Allow various setuid/setgid/chown family of syscalls with argument
# filtering. AppArmor has corresponding CAP_SETUID, CAP_SETGID and CAP_CHOWN
# rules.

# allow use of setgroups(0, ...). Note: while the setgroups() man page states
# that 'setgroups(0, NULL) should be used to clear all supplementary groups,
# the kernel will not consult the group list when size is '0', so we allow it
# to be anything for compatibility with (arguably buggy) programs that expect
# to clear the groups with 'setgroups(0, <non-null>).
setgroups 0 -
setgroups32 0 -

# allow setgid to root
setgid g:root
setgid32 g:root

# allow setuid to root
setuid u:root
setuid32 u:root

# allow setregid to root
setregid g:root g:root
setregid32 g:root g:root
setregid -1 g:root
setregid32 -1 g:root
setregid g:root -1
setregid32 g:root -1

# allow setresgid to root
# (permanent drop)
setresgid g:root g:root g:root
setresgid32 g:root g:root g:root
# (setegid)
setresgid -1 g:root -1
setresgid32 -1 g:root -1
# (setgid equivalent)
setresgid g:root g:root -1
setresgid32 g:root g:root -1

# allow setreuid to root
setreuid u:root u:root
setreuid32 u:root u:root
setreuid -1 u:root
setreuid32 -1 u:root
setreuid u:root -1
setreuid32 u:root -1

# allow setresuid to root
# (permanent drop)
setresuid u:root u:root u:root
setresuid32 u:root u:root u:root
# (seteuid)
setresuid -1 u:root -1
setresuid32 -1 u:root -1
# (setuid equivalent)
setresuid u:root u:root -1
setresuid32 u:root u:root -1
`

// Template for privilege drop and chown operations. This intentionally does
// not support all combinations of users or obscure combinations (we can add
// combinations as users dictate). Eg, these are supported:
//
//	chown foo:foo
//	chown foo
//	chgrp foo
//
// but these are not:
//
//	chown foo:bar
//	chown bar:foo
//
// For now, users who want 'foo:bar' can do:
//
//	chown foo ; chgrp bar
var privDropAndChownSyscalls = `
# allow setgid to ###GROUP###
setgid g:###GROUP###
setgid32 g:###GROUP###

# allow setregid to ###GROUP###
setregid g:###GROUP### g:###GROUP###
setregid32 g:###GROUP### g:###GROUP###
setregid -1 g:###GROUP###
setregid32 -1 g:###GROUP###
setregid g:###GROUP### -1
setregid32 g:###GROUP### -1
# (real root)
setregid g:root g:###GROUP###
setregid32 g:root g:###GROUP###
# (euid root)
setregid g:###GROUP### g:root
setregid32 g:###GROUP### g:root

# allow setresgid to ###GROUP###
# (permanent drop)
setresgid g:###GROUP### g:###GROUP### g:###GROUP###
setresgid32 g:###GROUP### g:###GROUP### g:###GROUP###
# (setegid)
setresgid -1 g:###GROUP### -1
setresgid32 -1 g:###GROUP### -1
# (setgid equivalent)
setresgid g:###GROUP### g:###GROUP### -1
setresgid32 g:###GROUP### g:###GROUP### -1
# (saving root)
setresgid g:###GROUP### g:###GROUP### g:root
setresgid32 g:###GROUP### g:###GROUP### g:root
# (euid root and saving root)
setresgid g:###GROUP### g:root g:root
setresgid32 g:###GROUP### g:root g:root

# allow setuid to ###USERNAME###
setuid u:###USERNAME###
setuid32 u:###USERNAME###

# allow setreuid to ###USERNAME###
setreuid u:###USERNAME### u:###USERNAME###
setreuid32 u:###USERNAME### u:###USERNAME###
setreuid -1 u:###USERNAME###
setreuid32 -1 u:###USERNAME###
setreuid u:###USERNAME### -1
setreuid32 u:###USERNAME### -1
# (real root)
setreuid u:root u:###USERNAME###
setreuid32 u:root u:###USERNAME###
# (euid root)
setreuid u:###USERNAME### u:root
setreuid32 u:###USERNAME### u:root

# allow setresuid to ###USERNAME###
# (permanent drop)
setresuid u:###USERNAME### u:###USERNAME### u:###USERNAME###
setresuid32 u:###USERNAME### u:###USERNAME### u:###USERNAME###
# (seteuid)
setresuid -1 u:###USERNAME### -1
setresuid32 -1 u:###USERNAME### -1
# (setuid equivalent)
setresuid u:###USERNAME### u:###USERNAME### -1
setresuid32 u:###USERNAME### u:###USERNAME### -1
# (saving root)
setresuid u:###USERNAME### u:###USERNAME### u:root
setresuid32 u:###USERNAME### u:###USERNAME### u:root
# (euid root and saving root)
setresuid u:###USERNAME### u:root u:root
setresuid32 u:###USERNAME### u:root u:root

# allow chown to ###USERNAME###:###GROUP###
# (chown ###USERNAME###:###GROUP###)
chown - u:###USERNAME### g:###GROUP###
chown32 - u:###USERNAME### g:###GROUP###
fchown - u:###USERNAME### g:###GROUP###
fchown32 - u:###USERNAME### g:###GROUP###
fchownat - - u:###USERNAME### g:###GROUP###
lchown - u:###USERNAME### g:###GROUP###
lchown32 - u:###USERNAME### g:###GROUP###
# (chown ###USERNAME###)
chown - u:###USERNAME### -1
chown32 - u:###USERNAME### -1
fchown - u:###USERNAME### -1
fchown32 - u:###USERNAME### -1
fchownat - - u:###USERNAME### -1
lchown - u:###USERNAME### -1
lchown32 - u:###USERNAME### -1
# (chgrp ###GROUP###)
chown - -1 g:###GROUP###
chown32 - -1 g:###GROUP###
fchown - -1 g:###GROUP###
fchown32 - -1 g:###GROUP###
fchownat - - -1 g:###GROUP###
lchown - -1 g:###GROUP###
lchown32 - -1 g:###GROUP###

# allow chown to ###USERNAME###:root
chown - u:###USERNAME### g:root
chown32 - u:###USERNAME### g:root
fchown - u:###USERNAME### g:root
fchown32 - u:###USERNAME### g:root
fchownat - - u:###USERNAME### g:root
lchown - u:###USERNAME### g:root
lchown32 - u:###USERNAME### g:root

# allow chown to root:###GROUP###
chown - u:root g:###GROUP###
chown32 - u:root g:###GROUP###
fchown - u:root g:###GROUP###
fchown32 - u:root g:###GROUP###
fchownat - - u:root g:###GROUP###
lchown - u:root g:###GROUP###
lchown32 - u:root g:###GROUP###
`
