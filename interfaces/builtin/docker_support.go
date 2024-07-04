// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2023 Canonical Ltd
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
	"github.com/snapcore/snapd/interfaces/kmod"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/release"
	apparmor_sandbox "github.com/snapcore/snapd/sandbox/apparmor"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/strutil"
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

const dockerSupportConnectedPlugAppArmorCore = `
# These accesses are necessary for Ubuntu Core 16 and 18, likely due to the
# version of apparmor or the kernel which doesn't resolve the upper layer of an
# overlayfs mount correctly the accesses show up as runc trying to read from
# /system-data/var/snap/docker/common/var-lib-docker/overlay2/$SHA/diff/
/system-data/var/snap/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/common/{,**} rwl,
/system-data/var/snap/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/@{SNAP_REVISION}/{,**} rwl,
`

const dockerSupportConnectedPlugAppArmorUserNS = `
# allow use of user namespaces
userns,
`

const dockerSupportConnectedPlugAppArmor = `
# Description: allow operating as the Docker daemon/containerd. This policy is
# intentionally not restrictive and is here to help guard against programming
# errors and not for security confinement. The Docker daemon by design requires
# extensive access to the system and cannot be effectively confined against
# malicious activity.

#include <abstractions/dbus-strict>

# Allow sockets/etc for docker
/{,var/}run/docker.sock rw,
/{,var/}run/docker/     rw,
/{,var/}run/docker/**   mrwklix,
/{,var/}run/runc/       rw,
/{,var/}run/runc/**     mrwklix,

# Allow sockets/etc for containerd
/{,var/}run/containerd/{,s/,runc/,runc/k8s.io/,runc/k8s.io/*/} rw,
/{,var/}run/containerd/runc/k8s.io/*/** rwk,
/{,var/}run/containerd/{io.containerd*/,io.containerd*/k8s.io/,io.containerd*/k8s.io/*/} rw,
/{,var/}run/containerd/io.containerd*/*/** rwk,
/{,var/}run/containerd/s/** rwk,

# Limit ipam-state to k8s
/run/ipam-state/k8s-** rw,
/run/ipam-state/k8s-*/lock k,

# Socket for docker-containerd-shim
unix (bind,listen) type=stream addr="@/containerd-shim/**.sock\x00",

/{,var/}run/mount/utab r,

# Wide read access to /proc, but somewhat limited writes for now
@{PROC}/ r,
@{PROC}/** r,
@{PROC}/[0-9]*/attr/{,apparmor/}exec w,
@{PROC}/[0-9]*/oom_score_adj w,

# Limited read access to specific bits of /sys
/sys/kernel/mm/hugepages/ r,
/sys/kernel/mm/transparent_hugepage/{,**} r,
/sys/fs/cgroup/cpuset/cpuset.cpus r,
/sys/fs/cgroup/cpuset/cpuset.mems r,
/sys/module/apparmor/parameters/enabled r,

# Limit cgroup writes a bit (Docker uses a "docker" sub-group)
/sys/fs/cgroup/*/docker/   rw,
/sys/fs/cgroup/*/docker/** rw,

# Also allow cgroup writes to kubernetes pods
/sys/fs/cgroup/*/kubepods/ rw,
/sys/fs/cgroup/*/kubepods/** rw,

# containerd can also be configured to use the systemd cgroup driver via
# plugins.cri.systemd_cgroup = true which moves container processes into
# systemd-managed cgroups. This is now the recommended configuration since it
# provides a single cgroup manager (systemd) in an effort to achieve consistent
# views of resources.
/sys/fs/cgroup/*/systemd/{,system.slice/} rw,          # create missing dirs
/sys/fs/cgroup/*/systemd/system.slice/** r,
/sys/fs/cgroup/*/systemd/system.slice/cgroup.procs w,

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
/{,usr/}sbin/apparmor_parser ixr,
/etc/apparmor.d/cache/ r,            # apparmor 2.12 and below
/etc/apparmor.d/cache/.features r,
/etc/apparmor.d/{,cache/}docker* rw,
/var/cache/apparmor/{,*/} r,         # apparmor 2.13 and higher
/var/cache/apparmor/*/.features r,
/var/cache/apparmor/*/docker* rw,
/etc/apparmor.d/tunables/{,**} r,
/etc/apparmor.d/abstractions/{,**} r,
/etc/apparmor/parser.conf r,
/etc/apparmor.d/abi/{,*} r,
/etc/apparmor/subdomain.conf r,
/sys/kernel/security/apparmor/.replace rw,
/sys/kernel/security/apparmor/{,**} r,

# use 'privileged-containers: true' to support --security-opts

# defaults for docker-default
# Unfortunately, the docker snap is currently (by design?) setup to have both 
# the privileged and unprivileged variant of the docker-support interface 
# connected which means we have rules that are compatible to allow both 
# transitioning to docker-default profile here AAAAAAND transitioning to any 
# other profile below in the privileged snippet, BUUUUUUUT also need to be 
# triply compatible with the injected compatibility snap-confine transition 
# rules to temporarily support executing other snaps from devmode snaps. 
# So we are left with writing out these extremely verbose regexps because AARE 
# does not have a negative concept to exclude just the paths we want. 
# See also https://bugs.launchpad.net/apparmor/+bug/1964853 and
# https://bugs.launchpad.net/apparmor/+bug/1964854 for more details on the 
# AppArmor parser side of things.
# TODO: When we drop support for executing other snaps from devmode snaps (or 
# when the AppArmor parser bugs are fixed) this can go back to the much simpler
# rule:
# change_profile unsafe /** -> docker-default,
# below are auto-generated rules using GenerateAAREExclusionPatterns
###EXCL{change_profile unsafe <> -> docker-default,:/snap/snapd/*/usr/lib/snapd/snap-confine,/snap/core/*/usr/lib/snapd/snap-confine}###

# signal/tracing rules too
signal (send) peer=docker-default,
ptrace (read, trace) peer=docker-default,

# defaults for containerd
# TODO: When we drop support for executing other snaps from devmode snaps (or 
# when the AppArmor parser bugs are fixed) this can go back to the much simpler
# rule:	
# change_profile unsafe /** -> cri-containerd.apparmor.d,
# below are auto-generated rules using GenerateAAREExclusionPatterns
###EXCL{change_profile unsafe <> -> cri-containerd.apparmor.d,:/snap/snapd/*/usr/lib/snapd/snap-confine,/snap/core/*/usr/lib/snapd/snap-confine}###

# signal/tracing rules too
signal (send) peer=cri-containerd.apparmor.d,
ptrace (read, trace) peer=cri-containerd.apparmor.d,

# Graph (storage) driver bits
/{dev,run}/shm/aufs.xino mrw,
/proc/fs/aufs/plink_maint w,
/sys/fs/aufs/** r,

#cf bug 1502785
/ r,

# recent versions of docker make a symlink from /dev/ptmx to /dev/pts/ptmx
# and so to allow allocating a new shell we need this
/dev/pts/ptmx rw,

# needed by runc for mitigation of CVE-2019-5736
# For details see https://bugs.launchpad.net/apparmor/+bug/1820344
/ ix,
/bin/runc ixr,

/pause ixr,
/bin/busybox ixr,

# When kubernetes drives containerd, containerd needs access to CNI services,
# like flanneld's subnet.env for DNS. This would ideally be snap-specific (it
# could if the control plane was a snap), but in deployments where the control
# plane is not a snap, it will tell flannel to use this path.
/run/flannel/{,**} rk,

# When kubernetes drives containerd, containerd needs access to various
# secrets for the pods which are overlayed at /run/secrets/....
# This would ideally be snap-specific (it could if the control plane was a
# snap), but in deployments where the control plane is not a snap, it will tell
# containerd to use this path for various account information for pods.
/run/secrets/kubernetes.io/{,**} rk,

# Allow using the 'autobind' feature of bind() (eg, for journald via go-systemd)
# unix (bind) type=dgram addr=auto,
# TODO: when snapd vendors in AppArmor userspace, then enable the new syntax
# above which allows only "empty"/automatic addresses, for now we simply permit
# all addresses with SOCK_DGRAM type, which leaks info for other addresses than
# what docker tries to use
# see https://bugs.launchpad.net/snapd/+bug/1867216
unix (bind) type=dgram,

# With cgroup v2, docker uses the systemd driver to run the containers,
# which requires dockerd to talk to systemd over system bus.
dbus (send)
    bus=system
    path=/org/freedesktop/systemd1
    interface=org.freedesktop.systemd1.Manager
    member={StartTransientUnit,KillUnit,StopUnit,ResetFailedUnit,SetUnitProperties}
    peer=(name=org.freedesktop.systemd1,label=unconfined),

dbus (receive)
    bus=system
    path=/org/freedesktop/systemd1
    interface=org.freedesktop.systemd1.Manager
    member=JobRemoved
    peer=(label=unconfined),

dbus (send)
    bus=system
    interface=org.freedesktop.DBus.Properties
    path=/org/freedesktop/systemd1
    member=Get{,All}
    peer=(name=org.freedesktop.systemd1,label=unconfined),

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
clock_getres_time64
clock_gettime
clock_gettime64
clock_nanosleep
clock_nanosleep_time64
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
futex_time64
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
get_tls
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
mq_timedreceive_time64
mq_timedsend
mq_timedsend_time64
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
ppoll_time64
prctl
pread64
preadv
prlimit64
process_vm_readv
process_vm_writev
pselect6
pselect6_time64
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
recvmmsg_time64
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
rt_sigtimedwait_time64
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
sched_rr_get_interval_time64
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
semtimedop_time64
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
timerfd_gettime64
timerfd_settime
timerfd_settime64
timer_getoverrun
timer_gettime
timer_gettime64
timer_settime
timer_settime64
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
utimensat_time64
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
# TODO: When we drop support for executing other snaps from devmode snaps (or 
# when the AppArmor parser bugs are fixed) this can go back to the much simpler
# rule:
# change_profile unsafe /**,
# but until then we need this set of rules to avoid exec transition conflicts.
# See also the comment above the "change_profile unsafe /** -> docker-default," 
# rule for more context.
# below are auto-generated rules using GenerateAAREExclusionPatterns
###EXCL{change_profile unsafe <>,:/snap/snapd/*/usr/lib/snapd/snap-confine,/snap/core/*/usr/lib/snapd/snap-confine}###

# allow signaling and tracing any unconfined process since if containers are 
# launched without confinement docker still needs to trace them
signal (send) peer=unconfined,
ptrace (read, trace) peer=unconfined,

# This grants raw access to device files and thus device ownership
/dev/** mrwkl,
@{PROC}/** mrwkl,

# When kubernetes drives docker/containerd, it creates and runs files in the
# container at arbitrary locations (eg, via pivot_root).
# Allow any file except for executing /snap/{snapd,core}/*/usr/lib/snapd/snap-confine
# because in devmode confinement we will have a separate "x" transition on exec
# rule that is in the policy that will overlap and thus conflict with this rule.
# TODO: When we drop support for executing other snaps from devmode snaps (or 
# when the AppArmor parser bugs are fixed) this can go back to the much simpler
# rule:
# /** rwlix,
# but until then we need this set of rules to avoid exec transition conflicts.
# See also the comment above the "change_profile unsafe /** -> docker-default," 
# rule for more context.
# below are auto-generated rules using GenerateAAREExclusionPatterns
###EXCL{<> rwlix,:/snap/snapd/*/usr/lib/snapd/snap-confine,/snap/core/*/usr/lib/snapd/snap-confine}###
`

