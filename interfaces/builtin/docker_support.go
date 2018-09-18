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
	"fmt"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/snap"
)

const dockerSupportSummary = `allows operating as the Docker daemon`

const dockerSupportBaseDeclarationPlugs = `
  docker-support:
    allow-installation: false
    deny-auto-connection: true
`

const dockerSupportBaseDeclarationSlots = `
  docker-support:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const dockerSupportConnectedPlugAppArmor = `
# Description: allow operating as the Docker daemon. This policy is
# intentionally not restrictive and is here to help guard against programming
# errors and not for security confinement. The Docker daemon by design requires
# extensive access to the system and cannot be effectively confined against
# malicious activity.

#include <abstractions/dbus-strict>

# Allow sockets
/{,var/}run/docker.sock rw,
/{,var/}run/docker/     rw,
/{,var/}run/docker/**   mrwklix,
/{,var/}run/runc/       rw,
/{,var/}run/runc/**     mrwklix,

# Socket for docker-container-shim
unix (bind,listen) type=stream addr="@/containerd-shim/moby/*/shim.sock\x00",

/{,var/}run/mount/utab r,

# Wide read access to /proc, but somewhat limited writes for now
@{PROC}/ r,
@{PROC}/** r,
@{PROC}/[0-9]*/attr/exec w,
@{PROC}/[0-9]*/oom_score_adj w,

# Limited read access to specific bits of /sys
/sys/kernel/mm/hugepages/ r,
/sys/fs/cgroup/cpuset/cpuset.cpus r,
/sys/fs/cgroup/cpuset/cpuset.mems r,
/sys/module/apparmor/parameters/enabled r,

# Limit cgroup writes a bit (Docker uses a "docker" sub-group)
/sys/fs/cgroup/*/docker/   rw,
/sys/fs/cgroup/*/docker/** rw,

# Allow tracing ourself (especially the "runc" process we create)
ptrace (trace) peer=@{profile_name},

# Docker needs a lot of caps, but limits them in the app container
capability,

# Docker does all kinds of mounts all over the filesystem
/dev/mapper/control rw,
/dev/mapper/docker* rw,
/dev/loop-control r,
/dev/loop[0-9]* rw,
/sys/devices/virtual/block/dm-[0-9]*/** r,
mount,
umount,

# After doing a pivot_root using <graph-dir>/<container-fs>/.pivot_rootNNNNNN,
# Docker removes the leftover /.pivot_rootNNNNNN directory (which is now
# relative to "/" instead of "<graph-dir>/<container-fs>" thanks to pivot_root)
pivot_root,
/.pivot_root[0-9]*/ rw,

# file descriptors (/proc/NNN/fd/X)
# file descriptors in the container show up here due to attach_disconnected
/[0-9]* rw,

# Docker needs to be able to create and load the profile it applies to
# containers ("docker-default")
/sbin/apparmor_parser ixr,
/etc/apparmor.d/cache/ r,
/etc/apparmor.d/cache/.features r,
/etc/apparmor.d/cache/docker* rw,
/etc/apparmor/parser.conf r,
/etc/apparmor/subdomain.conf r,
/sys/kernel/security/apparmor/.replace rw,
/sys/kernel/security/apparmor/{,**} r,

# use 'privileged-containers: true' to support --security-opts
change_profile -> docker-default,
signal (send) peer=docker-default,
ptrace (read, trace) peer=docker-default,

