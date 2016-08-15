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

const dockerPermanentSlotAppArmor = `
# Description: Allow operating as the Docker daemon. Reserved because this
#  gives privileged access to the system.
# Usage: reserved

# Allow sockets
/{,var/}run/docker.sock rw,
/{,var/}run/docker/     rw,
/{,var/}run/docker/**   mrwklix,
/{,var/}run/runc/       rw,
/{,var/}run/runc/**     mrwklix,

# Wide read access to /proc, but somewhat limited writes for now
@{PROC}/ r,
@{PROC}/** r,
@{PROC}/[0-9]*/attr/exec w,
@{PROC}/sys/net/** w,
@{PROC}/[0-9]*/cmdline r,
@{PROC}/[0-9]*/oom_score_adj w,

# Wide read access to /sys
/sys/** r,
# Limit cgroup writes a bit
/sys/fs/cgroup/*/docker/   rw,
/sys/fs/cgroup/*/docker/** rw,

# Allow tracing ourself (especially the "runc" process we create)
ptrace (trace) peer=@{profile_name},

# Docker needs a lot of caps, but limits them in the app container
capability,

# Docker does all kinds of mounts all over the filesystem
/dev/mapper/control rw,
/dev/mapper/docker* rw,
/dev/loop* r,
/dev/loop[0-9]* w,
mount,
umount,

# After doing a pivot_root using <graph-dir>/<container-fs>/.pivot_rootNNNNNN,
# Docker removes the leftover /.pivot_rootNNNNNN directory (which is now
# relative to "/" instead of "<graph-dir>/<container-fs>" thanks to pivot_root)
pivot_root,
/.pivot_root[0-9]*/ rw,

# file descriptors (/proc/NNN/fd/X)
/[0-9]* rw,
# file descriptors in the container show up here due to attach_disconnected

# Docker needs to be able to create and load the profile it applies to containers ("docker-default")
# XXX We might be able to get rid of this if we generate and load docker-default ourselves and make docker not do it.
/sbin/apparmor_parser ixr,
/etc/apparmor.d/cache/ r,
/etc/apparmor.d/cache/.features r,
/etc/apparmor.d/cache/docker* rw,
/etc/apparmor/parser.conf r,
/etc/apparmor/subdomain.conf r,
/sys/kernel/security/apparmor/.replace rw,

# We'll want to adjust this to support --security-opts...
change_profile -> docker-default,
signal (send) peer=docker-default,
ptrace (read, trace) peer=docker-default,

# Graph (storage) driver bits
/dev/shm/aufs.xino rw,

#cf bug 1502785
/ r,
`

const dockerConnectedPlugAppArmor = `
# Description: Allow using Docker. Reserved because this gives
#  privileged access to the service/host.
# Usage: reserved

# Obviously need to be able to talk to the daemon
/{,var/}run/docker.sock rw,

@{PROC}/sys/net/core/somaxconn r,
`