const dockerSupportPrivilegedSecComp = `
# Description: allow docker daemon to run privileged containers. This gives
# full access to all resources on the system and thus gives device ownership to
# connected snaps.

# This grants, among other things, kernel module loading and therefore device
# ownership.
@unrestricted
`

const dockerSupportServiceSnippet = `Delegate=true`

type dockerSupportInterface struct {
	commonInterface
}

func (iface *dockerSupportInterface) KModConnectedPlug(spec *kmod.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	// https://kubernetes.io/docs/setup/production-environment/container-runtimes/
	if err := spec.AddModule("overlay"); err != nil {
		return err
	}
	return nil
}

func (iface *dockerSupportInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	var privileged bool
	_ = plug.Attr("privileged-containers", &privileged)

	// The 'change_profile unsafe' rules conflict with the 'ix' rules in
	// the home interface, so suppress them (LP: #1797786)
	spec.SetSuppressHomeIx()
	// New enough docker & containers it launches appear to get
	// denial for writing pycache inside the container... which I
	// guess to apparmor it looks like a snap. This is harmless, as
	// docker snap no longer ships any python, and thus will not
	// try to modify, otherwise immutable, pycache inside the
	// snaps.
	spec.SetSuppressPycacheDeny()

	defaultSnippet, err := apparmor_sandbox.InsertAAREExclusionPatterns(
		dockerSupportConnectedPlugAppArmor,
		[]string{
			"/snap/snapd/*/usr/lib/snapd/snap-confine",
			"/snap/core/*/usr/lib/snapd/snap-confine",
		},
		&apparmor_sandbox.AAREExclusionPatternsOptions{
			Prefix: "change_profile unsafe ",
			Suffix: " -> docker-default,",
		},
	)
	if err != nil {
		return err
	}

	defaultSnippet, err = apparmor_sandbox.InsertAAREExclusionPatterns(
		defaultSnippet,
		[]string{
			"/snap/snapd/*/usr/lib/snapd/snap-confine",
			"/snap/core/*/usr/lib/snapd/snap-confine",
		},
		&apparmor_sandbox.AAREExclusionPatternsOptions{
			Prefix: "change_profile unsafe ",
			Suffix: " -> cri-containerd.apparmor.d,",
		},
	)
	if err != nil {
		return err
	}

	spec.AddSnippet(defaultSnippet)
	if privileged {
		privilegedSnippet, err := apparmor_sandbox.InsertAAREExclusionPatterns(
			dockerSupportPrivilegedAppArmor,
			[]string{
				"/snap/snapd/*/usr/lib/snapd/snap-confine",
				"/snap/core/*/usr/lib/snapd/snap-confine",
			},
			&apparmor_sandbox.AAREExclusionPatternsOptions{
				Prefix: "change_profile unsafe ",
				Suffix: ",",
			},
		)
		if err != nil {
			return err
		}

		privilegedSnippet, err = apparmor_sandbox.InsertAAREExclusionPatterns(
			privilegedSnippet,
			[]string{
				"/snap/snapd/*/usr/lib/snapd/snap-confine",
				"/snap/core/*/usr/lib/snapd/snap-confine",
			},
			&apparmor_sandbox.AAREExclusionPatternsOptions{
				Prefix: "",
				Suffix: " rwlix,",
			},
		)
		if err != nil {
			return err
		}

		spec.AddSnippet(privilegedSnippet)
	}
	if !release.OnClassic {
		spec.AddSnippet(dockerSupportConnectedPlugAppArmorCore)
	}
	// if apparmor supports userns mediation then add this too
	if apparmor_sandbox.ProbedLevel() != apparmor_sandbox.Unsupported {
		features, err := apparmor_sandbox.ParserFeatures()
		if err != nil {
			return err
		}
		if strutil.ListContains(features, "userns") {
			spec.AddSnippet(dockerSupportConnectedPlugAppArmorUserNS)
		}
	}

	spec.SetUsesPtraceTrace()
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
	registerIface(&dockerSupportInterface{commonInterface{
		name:                 "docker-support",
		summary:              dockerSupportSummary,
		implicitOnCore:       true,
		implicitOnClassic:    true,
		baseDeclarationPlugs: dockerSupportBaseDeclarationPlugs,
		baseDeclarationSlots: dockerSupportBaseDeclarationSlots,
		controlsDeviceCgroup: true,
		serviceSnippets:      []string{dockerSupportServiceSnippet},
		// docker-support also uses ptrace(trace), but it already declares this in
		// the AppArmorConnectedPlug method
	}})
}
