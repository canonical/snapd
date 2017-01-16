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

package seccomp

// defaultTemplate contains default seccomp template.
//
// It can be overridden for testing using MockTemplate().
//
// http://bazaar.launchpad.net/~ubuntu-security/ubuntu-core-security/trunk/view/head:/data/seccomp/templates/ubuntu-core/16.04/default
var defaultTemplate = []byte(`
# Description: Allows access to app-specific directories and basic runtime
# Usage: common
#

# Dangerous syscalls that we don't ever want to allow.
# Note: may uncomment once ubuntu-core-launcher understands @deny rules and
# if/when we conditionally deny these in the future.

# kexec
#@deny kexec_load

# kernel modules
#@deny create_module
#@deny init_module
#@deny finit_module
#@deny delete_module

# these have a history of vulnerabilities, are not widely used, and
# open_by_handle_at has been used to break out of docker containers by brute
# forcing the handle value: http://stealth.openwall.net/xSports/shocker.c
#@deny name_to_handle_at
#@deny open_by_handle_at

# Explicitly deny ptrace since it can be abused to break out of the seccomp
# sandbox
#@deny ptrace

# Explicitly deny capability mknod so apps can't create devices
#@deny mknod
#@deny mknodat

# Explicitly deny (u)mount so apps can't change mounts in their namespace
#@deny mount
#@deny umount
#@deny umount2

# Explicitly deny kernel keyring access
#@deny add_key
#@deny keyctl
#@deny request_key

# end dangerous syscalls

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

# snappy doesn't currently support per-app UID/GIDs so don't allow chown. To
# properly support chown, we need to have syscall arg filtering (LP: #1446748)
# and per-app UID/GIDs.
#chown
#chown32
#fchown
#fchown32
#fchownat
#lchown
#lchown32

clock_getres
clock_gettime
clock_nanosleep
clone
close
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

# LP: #1446748 - deny until we have syscall arg filtering. Alternatively, set
# RLIMIT_NICE hard limit for apps, launch them under an appropriate nice value
# and allow this call
#nice

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

# needed by ls -l
socket
connect

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