const dockerPermanentSlotSecComp = `
# The Docker daemon needs to be able to launch arbitrary processes within
# containers (whose syscall needs are unknown beforehand)

# Calls the Docker daemon itself requires
#   /snap/docker/VERSION/bin/docker-runc
#     "do not inherit the parent's session keyring"
#     "make session keyring searcheable"
keyctl
#   /snap/docker/VERSION/bin/docker-runc
pivot_root

# This list comes from Docker's default seccomp whitelist (which is applied to
#   all containers launched unless a custom profile is specified or
#   "--privileged" is used)
# https://github.com/docker/docker/blob/v1.12.0/profiles/seccomp/seccomp_default.go#L39-L1879
# $ grep -C1 'ActAllow' -- profiles/seccomp/seccomp_default.go \
#     | grep 'Name:' \
#     | cut -d'"' -f2 \
#     | sort -u
accept
accept4
access
acct
adjtimex
alarm
arch_prctl
bind
bpf
breakpoint
brk
cacheflush
capget
capset
chdir
chmod
chown
chown32
chroot
clock_getres
clock_gettime
clock_nanosleep
clone
close
connect
copy_file_range
creat
delete_module
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
exit
exit_group
faccessat
fadvise64
fadvise64_64
fallocate
fanotify_init
fanotify_mark
fchdir
fchmod
fchmodat
fchown
fchown32
fchownat
fcntl
fcntl64
fdatasync
fgetxattr
finit_module
flistxattr
flock
fork
fremovexattr
fsetxattr
fstat
fstat64
fstatat64
fstatfs
fstatfs64
fsync
ftruncate
ftruncate64
futex
futimesat
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
getpeername
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
get_robust_list
getrusage
getsid
getsockname
getsockopt
get_thread_area
gettid
gettimeofday
getuid
getuid32
getxattr
init_module
inotify_add_watch
inotify_init
inotify_init1
inotify_rm_watch
io_cancel
ioctl
io_destroy
io_getevents
ioperm
iopl
ioprio_get
ioprio_set
io_setup
io_submit
ipc
kcmp
kill
lchown
lchown32
lgetxattr
link
linkat
listen
listxattr
llistxattr
_llseek
lookup_dcookie
lremovexattr
lseek
lsetxattr
lstat
lstat64
madvise
memfd_create
mincore
mkdir
mkdirat
mknod
mknodat
mlock
mlock2
mlockall
mmap
mmap2
modify_ldt
mount
mprotect
mq_getsetattr
mq_notify
mq_open
mq_timedreceive
mq_timedsend
mq_unlink
mremap
msgctl
msgget
msgrcv
msgsnd
msync
munlock
munlockall
munmap
name_to_handle_at
nanosleep
newfstatat
_newselect
open
openat
open_by_handle_at
pause
perf_event_open
personality
pipe
pipe2
poll
ppoll
prctl
pread64
preadv
prlimit64
process_vm_readv
process_vm_writev
pselect6
ptrace
pwrite64
pwritev
query_module
read
readahead
readlink
readlinkat
readv
reboot
recv
recvfrom
recvmmsg
recvmsg
remap_file_pages
removexattr
rename
renameat
renameat2
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
s390_pci_mmio_read
s390_pci_mmio_write
s390_runtime_instr
sched_getaffinity
sched_getattr
sched_getparam
sched_get_priority_max
sched_get_priority_min
sched_getscheduler
sched_rr_get_interval
sched_setaffinity
sched_setattr
sched_setparam
sched_setscheduler
sched_yield
seccomp
select
semctl
semget
semop
semtimedop
send
sendfile
sendfile64
sendmmsg
sendmsg
sendto
setdomainname
setfsgid
setfsgid32
setfsuid
setfsuid32
setgid
setgid32
setgroups
setgroups32
sethostname
setitimer
setns
setpgid
setpriority
setregid
setregid32
setresgid
setresgid32
setresuid
setresuid32
setreuid
setreuid32
setrlimit
set_robust_list
setsid
setsockopt
set_thread_area
set_tid_address
settimeofday
set_tls
setuid
setuid32
setxattr
shmat
shmctl
shmdt
shmget
shutdown
sigaltstack
signalfd
signalfd4
sigreturn
socket
socketcall
socketpair
splice
stat
stat64
statfs
statfs64
stime
symlink
symlinkat
sync
sync_file_range
syncfs
sysinfo
syslog
tee
tgkill
time
timer_create
timer_delete
timerfd_create
timerfd_gettime
timerfd_settime
timer_getoverrun
timer_gettime
timer_settime
times
tkill
truncate
truncate64
ugetrlimit
umask
umount
umount2
uname
unlink
unlinkat
unshare
utime
utimensat
utimes
vfork
vhangup
vmsplice
wait4
waitid
waitpid
write
writev
`

const dockerConnectedPlugSecComp = `
setsockopt
bind
`

type DockerInterface struct{}

func (iface *DockerInterface) Name() string {
	return "docker"
}

func (iface *DockerInterface) PermanentPlugSnippet(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor, interfaces.SecurityDBus, interfaces.SecurityMount, interfaces.SecuritySecComp, interfaces.SecurityUDev:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

func (iface *DockerInterface) ConnectedPlugSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		return []byte(dockerConnectedPlugAppArmor), nil
	case interfaces.SecuritySecComp:
		return []byte(dockerConnectedPlugSecComp), nil
	case interfaces.SecurityDBus, interfaces.SecurityMount, interfaces.SecurityUDev:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

func (iface *DockerInterface) PermanentSlotSnippet(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		return []byte(dockerPermanentSlotAppArmor), nil
	case interfaces.SecuritySecComp:
		return []byte(dockerPermanentSlotSecComp), nil
	case interfaces.SecurityDBus, interfaces.SecurityMount, interfaces.SecurityUDev:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

func (iface *DockerInterface) ConnectedSlotSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	// The docker socket is a named socket and therefore mediated by AppArmor file rules and we can't currently limit connecting clients by their security label
	switch securitySystem {
	case interfaces.SecurityAppArmor, interfaces.SecurityDBus, interfaces.SecurityMount, interfaces.SecuritySecComp, interfaces.SecurityUDev:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

func (iface *DockerInterface) SanitizePlug(plug *interfaces.Plug) error {
	return nil
}

func (iface *DockerInterface) SanitizeSlot(slot *interfaces.Slot) error {
	return nil
}

func (iface *DockerInterface) AutoConnect() bool {
	return false
}