# Graph (storage) driver bits
/{dev,run}/shm/aufs.xino mrw,
/proc/fs/aufs/plink_maint w,
/sys/fs/aufs/** r,

#cf bug 1502785
/ r,
`

const dockerSupportConnectedPlugSecComp = `
# Description: allow operating as the Docker daemon. This policy is
# intentionally not restrictive and is here to help guard against programming
# errors and not for security confinement. The Docker daemon by design requires
# extensive access to the system and cannot be effectively confined against
# malicious activity.

# Because seccomp may only go more strict, we must allow all syscalls to Docker
# that it expects to give to containers in addition to what it needs to run and
# trust that docker daemon # only gives out reasonable syscalls to containers.

# Docker includes these in the default container whitelist, but they're
# potentially dangerous.
#finit_module
#init_module
#query_module
#delete_module

# These have a history of vulnerabilities, are not widely used, and
# open_by_handle_at has been used to break out of Docker containers by brute
# forcing the handle value: http://stealth.openwall.net/xSports/shocker.c
#name_to_handle_at
#open_by_handle_at

# Calls the Docker daemon itself requires

# /snap/docker/VERSION/bin/docker-runc
#   "do not inherit the parent's session keyring"
#   "make session keyring searcheable"
# runC uses this to ensure the container doesn't have access to the host
# keyring
keyctl

# /snap/docker/VERSION/bin/docker-runc
pivot_root

# ptrace can be abused to break out of the seccomp sandbox
# but is required by the Docker daemon.
ptrace

# This list comes from Docker's default seccomp whitelist (which is applied to
#   all containers launched unless a custom profile is specified or
#   "--privileged" is used)
# https://github.com/docker/docker/blob/v1.12.0/profiles/seccomp/seccomp_default.go#L39-L1879
# It has been further filtered to exclude certain known-troublesome syscalls.
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
nanosleep
newfstatat
_newselect
open
openat
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
pwrite64
pwritev
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

const dockerSupportPrivilegedAppArmor = `
# Description: allow docker daemon to run privileged containers. This gives
# full access to all resources on the system and thus gives device ownership to
# connected snaps.

# These rules are here to allow Docker to launch unconfined containers but
# allow the docker daemon itself to go unconfined. Since it runs as root, this
# grants device ownership.
change_profile -> *,
signal (send) peer=unconfined,
ptrace (read, trace) peer=unconfined,

# This grants raw access to device files and thus device ownership
/dev/** mrwkl,
@{PROC}/** mrwkl,
`

const dockerSupportPrivilegedSecComp = `
# Description: allow docker daemon to run privileged containers. This gives
# full access to all resources on the system and thus gives device ownership to
# connected snaps.

# This grants, among other things, kernel module loading and therefore device
# ownership.
@unrestricted
`

type dockerSupportInterface struct{}

func (iface *dockerSupportInterface) Name() string {
	return "docker-support"
}

func (iface *dockerSupportInterface) StaticInfo() interfaces.StaticInfo {
	return interfaces.StaticInfo{
		Summary:              dockerSupportSummary,
		ImplicitOnCore:       true,
		ImplicitOnClassic:    true,
		BaseDeclarationPlugs: dockerSupportBaseDeclarationPlugs,
		BaseDeclarationSlots: dockerSupportBaseDeclarationSlots,
	}
}

func (iface *dockerSupportInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	var privileged bool
	_ = plug.Attr("privileged-containers", &privileged)
	spec.AddSnippet(dockerSupportConnectedPlugAppArmor)
	if privileged {
		spec.AddSnippet(dockerSupportPrivilegedAppArmor)
	}
	return nil
}

func (iface *dockerSupportInterface) SecCompConnectedPlug(spec *seccomp.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	var privileged bool
	_ = plug.Attr("privileged-containers", &privileged)
	snippet := dockerSupportConnectedPlugSecComp
	if privileged {
		snippet += dockerSupportPrivilegedSecComp
	}
	spec.AddSnippet(snippet)
	return nil
}

func (iface *dockerSupportInterface) BeforePreparePlug(plug *snap.PlugInfo) error {
	if v, ok := plug.Attrs["privileged-containers"]; ok {
		if _, ok = v.(bool); !ok {
			return fmt.Errorf("docker-support plug requires bool with 'privileged-containers'")
		}
	}
	return nil
}

func (iface *dockerSupportInterface) AutoConnect(*snap.PlugInfo, *snap.SlotInfo) bool {
	// allow what declarations allowed
	return true
}

func init() {
	registerIface(&dockerSupportInterface{})
}
